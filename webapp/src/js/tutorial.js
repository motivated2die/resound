// The tour (Driver.js): a short walk through the remote, from Settings →
// Tour or the help button in the top bar.
//
// Until the tour was started once (localStorage tutorialCompleted), the help
// button shows in the top bar, and shortly after the app has loaded a callout
// below it offers the tour: Start tour, or Later (asks again next time the
// app is opened; the help button stays).
//
// Each step says whether edit mode and the sound sheet are open for it
// (tourStep), and entering the step sets that, so Next and Previous both
// find the app as the step describes it. Closing the tour leaves edit mode
// and closes the sheet.

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

// tourStep is a step for which edit mode is on (edit) or off, and the sound
// sheet open (sheet) or closed.
const tourStep = ({ edit = false, sheet = false, ...step }) => ({
    ...step,
    onHighlightStarted: () => {
        setEditMode(edit);
        if (sheet) openSoundSettingsPanelVisually();
        else closeSoundSettingsPanelVisually();
    },
});

const tourSteps = () => [
    tourStep({
        popover: { title: 'Welcome to Resound', description: 'A quick tour of your soundboard. It takes about a minute.' },
    }),
    tourStep({
        element: '#sounds-container',
        popover: { title: 'Play sounds', description: 'Tap a button to play its sound on your PC. Sounds can overlap.', side: 'top', align: 'center' },
    }),
    tourStep({
        element: '#stop-all',
        popover: { title: 'Stop', description: 'Stops all sounds at once.', side: 'left', align: 'start' },
    }),
    tourStep({
        element: '#talk-through-button',
        popover: { title: 'Talk-through', description: 'Hold it to pause the sounds and talk. Let go, and they continue.', side: 'top', align: 'center' },
    }),
    tourStep({
        element: '#tabs-container',
        popover: { title: 'Tabs', description: 'Each sound folder is a tab. Tap one, or swipe left and right over the buttons.', side: 'top', align: 'center' },
    }),
    tourStep({
        element: '#search-button',
        popover: { title: 'Search', description: 'Find any sound in all tabs. Shortcut: <b>pull down with two fingers</b> on the buttons.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        element: '#edit-mode-button',
        popover: { title: 'Edit mode', description: 'Tap the pencil to arrange and customize your board. Let\'s look inside.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: '#layout-mode-button',
        popover: { title: 'Layout', description: 'Switches this tab between 3 columns, 2 columns and a list.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: '#sort-mode-button',
        popover: { title: 'Sorting', description: 'Sorts this tab <b>A–Z</b> or <b>Z–A</b>, or back to your own order. Your own order is always kept.', side: 'bottom', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: '#sounds-container',
        popover: { title: 'Move buttons', description: 'In your own order, drag a button onto another to swap them.', side: 'top', align: 'center' },
    }),
    tourStep({
        edit: true,
        element: '#tabs-container',
        popover: { title: 'Move tabs', description: '<b>Long-press a tab</b>, then drag it to a new place.', side: 'top', align: 'center' },
    }),
    tourStep({
        edit: true,
        sheet: true,
        element: '#settings-card',
        popover: { title: 'Customize a sound', description: 'Tap a button in edit mode, or long-press it any time: favorite, volume, color and emoji. Only you hear the preview.', side: 'top', align: 'center' },
    }),
    tourStep({
        popover: { title: 'You\'re all set', description: 'Have fun! The tour is under Settings → <b>Tour</b> whenever you want it again.' },
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
        // Closed or finished: the app as before the tour.
        onDestroyStarted: () => {
            if (!isTutorialRunning) return;
            isTutorialRunning = false;
            setEditMode(false);
            closeSoundSettingsPanelVisually();
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
