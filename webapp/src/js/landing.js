// The landing page (index.html). The installed app opens the soundboard
// (or the connect page while no PC is paired) without showing the page;
// earlier installs start here. In a browser the page stays, and its
// buttons follow what this device can do: open the soundboard when a PC is
// paired, install the app when the browser offers it. A pairing link
// (/#pair&…) is forwarded by rtc.js, loaded before this.
(() => {
    'use strict';

    const rtc = window.resoundRTC;
    const pc = rtc ? rtc.currentPC() : null;
    const installed = window.matchMedia('(display-mode: standalone)').matches ||
        (window.matchMedia('(display-mode: fullscreen)').matches && !document.fullscreenElement) || // not a fullscreened tab
        navigator.standalone === true;

    if (location.hash.startsWith('#pair')) return; // rtc.js is on its way
    if (installed) {
        location.replace(pc ? '/app' : '/connect');
        return;
    }

    // Phones come here to use the soundboard, PCs to download Resound.
    const phone = window.matchMedia('(pointer: coarse)').matches && !/Windows NT/.test(navigator.userAgent);

    let installPrompt = null;
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault(); // offered as a button instead of a banner
        installPrompt = e;
        document.querySelectorAll('[data-install]').forEach((b) => { b.hidden = false; });
    });
    window.addEventListener('appinstalled', () => {
        document.querySelectorAll('[data-install]').forEach((b) => { b.hidden = true; });
    });

    document.addEventListener('DOMContentLoaded', () => {
        // News: the latest two entries stay open, older ones start closed.
        document.querySelectorAll('.news-item').forEach((d, i) => { if (i >= 2) d.open = false; });
        const target = pc ? '/app' : '/connect';
        document.querySelectorAll('[data-open-app]').forEach((a) => { a.href = target; });
        document.querySelectorAll('[data-open-app-label]').forEach((s) => {
            s.textContent = pc ? 'Open soundboard' : 'Connect your phone';
        });
        if (pc) {
            document.querySelectorAll('[data-pc-name]').forEach((s) => { s.textContent = pc.name; });
            document.querySelectorAll('[data-paired-note]').forEach((p) => { p.hidden = false; });
        }

        // On a phone the soundboard comes first; on a PC the download.
        if (phone) {
            document.querySelectorAll('.cta-row').forEach((row) => {
                const open = row.querySelector('[data-open-app]');
                const download = row.querySelector('[data-download]');
                if (!open || !download) return;
                open.classList.replace('btn-secondary', 'btn-primary');
                download.classList.replace('btn-primary', 'btn-secondary');
                row.insertBefore(open.closest('.cta-stack') || open, download); // with its note
            });
        }

        setupDownloadDialog(document.querySelector('[data-download-dialog]'));
        animateDesktopPanel(document.querySelector('[data-desk-panel]'));
        animateRemote(document.querySelector('[data-remote]'));

        document.querySelectorAll('[data-install]').forEach((b) => {
            b.hidden = !installPrompt;
            b.addEventListener('click', async () => {
                if (!installPrompt) return;
                installPrompt.prompt();
                await installPrompt.userChoice.catch(() => {});
                installPrompt = null;
                document.querySelectorAll('[data-install]').forEach((x) => { x.hidden = true; });
            });
        });
    });

    // The next steps after a download: the link still downloads as usual,
    // and the dialog opens over the dimmed page. A click beside its card
    // (on the backdrop, which belongs to the dialog itself) closes it.
    function setupDownloadDialog(dialog) {
        if (!dialog || typeof dialog.showModal !== 'function') return;
        document.querySelectorAll('[data-download]').forEach((a) => {
            a.addEventListener('click', () => { if (!dialog.open) dialog.showModal(); });
        });
        dialog.addEventListener('click', (e) => {
            if (e.target === dialog || e.target.closest('[data-dialog-close]')) dialog.close();
        });
    }

    // The hero's picture of the remote and the PC it controls: its pads play
    // like sounds. Every few seconds a random one plays by itself; a tap plays
    // that one at once and pauses the others for a while. Each play sends
    // waves from the phone, the logo's rings lit from the inside out; as they
    // pass the PC it lights up and passes the sound on: its emoji pops up at
    // each app. The stage is laid out in px and zoomed to its column.
    function animateRemote(stage) {
        if (!stage) return;
        const WIDTH = 380;          // the stage's width in css/landing.css (.rc)
        const PLAY = 2600;          // a pad's progress bar (--play in landing.css)
        const RING_STEP = 55;       // between one ring lighting up and the next
        const RING_FLASH = 520;     // each ring's flash
        const ARRIVE = 300;         // until the waves reach the PC (rings at its distance peak then)
        const EVERY = 3400;         // between plays by themselves
        const RESUME = 7000;        // after a tap, before they start again
        const host = stage.parentElement;
        const tiles = [...stage.querySelectorAll('.tile')];
        const rings = [...stage.querySelectorAll('[data-ring]')]; // inside out
        const pc = stage.querySelector('[data-pc]');
        const apps = [...stage.querySelectorAll('.rc-app')];
        const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

        const fit = () => stage.style.setProperty('--rc-zoom', Math.min(1, host.clientWidth / WIDTH).toFixed(3));
        fit();
        if ('ResizeObserver' in window) new ResizeObserver(fit).observe(host);
        else window.addEventListener('resize', fit);

        const emojiOf = (tile) => tile.querySelector('b').textContent;
        // restart plays a class's animation again, even while it runs.
        const restart = (el, cls) => { el.classList.remove(cls); void el.offsetWidth; el.classList.add(cls); };
        const showEmoji = (emoji) => apps.forEach((app) => { app.querySelector('.rc-pop').textContent = emoji; });

        // Without motion: a still picture, the airhorn playing into every
        // app. A tap swaps the pad and the emoji.
        if (still) {
            stage.classList.add('still');
            tiles[0].classList.add('playing');
            showEmoji(emojiOf(tiles[0]));
            tiles.forEach((tile) => tile.addEventListener('click', () => {
                tiles.forEach((t) => t.classList.toggle('playing', t === tile));
                showEmoji(emojiOf(tile));
            }));
            return;
        }

        const offTimers = new WeakMap();
        const later = (el, ms, fn) => { clearTimeout(offTimers.get(el)); offTimers.set(el, setTimeout(fn, ms)); };

        // The PC lights up and passes the sound on to each app in turn.
        const arrive = (emoji) => {
            restart(pc, 'hit');
            later(pc, 450, () => pc.classList.remove('hit'));
            apps.forEach((app, i) => setTimeout(() => {
                app.querySelector('.rc-pop').textContent = emoji;
                restart(app, 'on');
                later(app, 1000, () => app.classList.remove('on'));
            }, 60 + i * 65));
        };

        // The waves: each ring flashes in turn, from the phone outwards, so a
        // wave runs past the PC, which lights up as it gets there. Plays
        // overlap freely. The arrival has its own timer, so it comes on time
        // even where animations are held back.
        const send = (emoji) => {
            rings.forEach((ring, i) => {
                if (!ring.animate) return;
                ring.animate([{ opacity: 0 }, { opacity: 1, offset: 0.25 }, { opacity: 0 }],
                    { duration: RING_FLASH, delay: i * RING_STEP, easing: 'ease-out' });
            });
            setTimeout(() => arrive(emoji), ARRIVE);
        };

        const press = (tile) => {
            restart(tile, 'playing');
            later(tile, PLAY, () => tile.classList.remove('playing'));
            send(emojiOf(tile));
        };

        // Plays by themselves, only once the page has loaded, while the
        // picture is on screen and not right after a tap.
        let last = null, userUntil = 0, timer = 0, visible = false, ready = false;
        const auto = () => {
            timer = 0;
            if (!visible) return;
            const wait = userUntil - Date.now();
            if (wait > 0) { timer = setTimeout(auto, wait); return; }
            let tile;
            do { tile = tiles[Math.floor(Math.random() * tiles.length)]; } while (tile === last && tiles.length > 1);
            last = tile;
            press(tile);
            timer = setTimeout(auto, EVERY);
        };
        tiles.forEach((tile) => tile.addEventListener('click', () => {
            userUntil = Date.now() + RESUME;
            last = tile;
            press(tile);
        }));
        const run = (on) => {
            visible = on;
            if (on && ready && !timer) timer = setTimeout(auto, 700);
            if (!on && timer) { clearTimeout(timer); timer = 0; }
        };
        pageReady().then(() => { ready = true; run(visible); });
        if ('IntersectionObserver' in window) {
            new IntersectionObserver((entries) => run(entries[0].isIntersecting), { threshold: 0.3 }).observe(stage);
        } else {
            run(true);
        }
    }

    // pageReady resolves once the page has loaded (stylesheets, scripts,
    // images), its fonts are ready and a frame with them has been drawn.
    // Started earlier, on a slow first visit, the hero played while the
    // page was still coming in. In a background tab it waits for the tab to
    // be shown (no frames are drawn there).
    function pageReady() {
        const loaded = document.readyState === 'complete'
            ? Promise.resolve()
            : new Promise((resolve) => window.addEventListener('load', resolve, { once: true }));
        const fonts = document.fonts ? document.fonts.ready.catch(() => {}) : Promise.resolve();
        return Promise.all([loaded, fonts])
            .then(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    }

    // The picture of the desktop panel plays what the panel shows (internal/
    // app/flow.go): first someone talks, so the mic's pipe and "Others hear"
    // follow the voice; then a sound plays, so the mic is muted for it,
    // push-to-talk is held, and the sound flows to both outputs; then a
    // problem: VB-Cable isn't installed, so the cable is marked, the pipes
    // into it go unused and the home page lists it at the top. Meters jump
    // up at once and fall smoothly, like the panel's. The point beside the
    // picture that each phase shows stands out meanwhile.
    function animateDesktopPanel(panel) {
        if (!panel) return;
        const TALK = 4.2, PLAY = 6.4, WARN = 5.4; // seconds per phase
        const FALL = 0.05 / 0.033;            // meter fall per second (flowFall per flowTick)
        const IDLE = [0x4a, 0x4d, 0x57], ACCENT = [0x50, 0xde, 0xa8];
        const PHASES = ['talk', 'play', 'warn']; // in the order of the points

        const visual = panel.parentElement;
        const el = (name) => visual.querySelector(`[data-dp="${name}"]`);
        const meters = {};
        panel.querySelectorAll('[data-meter]').forEach((m) => { meters[m.dataset.meter] = { el: m, shown: 0 }; });
        const pipes = {};
        panel.querySelectorAll('[data-pipe]').forEach((p) => { pipes[p.dataset.pipe] = p; });
        const status = el('status'), progress = el('progress'), bar = progress.firstElementChild, muted = el('muted');
        const held = el('held'), ptt = el('ptt'), missing = el('missing'), legend = el('legend').querySelector('span');
        const boardIcon = el('board-icon'), boardLabel = el('board-label');
        const cableName = el('cable-name'), others = cableName.closest('.dp-unit');
        const points = document.querySelector('[data-dp-points]');
        const pointItems = points ? [...points.children] : [];

        // A pipe lit by level (0..1), or 'held', 'off' or 'broken'
        // (ui.PipeState), drawn by its class.
        const setPipe = (name, level, state = 'on') => {
            const p = pipes[name];
            ['held', 'off', 'broken'].forEach((c) => p.classList.toggle(c, state === c));
            if (state !== 'on') { p.style.color = ''; return; }
            const f = Math.min(1, level * 1.4);
            p.style.color = `rgb(${IDLE.map((c, i) => Math.round(c + (ACCENT[i] - c) * f)).join(',')})`;
        };
        const setMeter = (name, target, dt) => {
            const m = meters[name];
            m.shown = target >= m.shown ? target : Math.max(target, m.shown - FALL * dt);
            m.el.style.setProperty('--level', m.shown.toFixed(3));
            return m.shown;
        };
        let phase = null;
        const setPhase = (next) => {
            if (phase === next) return;
            phase = next;
            const playing = next === 'play', warn = next === 'warn';
            status.textContent = { talk: 'Ready', play: 'Playing', warn: '1 thing needs attention' }[next];
            progress.classList.toggle('on', playing);
            muted.classList.toggle('on', playing);
            held.classList.toggle('on', playing);
            ptt.classList.toggle('on', playing);
            missing.classList.toggle('on', warn);
            visual.classList.toggle('warn', warn);
            others.classList.toggle('missing', warn);
            cableName.textContent = warn ? 'Not installed' : 'CABLE Output';
            boardIcon.dataset.picon = playing ? 'player-stop' : 'player-play';
            boardLabel.textContent = playing ? 'Stop' : 'Test sound';
            legend.textContent = {
                talk: 'Talking: your voice goes into voice chat',
                play: 'Playing a sound: your mic is muted and push-to-talk is held',
                warn: 'A problem: VB-Cable is missing. One click installs it',
            }[next];
            pointItems.forEach((li, i) => li.classList.toggle('on', PHASES[i] === next));
        };

        // Levels on the panel's 0..1 (decibel) scale. A voice: syllables in
        // phrases with short breaks. A sound: loud, a little uneven, with a
        // quick attack and a tail.
        const voice = (t) => {
            const phrase = Math.sin(t * 1.9) + Math.sin(t * 0.7 + 1) > -0.6 ? 1 : 0;
            const syllable = Math.max(0, Math.sin(t * 23) * 0.5 + Math.sin(t * 9.3) * 0.5);
            return phrase * (0.25 + 0.45 * syllable + Math.random() * 0.08);
        };
        const sound = (t, left) => {
            const env = Math.min(1, t / 0.08) * Math.min(1, left / 0.9);
            return env * (0.7 + 0.08 * Math.sin(t * 17) + 0.06 * Math.sin(t * 5.1) + Math.random() * 0.07);
        };

        const frame = (cycle, dt) => {
            const next = cycle < TALK ? 'talk' : cycle < TALK + PLAY ? 'play' : 'warn';
            setPhase(next);
            if (next === 'talk') {
                const mic = setMeter('mic', voice(cycle), dt);
                setMeter('sounds', 0, dt);
                setMeter('others', mic * 0.93, dt);
                setMeter('you', 0, dt);
                setPipe('mic', mic);
                setPipe('shared', mic);
                setPipe('board', meters.sounds.shown);
                setPipe('you', meters.you.shown);
                bar.style.width = '0';
            } else if (next === 'play') {
                const t = cycle - TALK;
                const s = setMeter('sounds', sound(t, PLAY - t), dt);
                setMeter('mic', 0, dt);
                setMeter('others', s * 0.95, dt);
                setMeter('you', s * 0.88, dt);
                setPipe('mic', 0, 'held');
                setPipe('shared', s);
                setPipe('board', s);
                setPipe('you', s);
                bar.style.width = `${Math.min(100, (t / PLAY) * 100).toFixed(1)}%`;
            } else {
                // The voice still reaches the mic's meter, but nothing gets
                // to the cable: others hear nothing.
                setMeter('mic', voice(cycle - TALK - PLAY), dt);
                setMeter('sounds', 0, dt);
                setMeter('others', 0, dt);
                setMeter('you', 0, dt);
                setPipe('mic', 0, 'off');
                setPipe('board', 0, 'off');
                setPipe('shared', 0, 'broken');
                setPipe('you', meters.you.shown);
                bar.style.width = '0';
            }
        };

        // Without motion: one still picture of a sound playing.
        if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
            frame(TALK + 2.5, 1);
            ['sounds', 'others', 'you'].forEach((m) => setMeter(m, 0.72, 1));
            ['board', 'shared', 'you'].forEach((p) => setPipe(p, 0.72));
            return;
        }
        if (points) points.classList.add('cycling');

        // Runs only while the panel is on screen.
        let raf = 0, last = 0, cycle = 0;
        const tick = (now) => {
            const dt = last ? Math.min(0.1, (now - last) / 1000) : 0;
            last = now;
            cycle = (cycle + dt) % (TALK + PLAY + WARN);
            frame(cycle, dt);
            raf = requestAnimationFrame(tick);
        };
        const run = (on) => {
            if (on && !raf) { last = 0; raf = requestAnimationFrame(tick); }
            if (!on && raf) { cancelAnimationFrame(raf); raf = 0; }
        };
        if ('IntersectionObserver' in window) {
            new IntersectionObserver((entries) => run(entries[0].isIntersecting)).observe(panel);
        } else {
            run(true);
        }
    }
})();
