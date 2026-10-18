// Settings persistence (v3).
//
// Device-only preferences (fullscreen, keep the screen on,
// vibration, button position) are kept in this browser's localStorage.
// Everything about sounds and tabs -
// card colors, emoji, layout modes and custom orders - is stored on the PC
// and shared by every paired phone. It is keyed by stable sound and tab IDs,
// so it survives renaming or moving files and folders on the PC.
//
// The getters stay synchronous: they read an in-memory mirror of the PC's
// data (state.settings.soundSettings / tabLayouts / layoutOrders), which
// sync() refreshes and the setters update immediately before sending the
// change to the PC.
//
// Earlier versions (v1/v2) stored colors, emoji and orders per file path in
// localStorage. sync() sends that data to the PC once (filling only values
// that are not set yet), so nothing is lost on update.
const persistence = (() => {
    const STORAGE_KEY = 'resoundRemoteSettings_v3';
    const CURRENT_VERSION = 3;
    const LEGACY_KEYS = ['sounduxRemoteSettings_v2', 'sounduxRemoteSettings_v1', 'sounduxRemoteSettings'];
    const LEGACY_DONE_KEY = 'resoundLegacySettingsImported';
    const DEFAULT_LAYOUT = 'grid-3';

    // fullscreen is only stored once chosen; until then the device decides
    // (fullscreen.js).
    const getDefaultSettings = () => ({
        version: CURRENT_VERSION,
        keepScreenOn: true,
        haptics: true,
        swapButtonPosition: false,
        // Mirror of the PC's data (also cached here for a fast first paint).
        soundSettings: {}, // key: sound ID (string) -> { color, emoji, favorite, hasCustomVolume, volumeDown }
        tabLayouts: {},    // key: tab ID (string) | 'favorites' -> { currentLayoutMode }
        layoutOrders: {},  // key: `${tabKey}-${layoutMode}` -> [sound IDs as strings]
    });

    const readJSON = (key) => {
        try {
            const raw = localStorage.getItem(key);
            return raw ? JSON.parse(raw) : null;
        } catch (error) {
            console.warn(`Could not read ${key}:`, error);
            return null;
        }
    };

    // --- Local storage -------------------------------------------------------

    const loadSettings = () => {
        const defaults = getDefaultSettings();
        const stored = readJSON(STORAGE_KEY);
        if (stored && typeof stored === 'object' && stored.version === CURRENT_VERSION) {
            const settings = {
                ...defaults,
                ...stored,
                soundSettings: stored.soundSettings || {},
                tabLayouts: stored.tabLayouts || {},
                layoutOrders: stored.layoutOrders || {},
            };
            // autoFullscreenEnabled was saved as true by default everywhere,
            // so only false was a choice. Without it the device decides.
            if (settings.autoFullscreenEnabled === false && settings.fullscreen === undefined) settings.fullscreen = false;
            delete settings.autoFullscreenEnabled;
            return settings;
        }
        // First start after the update: keep this phone's device preferences.
        for (const key of LEGACY_KEYS) {
            const old = readJSON(key);
            if (old && typeof old === 'object') {
                if (old.autoFullscreenEnabled === false) defaults.fullscreen = false;
                if (typeof old.swapButtonPosition === 'boolean') defaults.swapButtonPosition = old.swapButtonPosition;
                break;
            }
        }
        return defaults;
    };

    const saveSettings = (settingsObject) => {
        if (!settingsObject || typeof settingsObject !== 'object') {
            console.error('Attempted to save invalid settings object:', settingsObject);
            return;
        }
        try {
            settingsObject.version = CURRENT_VERSION;
            localStorage.setItem(STORAGE_KEY, JSON.stringify(settingsObject));
        } catch (error) {
            console.error('Error saving settings to localStorage:', error);
        }
    };

    const saveLocal = () => saveSettings(state.settings);

    // --- PC communication ----------------------------------------------------

    const api = async (method, url, body) => {
        const response = await fetch(url, {
            method,
            credentials: 'include',
            headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
            body: body !== undefined ? JSON.stringify(body) : undefined,
        });
        if (response.status === 401) {
            window.location.href = '/connect'; // this device was signed out on the PC
            throw new Error('Unauthorized');
        }
        if (!response.ok) throw new Error(`${method} ${url} failed: ${response.status}`);
        return response.json();
    };

    // Changes are sent in order, per target, so a fast sequence of edits
    // always ends with the latest state on the PC.
    const queues = new Map();
    const send = (target, method, url, bodyFn) => {
        const prev = queues.get(target) || Promise.resolve();
        const next = prev
            .catch(() => {})
            .then(() => api(method, url, bodyFn()))
            .catch(error => console.error(`Saving ${target} on the PC failed:`, error));
        queues.set(target, next);
        return next;
    };

    const ensure = () => {
        if (!state.settings) state.settings = loadSettings();
        return state.settings;
    };

    // --- Mirror of the PC's data ---------------------------------------------

    const mirrorSound = (sound) => {
        const s = ensure();
        const id = String(sound.id);
        const entry = { ...(s.soundSettings[id] || {}) };
        const setOrDelete = (key, value) => { if (value) entry[key] = value; else delete entry[key]; };
        if ('color' in sound) setOrDelete('color', sound.color);
        if ('emoji' in sound) setOrDelete('emoji', sound.emoji);
        if ('isFavorite' in sound) setOrDelete('favorite', sound.isFavorite === true);
        if ('hasCustomVolume' in sound) setOrDelete('hasCustomVolume', sound.hasCustomVolume === true);
        if ('sliderPosition' in sound) setOrDelete('volumeDown', sound.sliderPosition < 0); // quieter than the default
        if (Object.keys(entry).length) s.soundSettings[id] = entry;
        else delete s.soundSettings[id];
    };

    // Applies a sound list from the PC (e.g. a tab's sounds) to the mirror.
    const ingestSounds = (sounds) => {
        if (!Array.isArray(sounds)) return;
        sounds.forEach(sound => { if (sound && sound.id !== undefined) mirrorSound(sound); });
        saveLocal();
    };

    const ingestLibrary = (data) => {
        const s = ensure();
        s.soundSettings = {};
        (data.sounds || []).forEach(mirrorSound);
        s.tabLayouts = {};
        s.layoutOrders = {};
        Object.entries(data.layouts || {}).forEach(([key, layout]) => {
            if (layout.mode) s.tabLayouts[key] = { currentLayoutMode: layout.mode };
            Object.entries(layout.orders || {}).forEach(([mode, ids]) => {
                if (Array.isArray(ids) && ids.length) s.layoutOrders[`${key}-${mode}`] = ids.map(String);
            });
        });
        saveLocal();
    };

    // Splits a v2 order key `${tabId}-${mode}`; tab IDs never contain '-'.
    const splitOrderKey = (key) => {
        const i = key.indexOf('-');
        return i > 0 ? [key.slice(0, i), key.slice(i + 1)] : [null, null];
    };

    // Converts v1/v2 (path-keyed) settings into an import request.
    const legacyImportBody = (old, overwrite) => {
        const body = { overwrite, sounds: [], layouts: {} };
        Object.entries(old.soundSettings || {}).forEach(([path, st]) => {
            if (!st || typeof st !== 'object') return;
            const color = st.color && st.color !== 'default' ? st.color : undefined;
            if (color || st.emoji) body.sounds.push({ path, color, emoji: st.emoji || undefined });
        });
        const layout = (key) => (body.layouts[key] = body.layouts[key] || { orders: {} });
        Object.entries(old.tabLayouts || {}).forEach(([key, value]) => {
            if (value?.currentLayoutMode) layout(String(key)).mode = value.currentLayoutMode;
        });
        Object.entries(old.layoutOrders || {}).forEach(([key, paths]) => {
            const [tab, mode] = splitOrderKey(key);
            if (tab && mode && Array.isArray(paths) && paths.length) layout(tab).orders[mode] = paths;
        });
        return body;
    };

    const migrateLegacy = async () => {
        if (localStorage.getItem(LEGACY_DONE_KEY)) return false;
        let old = null;
        for (const key of LEGACY_KEYS) {
            old = readJSON(key);
            if (old && typeof old === 'object') break;
        }
        if (old) {
            const body = legacyImportBody(old, false);
            if (body.sounds.length || Object.keys(body.layouts).length) {
                const result = await api('POST', '/api/library/import', body);
                console.log('Moved this phone\'s earlier sound settings to the PC:', result);
            }
        }
        try { localStorage.setItem(LEGACY_DONE_KEY, new Date().toISOString()); } catch (e) { /* ignore */ }
        return !!old;
    };

    // Loads sound and tab settings from the PC (and moves earlier local
    // settings there once). Call before rendering tabs.
    const sync = async () => {
        ensure();
        if (await migrateLegacy()) console.log('Earlier local settings were sent to the PC.');
        ingestLibrary(await api('GET', '/api/library'));
    };

    // --- Getters -------------------------------------------------------------

    const getSoundSettings = (soundId) => ensure().soundSettings[String(soundId)] || {};

    const getCurrentLayoutMode = (tabKey) => ensure().tabLayouts[String(tabKey)]?.currentLayoutMode || DEFAULT_LAYOUT;

    const getLayoutOrder = (tabKey, layoutMode) => ensure().layoutOrders[`${tabKey}-${layoutMode}`] || null;

    // --- Sound settings ------------------------------------------------------

    // Local mirror only (favorites and volumes are changed through their own
    // endpoints; this keeps the cards in sync until the next reload).
    const setSoundSetting = (soundId, key, value) => {
        const s = ensure();
        const id = String(soundId);
        s.soundSettings[id] = { ...(s.soundSettings[id] || {}), [key]: value };
        saveLocal();
    };

    const removeSoundSetting = (soundId, key) => {
        const s = ensure();
        const id = String(soundId);
        if (!s.soundSettings[id]) return;
        delete s.soundSettings[id][key];
        if (Object.keys(s.soundSettings[id]).length === 0) delete s.soundSettings[id];
        saveLocal();
    };

    const pushAppearance = (soundId) => {
        const id = String(soundId);
        return send(`sound-${id}`, 'POST', `/api/sounds/${id}/appearance`, () => {
            const st = getSoundSettings(id);
            return { color: st.color || '', emoji: st.emoji || '' };
        });
    };

    const setSoundColor = (soundId, color) => {
        if (!color || color === 'default') removeSoundSetting(soundId, 'color');
        else setSoundSetting(soundId, 'color', color);
        pushAppearance(soundId);
    };

    const setSoundEmoji = (soundId, emoji) => {
        if (emoji) setSoundSetting(soundId, 'emoji', emoji);
        else removeSoundSetting(soundId, 'emoji');
        pushAppearance(soundId);
    };

    const clearSoundEmoji = (soundId) => setSoundEmoji(soundId, '');
    const clearSoundColor = (soundId) => setSoundColor(soundId, 'default');

    const setSoundFavorite = (soundId, isFavorite) => setSoundSetting(soundId, 'favorite', !!isFavorite);

    // sliderPosition (-50..+50) tells a quieter custom volume from a louder one.
    const setSoundCustomVolume = (soundId, hasCustom, sliderPosition = 0) => {
        if (hasCustom) setSoundSetting(soundId, 'hasCustomVolume', true);
        else removeSoundSetting(soundId, 'hasCustomVolume');
        if (hasCustom && sliderPosition < 0) setSoundSetting(soundId, 'volumeDown', true);
        else removeSoundSetting(soundId, 'volumeDown');
    };

    // --- Layouts -------------------------------------------------------------

    const pushLayout = (tabKey) => {
        const key = String(tabKey);
        return send(`layout-${key}`, 'POST', `/api/layouts/${encodeURIComponent(key)}`, () => {
            const s = ensure();
            const orders = {};
            Object.entries(s.layoutOrders).forEach(([k, ids]) => {
                if (k.startsWith(`${key}-`)) orders[k.slice(key.length + 1)] = ids.map(Number);
            });
            return { mode: s.tabLayouts[key]?.currentLayoutMode || '', orders };
        });
    };

    const setCurrentLayoutMode = (tabKey, layoutMode) => {
        const key = String(tabKey);
        ensure().tabLayouts[key] = { currentLayoutMode: layoutMode };
        saveLocal();
        pushLayout(key);
    };

    const setLayoutOrder = (tabKey, layoutMode, orderArray) => {
        if (!Array.isArray(orderArray)) { console.error('setLayoutOrder requires an array.'); return; }
        ensure().layoutOrders[`${tabKey}-${layoutMode}`] = orderArray.map(String);
        saveLocal();
        pushLayout(tabKey);
    };

    const clearLayoutOrder = (tabKey, layoutMode) => {
        const key = `${tabKey}-${layoutMode}`;
        if (!ensure().layoutOrders[key]) return;
        delete state.settings.layoutOrders[key];
        saveLocal();
        pushLayout(tabKey);
    };

    // --- Reset ---------------------------------------------------------------

    // Clears every card color, emoji and layout on the PC (for all phones)
    // and this phone's preferences. Favorites and volumes are kept.
    const resetAllSettings = async () => {
        try {
            await api('POST', '/api/library/reset-appearance');
        } catch (error) {
            console.error('Resetting settings on the PC failed:', error);
        }
        try { localStorage.removeItem(STORAGE_KEY); } catch (e) { /* ignore */ }
        state.settings = getDefaultSettings();
        try { await sync(); } catch (e) { console.warn('Reload after reset failed:', e); }
        saveLocal();
        document.dispatchEvent(new CustomEvent('settingsReset'));
    };

    return {
        load: loadSettings,
        save: (settingsObj = state.settings) => saveSettings(settingsObj),
        sync,
        ingestSounds,

        getSoundSettings,
        getCurrentLayoutMode,
        getLayoutOrder,

        setSoundSetting,
        removeSoundSetting,
        setSoundColor,
        setSoundEmoji,
        setSoundFavorite,
        setSoundCustomVolume,
        setCurrentLayoutMode,
        setLayoutOrder,

        clearLayoutOrder,
        clearSoundEmoji,
        clearSoundColor,

        resetAllSettings,
    };
})();
