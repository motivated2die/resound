// The top bar's buttons, arranged to stay clear of the phone's camera: in
// fullscreen the bar shares its strip with the notch or hole punch, and browsers
// report how tall that strip is but not where the camera sits. So the user
// chooses (a device preference, persistence.js): in Settings at any time, or in
// the sheet that opens shortly after the first time in fullscreen on Android.
//
// A layout says which groups of buttons stand on which side of the bar. Groups
// keep buttons together that belong together, and buttons move in the DOM, so
// the order of keyboard focus and screen readers is the order on screen. Edit
// mode and search only show or hide buttons (editMode.js, search.js), which
// works the same on either side.
const topBarLayout = (() => {
    const DEFAULT = 'right';

    // In their order with the buttons on the right.
    const GROUPS = {
        playback: ['play-pause-toggle-button'],
        view: ['sort-mode-button', 'layout-mode-button'], // edit mode only
        tools: ['search-button', 'edit-mode-button'],     // the pencil stays beside what it reveals
        app: ['app-settings-button', 'tutorial-help-button'],
    };

    // Split leaves the bar's middle free: at most four buttons on one side,
    // three on the other, even on a narrow phone. mirrored: each group in
    // reverse, so that the whole row reads as the one on the right, mirrored.
    const LAYOUTS = {
        right: { start: [], end: ['playback', 'view', 'tools', 'app'] },
        left: { start: ['app', 'tools', 'view', 'playback'], end: [], mirrored: true },
        split: { start: ['app', 'playback'], end: ['view', 'tools'] },
    };

    const $ = (id) => document.getElementById(id);

    // A side's buttons in the order they stand, by ID.
    const buttons = (id, side) => {
        const layout = LAYOUTS[id];
        return layout[side].flatMap((group) => (layout.mirrored ? [...GROUPS[group]].reverse() : GROUPS[group]));
    };

    const chosen = () => !!LAYOUTS[state.settings?.topBarLayout];
    const current = () => (chosen() ? state.settings.topBarLayout : DEFAULT);

    const syncControl = () => {
        document.querySelectorAll('input[name="top-bar-layout"]').forEach((radio) => {
            radio.checked = radio.value === current();
        });
    };

    // apply arranges the bar; the sheet shows a layout this way before it is chosen.
    const apply = (id = current()) => {
        $('top-bar').dataset.layout = id;
        $('top-bar-start').append(...buttons(id, 'start').map($));
        $('top-bar-end').append(...buttons(id, 'end').map($));
        syncControl();
        document.dispatchEvent(new CustomEvent('topBarLayoutChanged'));
    };

    const choose = (id) => {
        state.settings.topBarLayout = id;
        persistence.save();
        apply(id);
    };

    const init = () => {
        apply();
        document.querySelectorAll('input[name="top-bar-layout"]').forEach((radio) => {
            radio.addEventListener('change', () => { if (radio.checked) choose(radio.value); });
        });
    };

    return { init, apply, choose, chosen, current, buttons };
})();

