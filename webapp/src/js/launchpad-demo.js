// The launchpad's live demo on the landing page (index.html #launchpad),
// and the tabs that switch between it and the overlay's (js/overlay-demo.js).
//
// It works like the launchpad in Resound (internal/app/launchpad.go): while
// Alt is held, the phone's buttons show the keys that play them, 1 to 6
// here; pressing one plays that button (the hero's pad animation,
// landing.js), and letting go of Alt takes the keys away. Alt is shared with
// the overlay's demo: held for HOLD_MS, or with a digit, it is the
// launchpad's (tookAlt); a short tap opens the overlay. Without a keyboard,
// the box holds the key until it is tapped again. The buttons only play by
// their keys, the point of the launchpad: clicked, the Alt key glows
// instead (without a keyboard, the box, unless it holds the key).
//
// The demos are two cards on top of each other, so they take the same
// height. While nobody plays with them, has the mouse over a card or the
// section out of view, the tabs above switch to the other one every
// CYCLE_MS, the selected tab's line filling up meanwhile. Using a demo's key
// shows its card.
(() => {
    'use strict';

    const HOLD_MS = 280;      // Alt held this long is the launchpad's, not a tap
    const CYCLE_MS = 6500;    // each demo, while nobody plays with them
    const PLAY_MS = 2600;     // a pad's progress bar (--play in css/landing.css)

    const touch = window.matchMedia('(pointer: coarse)').matches;
    const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    let section, tabs, cards, pads, tiles, box, key, label;
    let current = 'launchpad';
    let held = false;      // the keys show
    let altDown = false;
    let tookAlt = false;   // this Alt press was the launchpad's
    let altTimer = 0;
    const playTimers = new WeakMap();

    window.launchpadDemo = { tookAlt: () => tookAlt };

    document.addEventListener('DOMContentLoaded', () => {
        section = document.getElementById('overlay');
        pads = document.querySelector('[data-lp-pads]');
        box = document.querySelector('[data-lp-hold]');
        if (!section || !pads || !box) return;
        tabs = [...section.querySelectorAll('[data-demo-tab]')];
        cards = [...section.querySelectorAll('[data-demo]')];
        tiles = [...pads.querySelectorAll('.tile')];
        label = document.querySelector('[data-lp-label]');
        key = document.querySelector('[data-lp-key]');

        if (touch) { // no Alt key to hold: the box holds it
            key?.remove();
            key = null;
            setLabel();
        }
        box.addEventListener('click', () => (touch ? setHeld(!held) : hint()));
        tiles.forEach((tile, i) => tile.addEventListener('click', () => {
            if (touch && held) play(i);
            else hint();
            cycle.touched();
        }));
        document.addEventListener('keydown', onKeyDown, true);
        document.addEventListener('keyup', onKeyUp, true);
        window.addEventListener('blur', letGo);
        document.addEventListener('ovl-demo', (e) => {
            if (e.detail.open) select('overlay');
            cycle.sync();
        });

        tabs.forEach((tab) => {
            tab.addEventListener('click', () => { select(tab.dataset.demoTab); cycle.touched(); });
            tab.addEventListener('keydown', onTabKey);
        });
        cards.forEach((card) => card.addEventListener('pointerdown', () => cycle.touched()));
        select(location.hash === '#overlay' ? 'overlay' : 'launchpad', true); // the overlay's help article links to #overlay
        window.addEventListener('hashchange', () => {
            if (location.hash === '#launchpad' || location.hash === '#overlay') select(location.hash.slice(1));
        });
        cycle.init();
    });

    // --- the launchpad ---------------------------------------------------------

    function setHeld(on) {
        if (on === held) return;
        held = on;
        pads.classList.toggle('held', on);
        box.setAttribute('aria-pressed', String(on));
        if (on) select('launchpad');
        setLabel();
        cycle.sync();
    }

    function setLabel() {
        if (!touch || !label) return;
        label.innerHTML = held
            ? '<strong>Holding the key.</strong> Tap a button to play it, or tap here to let go.'
            : '<strong>Try it now:</strong> tap here to hold the launchpad key.';
    }

    // play plays the i-th button as its key would: the pad's animation, and
    // its key flashing while the keys show.
    function play(i) {
        const tile = tiles[i];
        if (!tile) return;
        tile.classList.remove('playing');
        void tile.offsetWidth; // restart the animation, even while it runs
        tile.classList.add('playing');
        clearTimeout(playTimers.get(tile));
        playTimers.set(tile, setTimeout(() => tile.classList.remove('playing'), PLAY_MS));
        const key = tile.querySelector('.lp-key');
        if (held && key) {
            key.classList.remove('hit');
            void key.offsetWidth;
            key.classList.add('hit');
        }
    }

    // hint points at the key that plays the buttons, when one is clicked
    // instead: the Alt key glows (without a keyboard, the box).
    function hint() {
        const el = key || box;
        el.classList.remove('hint');
        void el.offsetWidth; // again, even while it glows
        el.classList.add('hint');
    }

    // digit is the button a key plays: 1 to 6, above the letters or on the
    // numpad; -1 for other keys.
    function digit(e) {
        const m = /^(?:Digit|Numpad)([1-6])$/.exec(e.code || '');
        return m ? Number(m[1]) - 1 : -1;
    }

    // --- keys -------------------------------------------------------------------

    function onKeyDown(e) {
        const overlayOpen = window.overlayDemo?.isOpen();
        if (e.key === 'Alt') {
            if (!e.repeat && !altDown) {
                altDown = true;
                tookAlt = false;
                clearTimeout(altTimer);
                if (!overlayOpen) altTimer = setTimeout(take, HOLD_MS);
            }
            return; // overlay-demo.js keeps Alt from the page
        }
        if (!altDown || overlayOpen) return;
        const i = digit(e);
        if (i < 0 && !held) return;
        e.preventDefault(); // Alt + a digit switches browser tabs in some browsers
        e.stopPropagation();
        if (i < 0 || e.repeat) return;
        take();
        play(i);
    }

    function onKeyUp(e) {
        if (e.key !== 'Alt') return;
        altDown = false;
        clearTimeout(altTimer);
        if (tookAlt) setHeld(false);
        cycle.touched();
    }

    // take makes the Alt held now the launchpad's.
    function take() {
        clearTimeout(altTimer);
        if (!altDown) return;
        tookAlt = true;
        setHeld(true);
    }

    function letGo() {
        altDown = false;
        clearTimeout(altTimer);
        if (tookAlt) setHeld(false);
    }

    // --- tabs -------------------------------------------------------------------

    function select(name, quiet) {
        if (!cards.some((c) => c.dataset.demo === name)) return;
        if (name !== current && held && name !== 'launchpad') setHeld(false);
        current = name;
        tabs.forEach((tab) => {
            const on = tab.dataset.demoTab === name;
            tab.setAttribute('aria-selected', String(on));
            tab.tabIndex = on ? 0 : -1;
        });
        cards.forEach((card) => {
            const on = card.dataset.demo === name;
            card.classList.toggle('on', on);
            card.toggleAttribute('inert', !on);
            card.setAttribute('aria-hidden', String(!on));
        });
        if (!quiet) cycle.restart();
    }

    // Arrow keys move between the tabs, as in any tab list.
    function onTabKey(e) {
        if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
        e.preventDefault();
        const i = tabs.findIndex((t) => t.dataset.demoTab === current);
        const next = tabs[(i + (e.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
        select(next.dataset.demoTab);
        next.focus();
        cycle.touched();
    }

    // cycle switches to the other demo when the selected tab's line has
    // filled up (a CSS animation of CYCLE_MS), which stands still while the
    // section is out of view, hovered or focused, or a demo is in use. Any
    // use starts the line again from the beginning.
    const cycle = (() => {
        let list, visible = false, hovered = false, focused = false;
        const busy = () => held || altDown || !!window.overlayDemo?.isOpen();
        return {
            init() {
                list = section.querySelector('[data-demo-tabs]');
                if (!list) return;
                list.style.setProperty('--demo-cycle', CYCLE_MS + 'ms');
                if (still) return; // no switching by itself: the line stays full
                list.classList.add('cycling');
                tabs.forEach((tab) => tab.querySelector('.demo-tab-fill')?.addEventListener('animationend', () => {
                    if (tab.dataset.demoTab !== current) return;
                    const next = tabs[(tabs.indexOf(tab) + 1) % tabs.length];
                    select(next.dataset.demoTab);
                }));
                // The mouse over a card holds it, not over the tabs or beside the cards.
                cards.forEach((card) => {
                    card.addEventListener('pointerenter', (e) => { if (e.pointerType === 'mouse') { hovered = true; this.sync(); } });
                    card.addEventListener('pointerleave', () => { hovered = false; this.sync(); });
                });
                // Keyboard focus only: a clicked tab keeps the focus, but not the line.
                section.addEventListener('focusin', (e) => { focused = e.target.matches(':focus-visible'); this.sync(); });
                section.addEventListener('focusout', (e) => {
                    if (!section.contains(e.relatedTarget)) { focused = false; this.sync(); }
                });
                if ('IntersectionObserver' in window) {
                    new IntersectionObserver(([entry]) => { visible = entry.isIntersecting; this.sync(); }, { threshold: 0.5 }).observe(section);
                } else {
                    visible = true;
                }
                this.restart();
            },
            // sync holds the line while it shouldn't fill.
            sync() {
                list?.classList.toggle('paused', !visible || hovered || focused || busy());
            },
            // restart fills the selected tab's line from the beginning.
            restart() {
                if (!list?.classList.contains('cycling')) return;
                tabs.forEach((tab) => {
                    const fill = tab.querySelector('.demo-tab-fill');
                    if (!fill) return;
                    fill.style.animation = 'none';
                    void fill.offsetWidth;
                    fill.style.animation = '';
                });
                this.sync();
            },
            touched() { this.restart(); },
        };
    })();
})();
