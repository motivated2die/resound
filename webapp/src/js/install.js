// Installing the app: a pill in the top bar ("Install app") while this
// browser can install it, and a sheet with why and how.
//
// Whether a page can be installed isn't something browsers report directly:
//   - Chromium (Chrome, Edge, Samsung Internet; Android and desktop) fires
//     beforeinstallprompt when it can, and never once the app is installed:
//     one tap installs. Without the event (installed already, or not
//     installable) there is no pill.
//   - iPhone and iPad (Safari, and since iOS 16.4 other browsers there) have
//     no event: Share, then Add to Home Screen. The installed app keeps its
//     own storage, so it is paired again there.
//   - Firefox on Android: its menu, then Install. Safari on macOS 17+: File,
//     then Add to Dock.
//   - Never in the installed app itself, in apps' built-in browsers (they
//     can't install), or in desktop Firefox (no web apps).
// "Not now" hides the pill for a while; Settings → "Install the app" opens
// the sheet any time.
(() => {
    'use strict';

    const DISMISS_KEY = 'installPromptDismissedAt';
    const DISMISS_DAYS = 30;

    const ua = navigator.userAgent;
    const isIOS = /iPhone|iPad|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    const isAndroid = /Android/.test(ua);
    const inAppBrowser = /; wv\)|FBAN|FBAV|Instagram|Line\/|Snapchat|musical_ly|TikTok/i.test(ua);
    const macSafariVersion = (() => {
        if (isIOS || !/Macintosh/.test(ua) || /Chrome|Chromium|Edg|Firefox/.test(ua)) return 0;
        const m = /Version\/(\d+)[\d.]* Safari/.exec(ua);
        return m ? Number(m[1]) : 0;
    })();

    // standalone: the installed app. A browser tab the page put into
    // fullscreen (the Fullscreen setting, fullscreen.js) also matches
    // display-mode: fullscreen in Chromium, but has a fullscreenElement; the
    // installed app's fullscreen (manifest) has none.
    const standalone = () =>
        ['standalone', 'minimal-ui'].some(m => window.matchMedia(`(display-mode: ${m})`).matches) ||
        (window.matchMedia('(display-mode: fullscreen)').matches && !document.fullscreenElement && !document.webkitFullscreenElement) ||
        navigator.standalone === true;

    let installPrompt = null; // Chromium's beforeinstallprompt event

    // STEPS: how to install where no button can, as [title, text].
    const STEPS = {
        ios: [
            ['Tap Share', 'The square with an arrow: at the bottom in Safari, in the address bar in other browsers.'],
            ['Choose “Add to Home Screen”', 'Scroll down the list if you don’t see it, then tap Add.'],
            ['Open Resound and pair once more', 'Start it from your home screen and scan the QR code on your PC: the app keeps its own pairing.'],
        ],
        firefox: [
            ['Open the menu', 'The three dots next to the address bar.'],
            ['Choose “Install”', 'Or “Add to Home screen”, then confirm.'],
        ],
        macSafari: [
            ['Open the File menu', 'In the menu bar at the top of the screen.'],
            ['Choose “Add to Dock”', 'Resound then opens in a window of its own.'],
        ],
    };

    // support tells how this browser installs the app: 'prompt' (one tap),
    // a STEPS key, or null (not here).
    const support = () => {
        if (standalone() || inAppBrowser) return null;
        if (installPrompt) return 'prompt';
        if (isIOS) return 'ios';
        if (isAndroid && /Firefox\//.test(ua)) return 'firefox';
        if (macSafariVersion >= 17) return 'macSafari';
        return null;
    };

    const settings = () => (typeof state !== 'undefined' && state.settings) ? state.settings : persistence.load();
    const dismissedRecently = () => {
        const at = Number(settings()[DISMISS_KEY]) || 0;
        return Date.now() - at < DISMISS_DAYS * 24 * 60 * 60 * 1000;
    };
    const dismiss = () => {
        const s = settings();
        s[DISMISS_KEY] = Date.now();
        persistence.save(s);
    };

    const $ = (id) => document.getElementById(id);

    // refresh shows the pill and the settings button where installing works.
    const refresh = () => {
        const mode = support();
        const pill = $('install-pill');
        if (pill) pill.hidden = !mode || dismissedRecently();
        const button = $('install-app-button'); // Settings → PC
        if (button) button.hidden = !mode;
    };

    // --- The sheet -------------------------------------------------------------

    const renderSteps = (mode) => {
        const box = $('install-steps');
        const list = $('install-steps-list');
        const steps = STEPS[mode];
        box.hidden = !steps;
        list.textContent = '';
        for (const [title, text] of steps || []) {
            const li = document.createElement('li');
            const strong = document.createElement('strong');
            strong.textContent = title;
            const span = document.createElement('span');
            span.textContent = text;
            li.append(strong, span);
            list.append(li);
        }
    };

    const openSheet = () => {
        const mode = support();
        if (!mode) return;
        renderSteps(mode);
        $('install-confirm').textContent = mode === 'prompt' ? 'Install' : 'Got it';
        const overlay = $('install-sheet');
        overlay.hidden = false;
        void overlay.offsetWidth; // start the slide from below
        overlay.classList.add('open');
    };

    const closeSheet = () => {
        const overlay = $('install-sheet');
        if (!overlay || overlay.hidden) return;
        overlay.classList.remove('open');
        setTimeout(() => { if (!overlay.classList.contains('open')) overlay.hidden = true; }, 300);
    };

    const confirm = async () => {
        if (support() !== 'prompt') { closeSheet(); return; } // the steps are done by hand
        const prompt = installPrompt;
        installPrompt = null; // usable once
        closeSheet();
        prompt.prompt();
        await prompt.userChoice.catch(() => {});
        refresh();
    };

    // --- Wiring ----------------------------------------------------------------

    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault(); // offered in the top bar instead of the browser's banner
        installPrompt = e;
        refresh();
    });
    window.addEventListener('appinstalled', () => {
        installPrompt = null;
        closeSheet();
        refresh();
    });

    document.addEventListener('DOMContentLoaded', () => {
        $('install-pill')?.addEventListener('click', openSheet);
        $('install-app-button')?.addEventListener('click', () => {
            $('close-app-settings-button')?.click(); // the sheet replaces the settings
            openSheet();
        });
        $('install-not-now')?.addEventListener('click', () => {
            dismiss();
            closeSheet();
            refresh();
        });
        $('install-confirm')?.addEventListener('click', confirm);
        $('install-sheet')?.querySelector('.install-backdrop')?.addEventListener('click', closeSheet);
        refresh();
    });
})();
