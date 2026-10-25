// The tour (Driver.js): a short walk through the remote, from Settings →
// Tour or the help button in the top bar.
//
// Until the tour was started once (localStorage tutorialCompleted), the help
// button shows in the top bar, and shortly after the app has loaded a callout
// below it offers the tour: Start tour, or Later (asks again next time the
// app is opened; the help button stays).
//
// Each step says what is open for it: search, edit mode, the sound sheet
// (tourStep), and entering the step sets that, so Next and Previous both
// find the app as the step describes it. The tour ends in edit mode, with
// its ✓ pointed out; closing it closes search and the sheet.

let isTutorialRunning = false;
let currentDriverInstance = null;

const TOUR_DONE_KEY = 'tutorialCompleted';
const TOUR_LATER_KEY = 'resound.tourLater'; // sessionStorage: "Later" in the callout

const tourDone = () => {
    try { return localStorage.getItem(TOUR_DONE_KEY) === 'true'; } catch (e) { return false; }
};

const markTourDone = () => {
    try { localStorage.setItem(TOUR_DONE_KEY, 'true'); } catch (e) { /* storage blocked: offered again next time */ }
    document.getElementById('tutorial-help-button')?.classList.add('hidden');
    tourCallout.hide();
};

const safeClick = (selector) => {
    const element = document.querySelector(selector);
    if (element && typeof element.click === 'function') {
        element.click();
        return true;
    }
    console.warn(`Element not found or not clickable: ${selector}`);
    return false;
};

const setEditMode = (on) => {
    if (typeof editMode !== 'undefined' && editMode.isActive() !== on) safeClick('#edit-mode-button');
};

// --- The sound sheet, shown as an example (no sound loaded into it) ---------

const openSoundSettingsPanelVisually = () => {
    const settingsOverlay = document.getElementById('sound-settings-overlay');
    const settingsCard = document.getElementById('settings-card');
    const settingsBackdrop = document.getElementById('settings-backdrop');
    if (!settingsOverlay || !settingsCard || !settingsBackdrop) return false;
    if (!settingsOverlay.classList.contains('hidden')) return true;
    settingsCard.style.transition = 'none';
    settingsBackdrop.style.transition = 'none';
    settingsCard.style.transform = 'translateY(100%)';
    settingsBackdrop.style.opacity = '0';
    settingsOverlay.classList.remove('hidden');
    requestAnimationFrame(() => {
        settingsCard.style.transition = 'transform 0.3s cubic-bezier(0.4, 0, 0.2, 1)';
        settingsBackdrop.style.transition = 'opacity 0.3s ease';
        settingsBackdrop.style.opacity = '1';
        settingsCard.style.transform = 'translateY(0)';
    });
    return true;
};

const closeSoundSettingsPanelVisually = () => {
    const settingsOverlay = document.getElementById('sound-settings-overlay');
    const settingsCard = document.getElementById('settings-card');
    const settingsBackdrop = document.getElementById('settings-backdrop');
    if (!settingsOverlay || !settingsCard || !settingsBackdrop || settingsOverlay.classList.contains('hidden')) return false;
    settingsCard.style.transition = 'transform 0.3s cubic-bezier(0.4, 0, 0.2, 1)';
    settingsBackdrop.style.transition = 'opacity 0.3s ease';
    settingsCard.style.transform = 'translateY(100%)';
    settingsBackdrop.style.opacity = '0';
    setTimeout(() => {
        settingsOverlay.classList.add('hidden');
        settingsCard.style.transform = '';
        settingsCard.style.transition = '';
        settingsBackdrop.style.opacity = '';
        settingsBackdrop.style.transition = '';
    }, 300);
    return true;
};

// --- The callout on the help button (first run) -----------------------------

const tourCallout = (() => {
    let el = null;
    let anchor = null;

    const place = () => {
        if (!el || !anchor) return;
        const r = anchor.getBoundingClientRect();
        if (!r.width) { hide(); return; } // the help button went away
        const margin = 12;
        const width = Math.min(280, window.innerWidth - 2 * margin);
        const center = r.left + r.width / 2;
        const left = Math.max(margin, Math.min(center - width / 2, window.innerWidth - margin - width));
        el.style.width = `${width}px`;
        el.style.left = `${left}px`;
        el.style.top = `${r.bottom + 10}px`;
        el.style.setProperty('--arrow-x', `${center - left}px`);
    };

    const onKey = (e) => { if (e.key === 'Escape') later(); };

    const hide = () => {
        if (!el) return;
        el.remove();
        el = null;
        window.removeEventListener('resize', place);
        document.removeEventListener('keydown', onKey);
    };

    const later = () => {
        try { sessionStorage.setItem(TOUR_LATER_KEY, '1'); } catch (e) { /* asked again on the next load */ }
        hide();
    };

    const show = () => {
        anchor = document.getElementById('tutorial-help-button');
        if (el || !anchor || anchor.classList.contains('hidden') || tourDone() || isTutorialRunning) return;
        try { if (sessionStorage.getItem(TOUR_LATER_KEY)) return; } catch (e) { /* show it */ }
        // Not over the connection help, a search or edit mode.
        if (state.searching || (typeof editMode !== 'undefined' && editMode.isActive())) return;
        if (document.getElementById('connection-help')?.hidden === false) return;

        el = document.createElement('div');
        el.className = 'tour-callout';
        el.setAttribute('role', 'dialog');
        el.setAttribute('aria-labelledby', 'tour-callout-title');
        el.innerHTML = `
            <p class="tour-callout-title" id="tour-callout-title">New here?</p>
            <p class="tour-callout-text">Take a one-minute tour of your soundboard.</p>
            <div class="tour-callout-buttons">
                <button type="button" class="tour-callout-later">Later</button>
                <button type="button" class="tour-callout-start">Start tour</button>
            </div>`;
        el.querySelector('.tour-callout-later').addEventListener('click', later);
        el.querySelector('.tour-callout-start').addEventListener('click', () => {
            hide();
            handleStartTutorialClick();
        });
        document.body.append(el);
        place();
        window.addEventListener('resize', place);
        document.addEventListener('keydown', onKey);
    };

    return { show, hide };
})();

