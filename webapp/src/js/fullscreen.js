// Fullscreen in a browser tab. The installed app on Android opens
// fullscreen by itself (manifest.json), and iPhones have no fullscreen for
// pages; there the setting is hidden. Elsewhere "Fullscreen" enters
// fullscreen with the next tap, since browsers only allow it in answer to
// one, and again after the app was in the background. It is on by default
// on phones and tablets only (platform.js): on a computer, clicking into the
// window would fill the screen with it.
const fullscreenManager = (() => {
    const SETTING_KEY = 'fullscreen'; // stored once chosen (persistence.js)
    let isEnabled = false;
    let active = false; // init found fullscreen useful here

    const settings = () => (typeof state !== 'undefined' && state.settings) ? state.settings : null;

    const chosen = () => {
        const value = settings()?.[SETTING_KEY];
        return typeof value === 'boolean' ? value : window.platform.mobile;
    };

    // applicable reports whether the page can go fullscreen and isn't already.
    const applicable = () => {
        const el = document.documentElement;
        const canRequest = !!(el.requestFullscreen || el.webkitRequestFullscreen) && document.fullscreenEnabled !== false;
        return canRequest && !window.matchMedia('(display-mode: fullscreen)').matches;
    };

    const requestFullscreen = () => {
        if (!active || !isEnabled || document.fullscreenElement || document.webkitFullscreenElement) return;
        const el = document.documentElement;
        if (el.requestFullscreen) {
            el.requestFullscreen({ navigationUI: 'hide' }).catch(err => console.warn(`Fullscreen refused: ${err.message}`));
        } else if (el.webkitRequestFullscreen) {
            el.webkitRequestFullscreen();
        }
    };

    const exitFullscreen = () => {
        if (document.fullscreenElement && document.exitFullscreen) document.exitFullscreen().catch(() => {});
        else if (document.webkitFullscreenElement && document.webkitExitFullscreen) document.webkitExitFullscreen();
    };

    const save = () => {
        const s = settings();
        if (s) {
            s[SETTING_KEY] = isEnabled;
            persistence.save(s);
        }
    };

    const updateToggleUI = () => {
        const toggle = document.getElementById('auto-fullscreen-toggle');
        if (toggle) toggle.checked = isEnabled;
    };

    const setEnabled = (on) => {
        isEnabled = !!on;
        save();
        updateToggleUI();
        if (isEnabled) requestFullscreen(); else exitFullscreen();
    };

    // Taps in the settings don't enter fullscreen: the toggle does that itself.
    const handleInteraction = (event) => {
        if (event.target.closest('#app-settings-modal') || event.target.closest('#sound-settings-overlay')) return;
        requestFullscreen();
    };

    // After "Reset all settings": back to this device's default.
    const reset = () => {
        isEnabled = chosen();
        updateToggleUI();
        if (!isEnabled) exitFullscreen();
    };

    const init = () => {
        isEnabled = chosen();
        active = applicable();
        const container = document.getElementById('fullscreen-toggle-container');
        if (!active) {
            if (container) container.hidden = true;
            return;
        }
        updateToggleUI();
        const toggle = document.getElementById('auto-fullscreen-toggle');
        if (toggle) toggle.addEventListener('change', () => setEnabled(toggle.checked));
        document.addEventListener('pointerdown', handleInteraction, { capture: true, passive: true });
    };

    const publicInterface = { init, reset, setEnabled, isEnabled: () => isEnabled };
    window.fullscreenManager = publicInterface;
    return publicInterface;
})();

// The settings are loaded once the remote is ready.
document.addEventListener('appReady', () => window.fullscreenManager.init(), { once: true });
