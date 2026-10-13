// What the app gets from the device and the browser besides the screen
// (screen-wake.js) and fullscreen (fullscreen.js):
//   - a short vibration when a sound is played or stopped (Vibration API;
//     Android), "Vibrate on Tap" in the settings, on by default;
//   - "Install the app" in the settings while the browser offers it;
//   - persistent storage, so the browser never clears the pairing keys to
//     free space (the paired PCs would be lost).
(() => {
    'use strict';

    const KEY = 'haptics';
    const canVibrate = typeof navigator.vibrate === 'function';
    let vibrate = true;

    const settings = () => (typeof state !== 'undefined' && state.settings) ? state.settings : null;

    window.haptics = {
        tap: () => {
            if (canVibrate && vibrate) navigator.vibrate(12);
        },
    };

    // The browser offers installation (Chromium) once the page qualifies.
    let installPrompt = null;
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault(); // offered in the settings instead of a banner
        installPrompt = e;
        const button = document.getElementById('install-app-button');
        if (button) button.hidden = false;
    });
    window.addEventListener('appinstalled', () => {
        installPrompt = null;
        const button = document.getElementById('install-app-button');
        if (button) button.hidden = true;
    });

    document.addEventListener('DOMContentLoaded', () => {
        vibrate = (settings() || persistence.load())[KEY] !== false;
        const container = document.getElementById('haptics-toggle-container');
        const toggle = document.getElementById('haptics-toggle');
        if (canVibrate && container && toggle) {
            container.hidden = false;
            toggle.checked = vibrate;
            toggle.addEventListener('change', () => {
                vibrate = toggle.checked;
                const s = settings();
                if (s) {
                    s[KEY] = vibrate;
                    persistence.save(s);
                }
                window.haptics.tap();
            });
        }

        const install = document.getElementById('install-app-button');
        if (install) {
            install.hidden = !installPrompt;
            install.addEventListener('click', async () => {
                if (!installPrompt) return;
                installPrompt.prompt();
                await installPrompt.userChoice.catch(() => {});
                installPrompt = null;
                install.hidden = true;
            });
        }
    });

    if (navigator.storage && navigator.storage.persist) {
        navigator.storage.persisted()
            .then((persisted) => persisted || navigator.storage.persist())
            .catch(() => {});
    }
})();