// The first-run sheet: three small pictures of a phone, one for each place the
// camera can be, with the layout that suits it. Choosing one moves the real bar
// at once, which stays lit above the backdrop, so the user sees it beside their
// own camera before confirming. New users get the tour's offer in the same sheet
// instead of its callout (tutorial.js).
const topBarSetup = (() => {
    const AFTER_FULLSCREEN_MS = 800; // the browser's own transition first
    const AFTER_LOAD_MS = 1500;      // the installed app, fullscreen from the start
    const RETRY_MS = 1000;           // while something else is on screen

    const OPTIONS = [
        { id: 'right', camera: 'left', title: 'Camera left', text: 'Buttons right' },
        { id: 'split', camera: 'middle', title: 'Camera middle', text: 'Buttons on both sides' },
        { id: 'left', camera: 'right', title: 'Camera right', text: 'Buttons left' },
    ];
    // What the pictures show: the buttons that are always there.
    const PICTURED = {
        'search-button': '<span class="icon" data-icon="search"></span>',
        'edit-mode-button': '<span class="icon" data-icon="edit"></span>',
        'app-settings-button': '<span class="icon" data-icon="settings"></span>',
    };

    let el = null;
    let timer = null;
    let gaveUp = false; // fullscreen was expected, but the browser refused it

    const fullscreen = () => window.fullscreenManager;
    const $ = (id) => document.getElementById(id);

    // Only where the camera can be in the bar's way: iPhones and iPads keep the
    // bar below the status bar (platform.js), computers have no camera there.
    const wanted = () => window.platform.mobile && !window.platform.ios && !topBarLayout.chosen();

    // pending tells the tour's callout to wait: this sheet will bring its offer.
    const pending = () => wanted() && !gaveUp && (fullscreen().isFullscreen() || fullscreen().willEnter());

    // Not over anything the user is in the middle of.
    const blocked = () => state.searching || editMode.isActive() || isTutorialRunning
        || $('connection-help').hidden === false
        || !$('install-sheet').hidden
        || ['app-settings-modal-overlay', 'sound-settings-overlay', 'emoji-picker-modal-overlay']
            .some((id) => !$(id).classList.contains('hidden'));

    const picture = ({ id, camera }) => {
        const side = (name) => topBarLayout.buttons(id, name).map((button) => PICTURED[button] || '').join('');
        return `
            <span class="mini-phone" data-camera="${camera}" aria-hidden="true">
                <span class="mini-camera"></span>
                <span class="mini-bar"><span>${side('start')}</span><span>${side('end')}</span></span>
                <span class="mini-grid"><i></i><i></i><i></i><i></i><i></i><i></i></span>
            </span>`;
    };

    const setInert = (on) => {
        $('top-bar').inert = on;
        document.querySelector('.container').inert = on;
    };

    const close = (id) => {
        topBarLayout.choose(id);
        setInert(false);
        document.removeEventListener('keydown', onKey);
        const closing = el;
        el = null;
        closing.classList.remove('open');
        setTimeout(() => closing.remove(), 300);
    };

    const selected = () => el.querySelector('input:checked').value;

    const confirm = () => {
        if (el.querySelector('.top-bar-setup-tour')) tourCallout.later(); // asked again next time, as in the callout
        close(selected());
    };

    const onKey = (e) => { if (e.key === 'Escape') confirm(); };

    const open = () => {
        const offerTour = tourCallout.wanted();
        tourCallout.hide();
        const current = topBarLayout.current();

        el = document.createElement('div');
        el.className = 'install-overlay top-bar-setup';
        el.innerHTML = `
            <div class="install-backdrop"></div>
            <div class="install-card" role="dialog" aria-modal="true" aria-labelledby="top-bar-setup-title" tabindex="-1">
                <div>
                    <h2 id="top-bar-setup-title">Where's your camera?</h2>
                    <p>Pick the top bar that keeps its buttons clear of it. You can change this in Settings.</p>
                </div>
                <div class="top-bar-options" role="radiogroup" aria-labelledby="top-bar-setup-title">
                    ${OPTIONS.map((option) => `
                        <label class="top-bar-option">
                            <input type="radio" name="top-bar-setup" value="${option.id}"${option.id === current ? ' checked' : ''}>
                            ${picture(option)}
                            <strong>${option.title}</strong>
                            <span>${option.text}</span>
                        </label>`).join('')}
                </div>
                ${offerTour ? `
                <p class="top-bar-setup-tour">
                    <span class="icon" data-icon="school" aria-hidden="true"></span>
                    <span>New here? Take a one-minute tour of your soundboard.</span>
                </p>` : ''}
                <div class="install-actions">
                    <button type="button" class="action-button${offerTour ? '' : ' primary'} top-bar-setup-confirm">Confirm</button>
                    ${offerTour ? '<button type="button" class="action-button primary top-bar-setup-tour-start">Confirm &amp; start tour</button>' : ''}
                </div>
            </div>`;
        el.querySelectorAll('input').forEach((radio) => {
            radio.addEventListener('change', () => topBarLayout.apply(radio.value));
        });
        el.querySelector('.top-bar-setup-confirm').addEventListener('click', confirm);
        el.querySelector('.top-bar-setup-tour-start')?.addEventListener('click', () => {
            close(selected());
            handleStartTutorialClick();
        });
        document.body.append(el);
        setInert(true);
        document.addEventListener('keydown', onKey);
        void el.offsetWidth; // start the slide from below
        el.classList.add('open');
        el.querySelector('.install-card').focus({ preventScroll: true });
    };

    const tryOpen = () => {
        clearTimeout(timer);
        if (el || !wanted() || !fullscreen().isFullscreen()) return;
        if (blocked()) timer = setTimeout(tryOpen, RETRY_MS);
        else open();
    };

    const schedule = (delay) => {
        clearTimeout(timer);
        timer = setTimeout(tryOpen, delay);
    };

    const init = () => {
        if (!wanted()) return;
        if (fullscreen().isFullscreen()) schedule(AFTER_LOAD_MS);
        // A tab goes fullscreen with the next tap. Should that not work, the
        // tour's callout that was waiting comes after all.
        document.addEventListener('pointerdown', () => setTimeout(() => {
            if (fullscreen().isFullscreen()) return;
            gaveUp = true;
            tourCallout.show();
        }, AFTER_LOAD_MS), { capture: true, passive: true, once: true });
        ['fullscreenchange', 'webkitfullscreenchange'].forEach((name) => {
            document.addEventListener(name, () => schedule(AFTER_FULLSCREEN_MS));
        });
    };

    return { init, pending };
})();

// fullscreen.js initializes on the same event, and came first.
document.addEventListener('appReady', () => topBarSetup.init(), { once: true });