// --- Starting -----------------------------------------------------------------

function initializeTutorialTrigger() {
    const startTutorialButton = document.getElementById('start-tutorial-button');
    const helpButton = document.getElementById('tutorial-help-button');

    if (helpButton) {
        helpButton.classList.toggle('hidden', tourDone());
        helpButton.addEventListener('click', handleStartTutorialClick);
    }
    startTutorialButton?.addEventListener('click', handleStartTutorialClick);

    // The callout, once the board is there; it steps aside for edit mode.
    document.addEventListener('appReady', () => setTimeout(tourCallout.show, 1500), { once: true });
    document.addEventListener('editModeChanged', () => tourCallout.hide());
}

function handleStartTutorialClick() {
    if (isTutorialRunning || document.documentElement.classList.contains('driver-active')) return;
    markTourDone();

    const attemptStartTutorial = () => {
        if (typeof window.driver?.js?.driver === 'function') {
            startTour();
        } else {
            console.error('Driver.js (window.driver.js.driver) is not loaded.');
            isTutorialRunning = false;
        }
    };

    // From the settings: close them first.
    const appSettingsModalOverlay = document.getElementById('app-settings-modal-overlay');
    if (appSettingsModalOverlay && !appSettingsModalOverlay.classList.contains('hidden') && safeClick('#close-app-settings-button')) {
        setTimeout(attemptStartTutorial, 150); // the sheet's animation
    } else {
        attemptStartTutorial();
    }
}

// --- The tour -------------------------------------------------------------------

// tourTargets are invisible boxes over several parts of the app, one per
// step that shows them together (Driver.js highlights one element). A step
// places its box when it starts, once its scene is open; the boxes follow
// when the window changes size.
const tourTargets = (() => {
    const boxes = new Map(); // box -> the selectors it covers
    const place = (box) => {
        const rects = (boxes.get(box) || []).map((s) => document.querySelector(s)?.getBoundingClientRect()).filter((r) => r && r.width && r.height);
        if (!rects.length) return;
        const top = Math.min(...rects.map((r) => r.top));
        const left = Math.min(...rects.map((r) => r.left));
        Object.assign(box.style, {
            top: `${top}px`,
            left: `${left}px`,
            width: `${Math.max(...rects.map((r) => r.right)) - left}px`,
            height: `${Math.max(...rects.map((r) => r.bottom)) - top}px`,
        });
    };
    const placeAll = () => boxes.forEach((_, box) => place(box));
    const create = (selectors) => {
        const box = document.createElement('div');
        box.className = 'tour-target';
        box.setAttribute('aria-hidden', 'true');
        document.body.append(box);
        if (!boxes.size) window.addEventListener('resize', placeAll);
        boxes.set(box, selectors);
        return box;
    };
    const removeAll = () => {
        boxes.forEach((_, box) => box.remove());
        boxes.clear();
        window.removeEventListener('resize', placeAll);
    };
    return { create, place, removeAll };
})();

// setTourScene opens what a step shows: search, edit mode, the sound sheet.
const setTourScene = ({ edit = false, sheet = false, search = false }) => {
    const searchUI = window.soundSearch;
    if (!search && searchUI?.isOpen()) searchUI.close();
    if (search) searchUI?.open({ focus: false }); // no keyboard: the tour goes on
    else setEditMode(edit);
    if (sheet) openSoundSettingsPanelVisually();
    else closeSoundSettingsPanelVisually();
};

// tourStep is a step with its scene ({edit, sheet, search}: see
// setTourScene) and what it highlights: a selector, or a list of them shown
// together. Driver.js finds the element first, then calls the step's
// onHighlightStarted, then measures it: so the scene opens there, and
// elements still hidden until then (the edit mode buttons, the search field)
// are measured open. Entering the step sets the scene, so Next and Previous
// both find the app as described.
const tourStep = ({ edit = false, sheet = false, search = false, element, ...step }) => {
    const box = Array.isArray(element) ? tourTargets.create(element) : null;
    return {
        ...step,
        ...(element ? { element: box || element } : {}),
        onHighlightStarted: () => {
            setTourScene({ edit, sheet, search });
            if (box) tourTargets.place(box);
        },
    };
};

