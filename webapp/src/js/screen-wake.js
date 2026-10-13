// Keeps the screen on while the remote is open (Screen Wake Lock API). On by
// default; "Keep Screen On" in the settings turns it off. The browser drops
// the lock whenever the app is hidden, so it is requested again each time
// the app is visible. Some browsers grant it only after a tap: a refused
// request is retried on the next one.
const screenWakeLock = (() => {
    const KEY = 'keepScreenOn';
    const supported = 'wakeLock' in navigator;
    let lock = null;
    let wanted = true;
    let retryArmed = false;

    // The remote's settings object (app.js), once it has loaded it.
    const settings = () => (typeof state !== 'undefined' && state.settings) ? state.settings : null;

    const retryOnTap = () => {
        if (retryArmed) return;
        retryArmed = true;
        document.addEventListener('pointerdown', () => { retryArmed = false; acquire(); }, { once: true, capture: true });
    };

    const acquire = async () => {
        if (!supported || !wanted || lock || document.visibilityState !== 'visible') return;
        try {
            const l = await navigator.wakeLock.request('screen');
            lock = l;
            l.addEventListener('release', () => { if (lock === l) lock = null; });
        } catch (e) {
            console.warn('Wake lock refused:', e.name, e.message);
            retryOnTap();
        }
    };

    const release = () => {
        const l = lock;
        lock = null;
        if (l) l.release().catch(() => {});
    };

    const setWanted = (on) => {
        wanted = on;
        const s = settings();
        if (s) {
            s[KEY] = on;
            persistence.save(s);
        }
        if (on) acquire(); else release();
    };

    const init = () => {
        const toggle = document.getElementById('wake-lock-toggle');
        const label = document.getElementById('wake-lock-toggle-label');
        const container = document.getElementById('wake-lock-toggle-label-container');
        wanted = (settings() || persistence.load())[KEY] !== false;
        if (!supported) {
            if (toggle) { toggle.checked = false; toggle.disabled = true; }
            if (label) label.textContent = 'Keep Screen On (not supported by this browser)';
            if (container) container.classList.add('disabled');
            return;
        }
        if (toggle) {
            toggle.checked = wanted;
            toggle.addEventListener('change', () => setWanted(toggle.checked));
        }
        document.addEventListener('visibilitychange', () => {
            if (document.visibilityState === 'visible') acquire();
        });
        acquire();
    };

    return { init, isActive: () => lock !== null };
})();

document.addEventListener('DOMContentLoaded', screenWakeLock.init);
