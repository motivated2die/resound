// The in-game overlay's live demo on the landing page (index.html
// #overlay). It works like the overlay in Resound (internal/app/overlay.go):
// Alt (Resound lets the user pick the key) slides a bar of favorites in at
// the left edge; the wheel moves the highlight and stops at the ends; Alt
// again "plays" the highlighted sound (the highlight flashes green); Esc
// closes it, held for 700 ms it "stops all sounds" (the bar turns red
// meanwhile). While it is open, the wheel and those keys are kept from the
// page; a large close button and a click beside the bar close it too.
//
// On touch screens a swipe moves the highlight instead of the page, and
// swiping on past either end of the list closes the bar.
(() => {
    'use strict';

    // The phone demonstrator's sounds (index.html .mock-grid).
    const SOUNDS = [
        { emoji: '📯', name: 'Airhorn', color: 'red' },
        { emoji: '🥁', name: 'Ba dum tss', color: '' },
        { emoji: '👏', name: 'Applause', color: 'blue' },
        { emoji: '😂', name: 'Laugh track', color: 'yellow' },
        { emoji: '🎺', name: 'Sad trombone', color: 'purple' },
        { emoji: '🦆', name: 'Quack', color: 'green' },
        { emoji: '💥', name: 'Boom', color: 'orange' },
        { emoji: '🎉', name: 'Victory', color: 'pink' },
    ];
    const HOLD_MS = 700;     // Esc held this long stops all sounds
    const FLASH_MS = 150;    // the green flash before the bar slides out
    const CLOSE_MS = 100;    // the slide out
    const WHEEL_NOTCH = 50;  // a delta this large is a mouse wheel's notch: one step
    const WHEEL_STEP = 80;   // smaller deltas (touchpads, precise wheels) add up to a step
    const SWIPE_STEP = 36;   // finger travel per step
    const SWIPE_OUT = 70;    // finger travel past an end that closes the bar

    const touch = window.matchMedia('(pointer: coarse)').matches;
    let live, bar, items, status, trigger;
    let open = false;
    let flashing = false; // a sound "plays": the bar ignores input until it is gone
    let sel = Math.floor((SOUNDS.length - 1) / 2); // the middle one first, then where it was left
    let holdTimer = 0, closeTimer = 0, statusTimer = 0;
    let wheel = 0;
    let altAlone = false; // Alt is down and no other key came with it

    document.addEventListener('DOMContentLoaded', () => {
        trigger = document.querySelector('[data-ovl-open]');
        status = document.querySelector('[data-ovl-status]');
        if (!trigger) return;
        build();
        if (touch) { // no Alt key to press: the box is the way in
            document.querySelector('[data-ovl-key]')?.remove();
            const label = document.querySelector('[data-ovl-label]');
            if (label) label.innerHTML = '<strong>Try it now:</strong> tap here to bring up the overlay.';
        }
        trigger.setAttribute('aria-expanded', 'false');
        trigger.addEventListener('click', () => (open ? close() : show()));
        document.addEventListener('keydown', onKeyDown, true);
        document.addEventListener('keyup', onKeyUp, true);
        window.addEventListener('blur', () => { altAlone = false; stopHold(); });
        window.addEventListener('wheel', onWheel, { passive: false });
        document.addEventListener('touchstart', onTouchStart, { passive: true });
        document.addEventListener('touchmove', onTouchMove, { passive: false });
    });

    // build adds the live overlay to the page, hidden.
    function build() {
        live = document.createElement('div');
        live.className = 'ovl-live';
        live.hidden = true;
        live.setAttribute('role', 'dialog');
        live.setAttribute('aria-label', 'In-game overlay demo');
        const shade = document.createElement('div');
        shade.className = 'ovl-shade';
        shade.addEventListener('click', () => close());
        const ovb = document.createElement('div');
        ovb.className = 'ovb';
        bar = document.createElement('div');
        bar.className = 'ovb-bar';
        bar.setAttribute('role', 'listbox');
        bar.setAttribute('aria-label', 'Favorites');
        items = SOUNDS.map((s, i) => {
            const item = document.createElement('span');
            item.className = 'ovb-item' + (s.color ? ' ' + s.color : '');
            item.setAttribute('role', 'option');
            const icon = document.createElement('b');
            icon.textContent = s.emoji;
            const name = document.createElement('span');
            name.className = 'ovb-name';
            name.textContent = s.name;
            item.append(icon, name);
            item.addEventListener('click', () => (i === sel ? play() : select(i)));
            bar.append(item);
            return item;
        });
        const closeBtn = document.createElement('button');
        closeBtn.type = 'button';
        closeBtn.className = 'ovl-close';
        closeBtn.setAttribute('aria-label', 'Close the overlay');
        closeBtn.innerHTML = '<span class="icon" data-icon="close" aria-hidden="true"></span>';
        closeBtn.addEventListener('click', () => close());
        ovb.append(bar, closeBtn);
        // What to do now, at the bottom of the window.
        const tip = document.createElement('div');
        tip.className = 'ovl-tip';
        tip.innerHTML = touch
            ? '<span><strong>Swipe</strong> to pick a sound, <strong>tap it again</strong> to play it. Swipe past the end to close.</span>'
            : '<kbd class="kbd-big">Alt</kbd><span><strong>Press Alt</strong> to play the highlighted sound, or <strong>scroll</strong> to pick another. <kbd>Esc</kbd> closes, held it stops all sounds.</span>';
        live.append(shade, ovb, tip);
        document.body.append(live);
        render();
    }

    function render() {
        items.forEach((item, i) => {
            item.classList.toggle('on', i === sel);
            item.classList.remove('ok');
            item.setAttribute('aria-selected', String(i === sel));
        });
    }

    function select(i) {
        if (flashing) return;
        const next = Math.max(0, Math.min(SOUNDS.length - 1, i)); // stops at the ends
        if (next !== sel) {
            sel = next;
            render();
        }
    }

    function show() {
        if (open) return;
        clearTimeout(closeTimer);
        stopHold();
        open = true;
        wheel = 0;
        render();
        live.classList.remove('closing', 'holding');
        live.hidden = false;
        void live.offsetWidth; // start the slide from the edge
        live.classList.add('open');
        trigger.setAttribute('aria-expanded', 'true');
        say('');
    }

    function close(after) {
        if (!open) return;
        open = false;
        stopHold();
        live.classList.add('closing');
        live.classList.remove('open');
        trigger.setAttribute('aria-expanded', 'false');
        clearTimeout(closeTimer);
        closeTimer = setTimeout(() => {
            live.hidden = true;
            live.classList.remove('closing', 'holding');
            render();
            if (after) after();
        }, CLOSE_MS);
    }

    // play is the overlay key pressed again: the highlight flashes, the bar
    // slides out and the page says what would have played.
    function play() {
        if (!open) return;
        if (flashing) return;
        flashing = true;
        items[sel].classList.add('ok');
        const s = SOUNDS[sel];
        setTimeout(() => { flashing = false; close(); }, FLASH_MS);
        say(`${s.emoji} ${s.name} plays. In Resound, your friends would hear it now.`);
    }

    function stopAll() {
        if (flashing) return;
        close();
        say('■ All sounds stopped.', true);
    }

    function say(text, bad) {
        if (!status) return;
        clearTimeout(statusTimer);
        status.textContent = text;
        status.classList.toggle('bad', !!bad);
        if (text) statusTimer = setTimeout(() => { status.textContent = ''; }, 4000);
    }

    function stopHold() {
        clearTimeout(holdTimer);
        holdTimer = 0;
        live?.classList.remove('holding');
    }

    // --- keys -----------------------------------------------------------------

    // Alt counts when it is pressed and let go alone, so Alt+Tab and other
    // combinations still work as usual.
    function onKeyDown(e) {
        if (e.key === 'Alt') {
            if (!e.repeat) altAlone = true;
            e.preventDefault();
            return;
        }
        altAlone = false;
        if (!open) return;
        switch (e.key) {
        case 'Escape':
            e.preventDefault();
            e.stopPropagation();
            if (!e.repeat && !holdTimer) {
                live.classList.add('holding');
                holdTimer = setTimeout(() => { holdTimer = 0; stopAll(); }, HOLD_MS);
            }
            break;
        case 'ArrowUp':
        case 'ArrowDown':
            e.preventDefault();
            select(sel + (e.key === 'ArrowDown' ? 1 : -1));
            break;
        case 'Enter':
            e.preventDefault();
            play();
            break;
        case ' ':
        case 'PageUp':
        case 'PageDown':
        case 'Home':
        case 'End':
            e.preventDefault(); // the page stays put while the bar is open
            break;
        }
    }

    function onKeyUp(e) {
        if (e.key === 'Alt') {
            e.preventDefault(); // Firefox would show its menu bar
            if (altAlone) (open ? play : show)();
            altAlone = false;
            return;
        }
        if (e.key === 'Escape' && holdTimer) { // let go before the hold: just close
            e.preventDefault();
            stopHold();
            close();
        }
    }

    // --- wheel and touch -------------------------------------------------------

    function onWheel(e) {
        if (!open) return;
        e.preventDefault();
        const px = e.deltaMode === 1 ? e.deltaY * 40 : e.deltaMode === 2 ? e.deltaY * 400 : e.deltaY;
        if (Math.abs(px) >= WHEEL_NOTCH) { // one notch, one step, whatever the scroll speed setting
            wheel = 0;
            select(sel + Math.sign(px));
            return;
        }
        wheel += px;
        while (Math.abs(wheel) >= WHEEL_STEP) {
            const dir = Math.sign(wheel);
            select(sel + dir);
            wheel -= dir * WHEEL_STEP;
        }
    }

    let touchY = null, touchMoved = 0, past = 0;

    function onTouchStart(e) {
        if (!open || e.touches.length !== 1) { touchY = null; return; }
        touchY = e.touches[0].clientY;
        touchMoved = 0;
        past = 0;
    }

    // A swipe up moves the highlight down the list, as the page would
    // scroll; on past the end, the bar closes.
    function onTouchMove(e) {
        if (!open || touchY === null) return;
        e.preventDefault();
        const y = e.touches[0].clientY;
        touchMoved += touchY - y;
        touchY = y;
        while (Math.abs(touchMoved) >= SWIPE_STEP) {
            const dir = Math.sign(touchMoved);
            touchMoved -= dir * SWIPE_STEP;
            const atEnd = dir > 0 ? sel === SOUNDS.length - 1 : sel === 0;
            if (atEnd) {
                past += SWIPE_STEP;
                if (past >= SWIPE_OUT) {
                    touchY = null;
                    close();
                    return;
                }
            } else {
                past = 0;
                select(sel + dir);
            }
        }
    }
})();