// On the whole grid: in the middle, clear of the tab bar (styles.css).
const MIDDLE = 'soundux-driver-popover tour-popover-middle';

const tourSteps = () => [
    tourStep({
        popover: { title: 'Welcome to Resound', description: 'Here\'s a quick tour of your soundboard and the tricks that make it yours. It takes about a minute.' },
    }),
    tourStep({
        element: '#sounds-container',
        popover: { title: 'Play sounds', description: 'Tap a button to play its sound on your PC, for you and everyone in your voice chat. Tap more buttons to play several sounds at once.', popoverClass: MIDDLE },
    }),
    tourStep({
        element: '#stop-all',
        popover: { title: 'Stop', description: 'Stops every sound that\'s playing right now, on your PC and in voice chat.', side: 'left', align: 'start' },
    }),
    tourStep({
        element: '#talk-through-button',
        popover: { title: 'Talk-through', description: 'Hold it to pause your sounds and talk freely. Let go, and they carry on where they stopped.', side: 'top', align: 'center' },
    }),
    tourStep({
        element: '#tabs-container',
        popover: { title: 'Tabs', description: 'Each sound folder on your PC is a tab down here. Tap a tab, or swipe left and right over the buttons to switch.', side: 'top', align: 'center' },
    }),
    tourStep({
        search: true,
        element: ['#top-bar', '#search-panel'],
        popover: { title: 'Search', description: 'Find any sound in all your tabs by typing part of its name, even with a typo. The colored dots filter by button color. Opens with the magnifier, or <b>pull down with two fingers</b> on the buttons.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: '#top-bar',
        popover: { title: 'Edit mode', description: 'The pencil at the top opens edit mode, where you arrange and customize your board. You\'re in it now: the top bar turns green.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: ['#sort-mode-button', '#layout-mode-button'],
        popover: { title: 'Sorting and layout', description: '<b>Sorting</b> (left) sorts this tab A–Z or Z–A, or back to your own order, which is always kept. <b>Layout</b> (right) switches between 3 columns, 2 columns and a list.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: ['#sounds-container', '#tabs-container'],
        popover: { title: 'Move buttons and tabs', description: '<b>Drag a button</b> onto another to swap them (in your own order). <b>Long-press a tab</b>, then drag it to a new place.', popoverClass: MIDDLE },
    }),
    tourStep({
        edit: true,
        sheet: true,
        element: '#settings-card',
        popover: {
            title: 'Customize a sound',
            description: 'Tap a button in edit mode, or long-press it any time, to make it a favorite, change its volume, color or emoji. Only you hear the preview.',
            popoverClass: 'soundux-driver-popover tour-popover-top', // above the sheet, not over it (styles.css)
        },
    }),
    tourStep({
        edit: true,
        element: '#edit-mode-button',
        popover: { title: 'You\'re all set', description: 'You\'re still in edit mode, so go ahead and try things out. Tap <b>✓</b> to leave it. The tour is under Settings → <b>Tour</b> whenever you want it again.', side: 'bottom', align: 'end' },
    }),
];

function startTour() {
    if (isTutorialRunning || document.documentElement.classList.contains('driver-active')) return;
    if (currentDriverInstance) {
        try { currentDriverInstance.destroy(); } catch (e) { /* ignore */ }
        currentDriverInstance = null;
    }
    const driver = window.driver.js.driver;

    currentDriverInstance = driver({
        stageRadius: 5,
        showProgress: true,
        allowClose: true,
        disableActiveInteraction: true,
        popoverClass: 'soundux-driver-popover',
        doneBtnText: 'Done',
        // Closed or finished: search and the sound sheet close; edit mode
        // stays as it is (the last step leaves the user in it, with ✓
        // pointed out).
        onDestroyStarted: () => {
            if (!isTutorialRunning) return;
            isTutorialRunning = false;
            if (window.soundSearch?.isOpen()) window.soundSearch.close();
            closeSoundSettingsPanelVisually();
            tourTargets.removeAll();
            const instance = currentDriverInstance;
            currentDriverInstance = null;
            try {
                instance?.destroy();
            } catch (e) {
                console.error('Ending the tour:', e);
                document.documentElement.classList.remove('driver-active');
            }
        },
        steps: tourSteps(),
    });

    try {
        isTutorialRunning = true;
        currentDriverInstance.drive();
    } catch (e) {
        console.error('Starting the tour:', e);
        tourTargets.removeAll();
        isTutorialRunning = false;
        try { currentDriverInstance?.destroy(); } catch (err) { /* ignore */ }
        currentDriverInstance = null;
        document.documentElement.classList.remove('driver-active');
    }
}

document.addEventListener('DOMContentLoaded', () => {
    initializeTutorialTrigger();
    // A tour can't survive a reload: no leftovers of one.
    document.documentElement.classList.remove('driver-active');
});
