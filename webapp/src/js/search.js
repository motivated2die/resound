// Search: finds sounds by name across all tabs, optionally of one colour.
//
// Opened by the search button in the top bar or by pulling down with two
// fingers on the sounds. The top bar says SEARCH, the tabs are tucked away,
// and the search field with the colour chips sits above the results. The
// results are the usual cards in the usual grid (#sounds-container), so
// they play, show progress and open their settings like on any tab; tab
// loading and swiping pause meanwhile (state.searching). Closing returns to
// the tab the user was on.
//
// With a virtual keyboard open, the bottom bar sits on top of it (the
// visual viewport shrinks by the keyboard's height: --keyboard in
// styles.css), so sounds can be stopped while typing. On Android that shrink
// leaves out the gesture bar's strip, which the keyboard covers too: there
// the bottom safe-area inset is added (.keyboard-plus-inset); on iPhone and
// iPad the shrink includes the home indicator's area already. Tapping a result keeps
// the keyboard open; scrolling the results or Enter closes it.
(() => {
    'use strict';

    const MAX_RESULTS = 200;
    const PULL_OPEN_PX = 70;   // two-finger pull that opens the search
    const COLORS = ['default', 'red', 'orange', 'yellow', 'green', 'blue', 'purple', 'pink'];

    const $ = (id) => document.getElementById(id);
    let panel, input, colors, grid, wrapper, topBar, button;
    let open = false;
    let color = null;          // the colour filter, or null
    let sounds = [];           // the library's sounds: { id, name, key, ... }
    let loading = null;
    let pushedHistory = false;

    // --- Matching --------------------------------------------------------------
    // Fuzzy, by words: every word of the query must match the name, as a
    // part of it (best at a word's start), as letters in order close together
    // ("drmrl" finds "drumroll"), or with a typo (one in 4+ letters, two in
    // 7+). Diacritics, case and _-. are ignored.

    const normalize = (text) => String(text || '')
        .normalize('NFD').replace(/[̀-ͯ]/g, '')
        .toLowerCase().replace(/[_\-.,()[\]]+/g, ' ').replace(/\s+/g, ' ').trim();

    // Optimal string alignment distance (adjacent swaps count once).
    const distance = (a, b) => {
        const d = Array.from({ length: a.length + 1 }, (_, i) => [i]);
        for (let j = 1; j <= b.length; j++) d[0][j] = j;
        for (let i = 1; i <= a.length; i++) {
            for (let j = 1; j <= b.length; j++) {
                const cost = a[i - 1] === b[j - 1] ? 0 : 1;
                d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + cost);
                if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) d[i][j] = Math.min(d[i][j], d[i - 2][j - 2] + 1);
            }
        }
        return d[a.length][b.length];
    };

    const wordScore = (token, key, words) => {
        // A part of the name.
        const at = key.indexOf(token);
        if (at >= 0) {
            const wordStart = at === 0 || key[at - 1] === ' ';
            return 100 + (wordStart ? 40 : 0) + (at === 0 ? 20 : 0) + token.length * 2 - Math.min(at, 20);
        }
        // Otherwise the better of: letters in order, not spread out; a typo
        // in a word (or the start of one), which beats letters strewn
        // across words.
        let best = -1;
        if (token.length >= 2) {
            let pos = -1, first = -1, gaps = 0;
            for (const ch of token) {
                const next = key.indexOf(ch, pos + 1);
                if (next < 0) { first = -2; break; }
                if (first === -1) first = next;
                else if (next > pos + 1) gaps += next - pos - 1;
                pos = next;
            }
            if (first >= 0 && gaps <= token.length * 2) {
                const wordStart = first === 0 || key[first - 1] === ' ';
                best = 60 + (wordStart ? 15 : 0) - gaps * 6;
            }
        }
        const allowed = token.length >= 7 ? 2 : token.length >= 4 ? 1 : 0;
        if (allowed) {
            let d = Infinity;
            for (const w of words) {
                if (Math.abs(w.length - token.length) <= allowed) d = Math.min(d, distance(token, w));
                if (w.length > token.length) d = Math.min(d, distance(token, w.slice(0, token.length)));
            }
            if (d <= allowed) best = Math.max(best, 70 - d * 15);
        }
        return best;
    };

    // results: the sounds matching the query and the colour, best first.
    const results = (query) => {
        const q = normalize(query);
        const tokens = q ? q.split(' ') : [];
        const out = [];
        for (const s of sounds) {
            if (color) {
                const c = persistence.getSoundSettings(s.id).color || 'default'; // live: the sheet may change it
                if (c !== color) continue;
            }
            let score = 0;
            for (const t of tokens) {
                const w = wordScore(t, s.key, s.words);
                if (w < 0) { score = -1; break; }
                score += w;
            }
            if (score < 0) continue;
            if (q && s.key.startsWith(q)) score += 50;
            out.push({ s, score });
        }
        out.sort((a, b) => b.score - a.score || a.s.name.localeCompare(b.s.name));
        return out.map(r => r.s);
    };

    // --- Library -----------------------------------------------------------------

    const loadLibrary = () => {
        if (loading) return loading;
        loading = apiFetch('/api/library').then((data) => {
            persistence.ingestSounds(data.sounds); // the cards read colours etc. from here
            const seen = new Set();
            sounds = (data.sounds || []).filter(s => s && s.name && !seen.has(s.id) && seen.add(s.id)).map(s => {
                const key = normalize(s.name);
                return { ...s, key, words: key.split(' ') };
            });
        }).catch((e) => {
            console.warn('Search: could not load the library:', e);
        }).finally(() => { loading = null; });
        return loading;
    };

    // --- Rendering ---------------------------------------------------------------

    const message = (text) => {
        const p = document.createElement('p');
        p.className = 'no-sounds search-message';
        p.textContent = text;
        return p;
    };

    let frame = 0;
    const render = ({ keepScroll = false } = {}) => {
        cancelAnimationFrame(frame);
        frame = requestAnimationFrame(() => {
            if (!open) return;
            const scroll = grid.scrollTop;
            grid.innerHTML = '';
            const query = input.value.trim();
            if (!query && !color) {
                grid.append(message(loading && !sounds.length ? 'Loading your sounds…'
                    : `Type to search ${sounds.length} sound${sounds.length === 1 ? '' : 's'}, or pick a colour.`));
                return;
            }
            const found = results(query);
            if (!found.length) {
                grid.append(message(loading && !sounds.length ? 'Loading your sounds…' : 'No sounds found.'));
                return;
            }
            const fragment = document.createDocumentFragment();
            for (const s of found.slice(0, MAX_RESULTS)) {
                fragment.append(app.createSoundCardElement(s, { animate: false, tabId: 'search' }));
            }
            grid.append(fragment);
            if (found.length > MAX_RESULTS) grid.append(message(`${found.length - MAX_RESULTS} more: type more to narrow it down.`));
            grid.scrollTop = keepScroll ? scroll : 0;
        });
    };

    const renderChips = () => {
        colors.querySelectorAll('.search-color').forEach(chip => {
            const on = chip.dataset.color === color;
            chip.classList.toggle('selected', on);
            chip.setAttribute('aria-checked', String(on));
        });
    };

    // --- The virtual keyboard ----------------------------------------------------
    // --keyboard is how much of the layout the visual viewport doesn't show
    // at the bottom: the keyboard (0 on desktops and while it is closed).

    const isIOS = /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    document.documentElement.classList.toggle('keyboard-plus-inset', !isIOS);
    const vv = window.visualViewport;
    const updateKeyboard = () => {
        let kb = vv ? Math.round(window.innerHeight - vv.height - vv.offsetTop) : 0;
        if (!open || kb < 40) kb = 0; // browser bars, not a keyboard
        document.documentElement.style.setProperty('--keyboard', kb + 'px');
        document.body.classList.toggle('keyboard-open', kb > 0);
    };
    if (vv) {
        vv.addEventListener('resize', updateKeyboard);
        vv.addEventListener('scroll', updateKeyboard);
    }

    // --- Opening and closing ------------------------------------------------------

    const setOpen = (on) => {
        open = on;
        state.searching = on;
        document.body.classList.toggle('search-mode', on);
        topBar?.classList.toggle('search-mode-active', on);
        $('search-mode-info')?.classList.toggle('hidden', !on);
        button?.classList.toggle('active', on);
        panel.hidden = !on;
    };

    // The PC's library changed (app.js): an open search shows the new list.
    document.addEventListener('libraryChanged', () => {
        if (open) loadLibrary().then(() => render({ keepScroll: true }));
    });

    // openSearch shows the search. focus: open the keyboard now (only works
    // in answer to a tap, which the two-finger pull calls later).
    const openSearch = ({ focus = true } = {}) => {
        if (!open) {
            if (typeof editMode !== 'undefined' && editMode.isActive()) $('edit-mode-button')?.click();
            setOpen(true);
            history.pushState({ resoundSearch: true }, '');
            pushedHistory = true;
            input.value = '';
            color = null;
            renderChips();
            grid.scrollTop = 0;
            render();
            loadLibrary().then(() => render({ keepScroll: true }));
        }
        if (focus) input.focus({ preventScroll: true });
    };

    const closeSearch = ({ fromHistory = false } = {}) => {
        if (!open) return;
        input.blur();
        setOpen(false);
        updateKeyboard();
        if (pushedHistory && !fromHistory) history.back();
        pushedHistory = false;
        wrapper.style.transform = ''; // never leave the page pulled down
        grid.innerHTML = '';
        grid.scrollTop = 0;
        app.reloadSoundsForCurrentTab(); // from the cache: at once
    };

    // --- Two-finger pull ------------------------------------------------------------
    // Pulling arms the search (with a tap of vibration); lifting the fingers
    // opens it and focuses the field. Opening on the lift matters twice:
    // opening empties the grid, and a removed card under the fingers would
    // take the touch's end with it (the reset of the pull and the focus were
    // lost); and the lift is what lets a page open the keyboard.

    const setupPull = () => {
        let pull = null; // { y0, x0, armed }
        const avg = (touches, k) => (touches[0][k] + touches[1][k]) / 2;
        const reset = () => {
            wrapper.style.transition = 'transform 0.2s ease';
            wrapper.style.transform = '';
        };
        wrapper.addEventListener('touchstart', (e) => {
            if (e.touches.length !== 2 || open || (typeof editMode !== 'undefined' && editMode.isActive())) return;
            pull = { y0: avg(e.touches, 'clientY'), x0: avg(e.touches, 'clientX'), armed: false };
            wrapper.style.transition = 'none';
        }, { passive: true });
        wrapper.addEventListener('touchmove', (e) => {
            if (!pull || e.touches.length !== 2) return;
            e.preventDefault(); // the pull, not a scroll
            const dy = avg(e.touches, 'clientY') - pull.y0;
            const dx = Math.abs(avg(e.touches, 'clientX') - pull.x0);
            if (dy <= 0 || dx > dy) { wrapper.style.transform = ''; return; }
            wrapper.style.transform = `translateY(${Math.min(dy, 160) * 0.45}px)`; // follows, with resistance
            const armed = dy > PULL_OPEN_PX;
            if (armed && !pull.armed) window.haptics?.tap();
            pull.armed = armed; // pulled back up: not any more
        }, { passive: false });
        const end = () => {
            if (!pull) return;
            const armed = pull.armed;
            pull = null;
            reset();
            if (armed) openSearch(); // focuses the field: the lift may open the keyboard
        };
        wrapper.addEventListener('touchend', end);
        wrapper.addEventListener('touchcancel', end);
    };

    // --- Wiring -----------------------------------------------------------------------

    const init = () => {
        panel = $('search-panel');
        input = $('search-input');
        colors = $('search-colors');
        grid = $('sounds-container');
        wrapper = $('tab-content-wrapper');
        topBar = $('top-bar');
        button = $('search-button');
        if (!panel || !input || !grid || !wrapper) return;

        for (const c of COLORS) {
            const chip = document.createElement('button');
            chip.type = 'button';
            chip.className = `search-color color-swatch ${c}`;
            chip.dataset.color = c;
            chip.setAttribute('role', 'radio');
            chip.setAttribute('aria-label', c === 'default' ? 'No colour' : c[0].toUpperCase() + c.slice(1));
            colors.append(chip);
        }
        colors.addEventListener('click', (e) => {
            const chip = e.target.closest('.search-color');
            if (!chip) return;
            color = color === chip.dataset.color ? null : chip.dataset.color; // one at a time; again: off
            renderChips();
            render();
        });

        button?.addEventListener('click', () => (open ? closeSearch() : openSearch()));
        $('search-close').addEventListener('click', () => closeSearch());
        input.addEventListener('input', () => render());
        input.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') input.blur(); // done typing: the keyboard goes
            if (e.key === 'Escape') closeSearch();
        });
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && open && document.activeElement !== input && !document.querySelector('#sound-settings-overlay:not(.hidden)')) closeSearch();
        });
        window.addEventListener('popstate', () => {
            if (open) { pushedHistory = false; closeSearch({ fromHistory: true }); }
        });

        // Taps on results, the colours and the bottom bar keep the field
        // focused, so the keyboard stays open (focus moves on mousedown).
        document.addEventListener('mousedown', (e) => {
            if (open && document.activeElement === input &&
                e.target.closest('#sounds-container .sound-card, #search-colors, header')) e.preventDefault();
        }, true);
        // Scrolling the results closes the keyboard.
        let scrollY = null;
        grid.addEventListener('touchstart', (e) => { scrollY = e.touches.length === 1 ? e.touches[0].clientY : null; }, { passive: true });
        grid.addEventListener('touchmove', (e) => {
            if (open && scrollY !== null && Math.abs(e.touches[0].clientY - scrollY) > 12 && document.activeElement === input) input.blur();
        }, { passive: true });

        // A sound's settings sheet takes the screen: the keyboard goes; when
        // it closes, the results follow changed colours.
        const overlay = $('sound-settings-overlay');
        if (overlay) {
            let wasOpen = false;
            new MutationObserver(() => {
                const isOpen = !overlay.classList.contains('hidden');
                if (open && isOpen && !wasOpen) input.blur();
                if (open && !isOpen && wasOpen) render({ keepScroll: true });
                wasOpen = isOpen;
            }).observe(overlay, { attributes: true, attributeFilter: ['class'] });
        }

        setupPull();
    };

    document.addEventListener('DOMContentLoaded', init);
    window.soundSearch = { open: openSearch, close: closeSearch, isOpen: () => open };
})();
