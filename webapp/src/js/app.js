// Global state management
const state = {
  currentlyPlaying: new Map(), // Map soundId -> playingId
  tabs: [],
  currentTab: null, // Can be tabId (string) or 'favorites' (string)
  searching: false, // the search shows its results in the grid (search.js)
  // Loaded at once: device.js, screen-wake.js and install.js read their
  // preferences before init() runs (an empty object here made them miss
  // saved ones, e.g. vibration off). init() loads them again.
  settings: persistence.load(),
  apiBaseUrl: '', // Determined on init
  longPressTimer: null,
  longPressTarget: null,
  lastLongPressTime: 0,

  isAnythingPlayingUnpaused: false, // Tracks overall playback state for UI
  isTalkThroughButtonPressed: false, // Tracks button press state locally
  playbackGloballyPausedByToggle: false, // Tracks C++ state via callback
};
// Other scripts read it as window.state (a top-level const is no window
// property): sound-settings.js marks long presses and checks push-to-talk.
window.state = state;

// DOM Elements
const serverStatusEl = document.getElementById('server-status-text');
const statusIndicatorEl = document.getElementById('status-indicator');
const stopAllButton = document.getElementById('stop-all');
const tabsContainerEl = document.getElementById('tabs-container');
const soundsContainerEl = document.getElementById('sounds-container');
const topBarEl = document.getElementById('top-bar');
const playIndicatorEl = document.getElementById('play-indicator');
const headerEl = document.querySelector('header'); // Get the header element
const rippleContainerEl = headerEl?.querySelector('.ripple-container'); // Get the ripple container

const playPauseToggleButton = document.getElementById('play-pause-toggle-button');
const talkThroughButton = document.getElementById('talk-through-button');


const appSettingsButton = document.getElementById('app-settings-button');
const appSettingsModalOverlay = document.getElementById('app-settings-modal-overlay');
const closeAppSettingsButton = document.getElementById('close-app-settings-button');
const autoFullscreenToggle = document.getElementById('auto-fullscreen-toggle'); // Added for completeness
const swapButtonPositionToggle = document.getElementById('swap-button-position-toggle'); // NEW Toggle
// Reset Buttons
const resetCurrentPageVisualsButton = document.getElementById('reset-current-page-visuals');
const resetCurrentPageLayoutButton = document.getElementById('reset-current-page-layout');
const resetAllSettingsButton = document.getElementById('reset-all-settings');


// updateTopBarProgressState shows the furthest progress of the playing
// sounds as a fill behind the top bar (.top-bar::before in styles.css). The
// fill glides between polls, fades in and out, and resets once faded.
let topBarResetTimer = null;
function updateTopBarProgressState(currentMaxPercentage = 0) {
    if (!topBarEl) return;

    // Visible if: actively playing OR paused by toggle button (but not during talk-through)
    const shouldShowProgress = (state.isAnythingPlayingUnpaused || state.playbackGloballyPausedByToggle) && !state.isTalkThroughButtonPressed;
    const wasShowing = topBarEl.classList.contains('progress-active');
    topBarEl.classList.toggle('progress-active', shouldShowProgress);

    if (shouldShowProgress) {
        clearTimeout(topBarResetTimer);
        let maxPercentage = currentMaxPercentage;
        if (maxPercentage === 0 && soundProgress.activeSounds.size > 0) {
            soundProgress.activeSounds.forEach(sound => {
                if (sound.lengthInMs > 0) {
                    maxPercentage = Math.max(maxPercentage, Math.min(100, (sound.readInMs / sound.lengthInMs) * 100));
                }
            });
        }
        topBarEl.style.setProperty('--progress-fraction', String(Math.max(0, Math.min(100, maxPercentage)) / 100));
    } else if (wasShowing) {
        clearTimeout(topBarResetTimer);
        topBarResetTimer = setTimeout(() => {
            if (topBarEl.classList.contains('progress-active')) return;
            topBarEl.classList.add('progress-reset');
            topBarEl.style.setProperty('--progress-fraction', '0');
            void topBarEl.offsetWidth;
            topBarEl.classList.remove('progress-reset');
        }, 400);
    }
}

// Initialize the application
function init() {
    state.apiBaseUrl = window.location.origin;
    console.log("API Base URL:", state.apiBaseUrl);
    // ---> LOAD PERSISTENCE FIRST <---
    state.settings = persistence.load();
    console.log("Initial settings loaded:", state.settings);
    // Apply initial top bar alignment BEFORE setting up listeners that might depend on it
    applyButtonPositionSetting();
    setupEventListeners();
    state.isAnythingPlayingUnpaused = false;
    state.isTalkThroughButtonPressed = false;
    state.playbackGloballyPausedByToggle = false; // Init new state
    updatePlayPauseButtonIcon(); // Set initial icon state & visibility
    updateTopBarProgressState(0); // Set initial progress state (inactive)
    setupPanelLongPress();
    checkServerStatus();
}

// --- Long press on the logo: the PC's panel ---
// Holding the logo (with the title next to it) opens Resound's panel on the
// PC, or closes it if it is open. The browser's own long press (the image
// menu, Android's long-press vibration, dragging the image) is suppressed.
const PANEL_HOLD_MS = 500;
function setupPanelLongPress() {
    const area = headerEl?.querySelector('.app-title');
    if (!area) return;
    area.querySelector('.logo')?.setAttribute('draggable', 'false');
    let timer = null;
    let start = null;
    const cancel = () => {
        clearTimeout(timer);
        timer = null;
        start = null;
        area.classList.remove('holding');
    };
    area.addEventListener('pointerdown', (e) => {
        if (e.button > 0) return;
        cancel();
        start = { x: e.clientX, y: e.clientY };
        area.classList.add('holding');
        timer = setTimeout(() => {
            timer = null;
            start = null;
            area.classList.remove('holding');
            togglePcPanel(e);
        }, PANEL_HOLD_MS);
    });
    area.addEventListener('pointermove', (e) => {
        if (start && Math.hypot(e.clientX - start.x, e.clientY - start.y) > 10) cancel();
    });
    for (const type of ['pointerup', 'pointercancel', 'pointerleave']) area.addEventListener(type, cancel);
    area.addEventListener('touchstart', (e) => e.preventDefault(), { passive: false }); // no system long press
    area.addEventListener('contextmenu', (e) => e.preventDefault());
    area.addEventListener('dragstart', (e) => e.preventDefault());
}

async function togglePcPanel(event) {
    if (window.haptics) window.haptics.tap();
    createRipple(event, headerEl, 'rgba(80, 222, 168, 0.25)');
    try {
        const data = await apiFetch('/api/panel/toggle', { method: 'POST' });
        showToast(data.open ? 'Resound is open on your PC.' : 'Resound closed on your PC.');
    } catch (error) {
        if (error.status === 404) showToast('Update Resound on your PC to open it from here.');
        else if (error.status) showToast("Couldn't open Resound on your PC.");
        // No answer at all: the connection help shows what to check.
    }
}


// --- Connection status ---
// "Can't reach the PC" means a request got no answer at all (Wi-Fi gone, PC
// asleep, Resound closed). Any answer, even an error such as a sound whose
// file is damaged, proves the PC is reachable. While unreachable, the remote
// retries with a growing delay and refreshes itself when the PC is back.
const connection = { up: null, retryTimer: null, retryDelay: 1000, loaded: false };

function setConnection(up) {
    if (up) {
        clearTimeout(connection.retryTimer);
        connection.retryTimer = null;
        connection.retryDelay = 1000;
        showConnectionHelp(false);
    }
    if (connection.up !== up) {
        connection.up = up;
        if (serverStatusEl) serverStatusEl.textContent = up ? 'Connected' : "Can't reach the PC";
        if (statusIndicatorEl) statusIndicatorEl.className = up ? 'status-dot connected' : 'status-dot error';
    }
    if (!up) scheduleReconnect();
}

function scheduleReconnect() {
    if (connection.retryTimer) return;
    connection.retryTimer = setTimeout(reconnect, connection.retryDelay);
    connection.retryDelay = Math.min(connection.retryDelay * 2, 10000);
}

async function reconnect() {
    connection.retryTimer = null;
    try {
        await apiFetch('/api/status'); // success marks the connection up
    } catch (_) {
        return; // apiFetch scheduled the next attempt
    }
    if (!connection.loaded) {
        checkServerStatus(); // the first load never finished
        return;
    }
    checkForPlayingSounds(); // pick up sounds started meanwhile
    // A tab opened while the PC was away shows an error instead of its sounds.
    if (soundsContainerEl?.querySelector('.error-text')) reloadSoundsForCurrentTab();
}

// Retry at once when the phone wakes up or gets its network back.
function retryNow() {
    if (connection.up !== false) return;
    clearTimeout(connection.retryTimer);
    connection.retryTimer = null;
    connection.retryDelay = 1000;
    reconnect();
}
document.addEventListener('visibilitychange', () => { if (!document.hidden) retryNow(); });
window.addEventListener('online', retryNow);

// --- Can't reach the PC ---
// Once a connection attempt has failed, the sounds make way for a list of
// what to check (connection-help.js). A dropped connection alone doesn't
// show it: that usually comes straight back (rtc.js reconnects in ~70 ms),
// and an attempt that gets no answer fails after 8 s.
const connectionHelpEl = document.getElementById('connection-help');
const connectionHelpRetry = document.getElementById('connection-help-retry');

function showConnectionHelp(on) {
    if (!connectionHelpEl || connectionHelpEl.hidden === !on) return;
    if (on) {
        const pc = window.resoundRTC?.currentPC();
        document.getElementById('connection-help-name').textContent = pc ? pc.name : 'your PC';
        window.resoundHelp?.render(document.getElementById('connection-help-steps'), false);
    }
    connectionHelpEl.hidden = !on;
}

window.resoundRTC?.onStatus((status, detail) => {
    if (status === 'up') showConnectionHelp(false);
    else if (status === 'down' && detail) showConnectionHelp(true); // an attempt failed
    if (connectionHelpRetry) {
        connectionHelpRetry.disabled = status === 'connecting';
        connectionHelpRetry.textContent = status === 'connecting' ? 'Connecting…' : 'Try again';
    }
});
connectionHelpRetry?.addEventListener('click', retryNow);
document.getElementById('connection-help-scan')?.addEventListener('click', () => { location.href = '/connect#scan'; });
// The device's own network state is part of the list.
const refreshConnectionHelp = () => {
    if (connectionHelpEl && !connectionHelpEl.hidden) window.resoundHelp?.render(document.getElementById('connection-help-steps'), false);
};
window.addEventListener('online', refreshConnectionHelp);
window.addEventListener('offline', refreshConnectionHelp);
navigator.connection?.addEventListener?.('change', refreshConnectionHelp);

// --- API Helper ---
async function apiFetch(endpoint, options = {}) {
    const url = `${state.apiBaseUrl}${endpoint}`;
    let response;
    try {
        response = await fetch(url, { credentials: 'include', ...options });
    } catch (networkError) {
        console.error(`API Fetch Error for ${url}:`, networkError);
        setConnection(false);
        throw networkError;
    }
    setConnection(true);

    if (!response.ok) {
        let errorMsg = `HTTP error! status: ${response.status}`;
        try {
            const textResponse = await response.text();
            try {
                errorMsg = JSON.parse(textResponse).error || errorMsg;
            } catch (jsonError) {
                if (textResponse.length > 0 && textResponse.length < 100) errorMsg += ` - ${textResponse}`;
            }
        } catch (bodyError) {
            console.warn(`API Fetch Error: Could not read response body for ${endpoint}.`);
        }
        if (response.status === 401) {
            console.warn(`API request unauthorized (${endpoint}). Redirecting to login.`);
            window.location.href = '/connect'; // this device was signed out on the PC
        }
        const error = new Error(errorMsg);
        error.status = response.status; // the server answered: not a connection problem
        throw error;
    }

    const contentType = response.headers.get("content-type");
    if (contentType && contentType.indexOf("application/json") !== -1) {
        return await response.json();
    }
    if (options.headers && options.headers['Accept'] === 'application/json') {
        throw new Error("Unexpected response format from server.");
    }
    return response; // e.g. for file downloads
}

// --- Toast messages ---
// A short message above the tabs, e.g. when a sound can't be played.
let toastTimer = null;
function showToast(text) {
    let el = document.getElementById('toast');
    if (!el) {
        el = document.createElement('div');
        el.id = 'toast';
        el.setAttribute('role', 'status');
        document.body.appendChild(el);
    }
    el.textContent = text;
    el.classList.add('visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.remove('visible'), 3500);
}

// --- Server Status & Initial Load ---
async function checkServerStatus() {
  try {
      await apiFetch('/api/status'); // marks the connection up
      updateTopBarProgressState(0);

      // Card colors, emoji and layouts are stored on the PC (shared by all
      // phones). Without them the cached copy is used.
      try {
          await persistence.sync();
      } catch (syncError) {
          console.warn('Could not load sound settings from the PC, using the cached copy:', syncError);
      }

      await loadTabs(); // Load tabs and activate the first/last active one
      connection.loaded = true;


       if (state.currentTab !== null && typeof state.currentTab !== 'undefined') {
           checkForPlayingSounds(); // Checks for currently playing sounds
           // loadSounds for the current tab is called within setActiveTab during loadTabs
       } else {
           console.warn("No current tab set after loadTabs, skipping initial checkForPlayingSounds.");
           if (tabsContainerEl && tabsContainerEl.children.length === 0) {
               // Handle case with no tabs if necessary
           }
           if(soundsContainerEl) soundsContainerEl.innerHTML = '<p class="no-sounds">Select a tab.</p>';
       }
       console.log("checkServerStatus successful, dispatching appReady");
       document.dispatchEvent(new CustomEvent('appReady')); // Notify other modules (like search.js)

  } catch (error) {
      console.error("Failed initial server status check or subsequent load sequence:", error.message);
      // No answer from the PC: the connection help covers the sounds, and
      // the first load runs again once the PC is back (reconnect).
      const text = error.status ? '<p class="error-text">Failed to load.</p>' : '';
      if (tabsContainerEl) tabsContainerEl.innerHTML = text;
      if (soundsContainerEl) soundsContainerEl.innerHTML = text;
      updateTopBarProgressState(0);
      state.isAnythingPlayingUnpaused = false; // Ensure state reflects no playback on error
      updatePlayPauseButtonIcon();
      console.log("checkServerStatus failed, dispatching appReady (with error state)");
      document.dispatchEvent(new CustomEvent('appReady')); // Dispatch even on error

  }
}



// --- Ripple Effect ---
function createRipple(event, targetButton, color = 'rgba(255, 255, 255, 0.2)', persistent = false) {
    if (!rippleContainerEl || !headerEl) return;

    const headerRect = headerEl.getBoundingClientRect();
    const ripple = document.createElement('span');
    const diameter = Math.max(headerRect.width, headerRect.height) * 1.5; // Make ripple large enough
    const radius = diameter / 2;

    // Calculate click position relative to the header container
    const clickX = event.clientX - headerRect.left;
    const clickY = event.clientY - headerRect.top;

    ripple.style.width = ripple.style.height = `${diameter}px`;
    ripple.style.left = `${clickX - radius}px`;
    ripple.style.top = `${clickY - radius}px`;
    ripple.classList.add('ripple');
    ripple.style.backgroundColor = color;

    if (persistent) {
        ripple.classList.add('persistent'); // Add class for persistent ripples
        ripple.style.animation = 'none'; // Don't start animation immediately
        ripple.style.transform = 'scale(4)'; // Start scaled up
        ripple.style.opacity = '0.4'; // Keep opacity
        // Store reference on the button itself
        targetButton._persistentRipple = ripple;
    }

    rippleContainerEl.appendChild(ripple);

    // Clean up non-persistent ripples after animation
    if (!persistent) {
        ripple.addEventListener('animationend', () => {
            ripple.remove();
        });
    }
}



// --- Event Listeners ---
function setupEventListeners() {
    // Stop All Button Listener (PointerDown for ripple)
    if (stopAllButton) {
        stopAllButton.addEventListener('pointerdown', (e) => {
            createRipple(e, stopAllButton, 'rgba(242, 65, 111, 0.5)'); // berry red, like the button
        });
        stopAllButton.addEventListener('click', stopAllSounds); // Keep click for action
    } else console.error("Stop All button not found");


    if (playPauseToggleButton) {
        playPauseToggleButton.addEventListener('click', handleTogglePlayPause);
    } else console.error("Play/Pause toggle button not found");

    // Talk Through Button Listeners (Modified for persistent ripple)
    if (talkThroughButton) {
        // --- Use pointerdown for press ---
        talkThroughButton.addEventListener('pointerdown', (e) => {
            // The browser's own long press is cancelled on touchstart (below).

            // --- Aggressively clear sound-card timer ---
            if (typeof soundSettingsManager !== 'undefined' && soundSettingsManager.clearLongPressSoundCardTimer) {
                 soundSettingsManager.clearLongPressSoundCardTimer();
            } else { console.warn("Could not clear sound card long-press timer - manager or function not found."); }

            // --- VIBRATION START (Press) ---
            if (navigator.vibrate) {
                try { navigator.vibrate(7); } catch (err) { console.warn("Vibration failed:", err); }
            }

            // --- Add background class to header ---
            if (headerEl) { headerEl.classList.add('ptt-active-bg'); }

            // --- Create standard visual ripple ---
            createRipple(e, talkThroughButton, 'rgba(80, 222, 168, 0.3)');

            // --- Call handler that triggers API call ---
            handleTalkThroughStart(e);
        });

        // --- Use pointerup for release ---
        talkThroughButton.addEventListener('pointerup', (e) => {
            // Only run release logic if PTT was actually active
            if (!state.isTalkThroughButtonPressed) return;

            // --- VIBRATION END (Release) ---
            if (navigator.vibrate) {
                 try { navigator.vibrate(7); } catch (err) { console.warn("Vibration failed:", err); }
            }

            // --- Remove background class from header ---
            if (headerEl) { headerEl.classList.remove('ptt-active-bg'); }

            // --- Call handler that triggers API call ---
            handleTalkThroughEnd(e);
        });

        // --- Use pointerleave for leaving button while pressed ---
        talkThroughButton.addEventListener('pointerleave', (e) => {
             // Only run release logic if PTT was actually active
             if (state.isTalkThroughButtonPressed) {
                 // --- VIBRATION END (Leave while pressed) ---
                 if (navigator.vibrate) {
                      try { navigator.vibrate(7); } catch (err) { console.warn("Vibration failed:", err); }
                 }

                 // --- Remove background class from header ---
                 if (headerEl) { headerEl.classList.remove('ptt-active-bg'); }

                 // --- Call handler that triggers API call ---
                 handleTalkThroughEnd(e);
             }
        });

        // --- Keep context menu prevention ---
        talkThroughButton.addEventListener('contextmenu', (e) => {
             e.preventDefault();
        });

        // A touch on the button is only push-to-talk. Cancelling touchstart
        // keeps the browser from treating it as a long press (Android's own
        // long-press vibration, text selection), a scroll or a click; the
        // pointer events above still arrive, so the only vibrations are ours
        // on press and release.
        talkThroughButton.addEventListener('touchstart', (e) => {
            if (e.cancelable) e.preventDefault();
        }, { passive: false });

    } else console.error("Talk-Through button not found");




    if (appSettingsButton) appSettingsButton.addEventListener('click', openAppSettingsModal);
    else console.error("App Settings button not found");

    if (appSettingsModalOverlay) {
        appSettingsModalOverlay.addEventListener('click', (e) => {
             if (e.target === appSettingsModalOverlay) closeAppSettingsModal();
        });
    } else console.error("App Settings modal overlay not found");

    if (closeAppSettingsButton) closeAppSettingsButton.addEventListener('click', closeAppSettingsModal);
    else console.error("Close App Settings button not found");



    if (soundsContainerEl) {
        soundsContainerEl.addEventListener('click', (e) => {
            const card = e.target.closest('.sound-card');
            if (!card) return;
            const soundId = parseInt(card.dataset.soundId);
            if (isNaN(soundId)) return;

            const timeSinceLongPress = Date.now() - (state.lastLongPressTime || 0);
            // Increase tolerance slightly for long press end vs click start
            if (timeSinceLongPress < 350) {
                // console.log("Click ignored shortly after long press end.");
                return;
            }

            if (editMode.isActive()) {
                // Open settings only if edit mode is active AND soundSettings module exists
                if (window.soundSettings && typeof window.soundSettings.open === 'function') {
                    window.soundSettings.open(soundId);
                } else {
                    console.error("Sound settings module not available or open function missing.");
                }
            } else {
                // Play sound only if edit mode is NOT active
                playSound(soundId);
            }
        });
    } else { console.error("Sounds container element not found"); }

     document.addEventListener('settingsChanged', () => {
         // console.log("Global settings changed event received.");
         // Might trigger UI updates if needed, e.g., reload sounds if volumes changed globally
     });

     // Setup long press listeners for reset buttons
     setupLongPressResetButtons();

     // NEW: Listener for Swap Button Position Toggle
     if (swapButtonPositionToggle) {
         swapButtonPositionToggle.addEventListener('change', handleSwapButtonPositionChange);
     } else {
         console.error("Swap Button Position toggle not found for event listener setup.");
     }

     window.playbackStateChanged = (newState) => {
        console.log("Playback state changed notification received:", newState);
        const isNowPlaying = (newState === 'playing');
        state.isAnythingPlayingUnpaused = isNowPlaying;
        // If newState is 'paused', it means the toggle button was used to pause.
        state.playbackGloballyPausedByToggle = (newState === 'paused');
        updatePlayPauseButtonIcon();
        updateTopBarProgressState(); // Update progress bar visibility/state
   };
   window.talkThroughStateChanged = (isActive) => {
       console.log("Talk-through state changed notification received:", isActive);
       state.isTalkThroughButtonPressed = isActive;
       talkThroughButton?.classList.toggle('active', isActive);
       updatePlayPauseButtonIcon();
       updateTopBarProgressState(); // Update progress bar visibility/state
   };


    // --- MODIFIED: Defer toggle checks until appReady ---
    document.addEventListener('appReady', () => {
        // Fullscreen Toggle Check
        if (window.fullscreenManager) {
            console.log("App ready: Fullscreen Manager found.");
            if (autoFullscreenToggle) {
                autoFullscreenToggle.checked = state.settings?.autoFullscreenEnabled ?? false;
                // Listener is attached within fullscreenManager.init()
            } else {
                console.error("App ready, Auto Fullscreen toggle button not found!");
            }
        } else {
            console.warn("App ready: window.fullscreenManager not found!");
            if (autoFullscreenToggle) autoFullscreenToggle.disabled = true; // Disable if manager missing
        }

        // Swap Button Position Toggle Check
        if (swapButtonPositionToggle) {
            swapButtonPositionToggle.checked = state.settings?.swapButtonPosition ?? false;
            // Listener is attached in setupEventListeners
        } else {
            console.error("App ready, Swap Button Position toggle button not found!");
        }

    }, { once: true });
    // --- END Defer toggle checks ---


}

// --- NEW Function to apply button position ---
function applyButtonPositionSetting() {
    if (!topBarEl) return;
    const swapEnabled = state.settings?.swapButtonPosition ?? false;
    topBarEl.classList.toggle('buttons-left', swapEnabled);
    console.log(`Top bar button position set to: ${swapEnabled ? 'left' : 'right'}`);
}

// --- NEW Handler for Swap Button Position Toggle ---
function handleSwapButtonPositionChange(event) {
    const isEnabled = event.target.checked;
    console.log(`Swap Button Position toggled: ${isEnabled}`);
    if (!state.settings) state.settings = {}; // Ensure settings object exists
    state.settings.swapButtonPosition = isEnabled;
    persistence.save(state.settings); // Save the new setting
    applyButtonPositionSetting(); // Apply the change immediately
}

async function handleTogglePlayPause() {
    if (!playPauseToggleButton) return;
    console.log("Play/Pause toggle button clicked.");
    // Removed button disabling logic

    try {
        // Send the request but don't update state/UI directly from the response here.
        // Rely on the window.playbackStateChanged callback triggered by the backend.
        const result = await apiFetch('/api/playback/toggle', { method: 'POST' });
        if (result && result.success) {
            console.log("Playback toggle request sent successfully via API. Waiting for backend confirmation callback.");
            // State and UI update will be handled by window.playbackStateChanged
        } else {
            console.error("Failed to send toggle playback request:", result ? result.error : "Unknown API error");
            // If the API call itself fails, update the icon based on the current (potentially stale) state as a fallback.
             updatePlayPauseButtonIcon();
        }
    } catch (error) {
        console.error("Error calling toggle playback API:", error);
        // If the fetch fails, update the icon based on the current (potentially stale) state as a fallback.
        updatePlayPauseButtonIcon();
    }
}

async function handleTalkThroughStart(event) {
     // Don't start if already active or if globally paused by toggle
     if (state.isTalkThroughButtonPressed || state.playbackGloballyPaused) return;
     if (!talkThroughButton) return;

     // For pointer events, we need to capture the pointer to handle pointerleave correctly
     talkThroughButton.setPointerCapture(event.pointerId);

     console.log("Talk-Through button pressed (pointerdown).");
     state.isTalkThroughButtonPressed = true; // Set local state immediately
     talkThroughButton.classList.add('active'); // Immediate visual feedback

     try {
         await apiFetch('/api/talkthrough/start', { method: 'POST' });
         console.log("Talk-through started via API.");
         // C++ side should call window.talkThroughStateChanged(true)
     } catch (error) {
         console.error("Error starting talk-through:", error);
         state.isTalkThroughButtonPressed = false; // Revert state on error
         talkThroughButton.classList.remove('active'); // Remove active state on error
         talkThroughButton.releasePointerCapture(event.pointerId); // Release capture on error
     }
}

async function handleTalkThroughEnd(event) {
     // Only act if the button was actually pressed
     if (!state.isTalkThroughButtonPressed) return;
     if (!talkThroughButton) return;

     console.log("Talk-Through button released (pointerup/leave).");
     state.isTalkThroughButtonPressed = false; // Reset local state
     talkThroughButton.classList.remove('active'); // Immediate visual feedback
     talkThroughButton.releasePointerCapture(event.pointerId); // Release pointer capture

     try {
         await apiFetch('/api/talkthrough/stop', { method: 'POST' });
         console.log("Talk-through stopped via API.");
         // C++ side should call window.talkThroughStateChanged(false)
     } catch (error) {
         console.error("Error stopping talk-through:", error);
         // Consider what state the UI should be in on error (button is already inactive visually)
     }
}


function updatePlayPauseButtonIcon() {
    if (!playPauseToggleButton) return;
    const icon = playPauseToggleButton.querySelector('.icon');
    if (icon) {
        const isCurrentlyPlaying = state.isAnythingPlayingUnpaused;
        // Visibility: Show if anything is actively playing OR if sounds are tracked but potentially paused
        const shouldBeVisible = isCurrentlyPlaying || (soundProgress.activeSounds.size > 0);
        playPauseToggleButton.classList.toggle('hidden', !shouldBeVisible);

        // Icon: Show 'pause' if playing, 'play' otherwise (even if sounds are paused)
        const iconName = isCurrentlyPlaying ? 'pause' : 'play_arrow';
        const label = isCurrentlyPlaying ? 'Pause All Sounds' : 'Resume Paused Sounds';
        if (icon.dataset.icon !== iconName) {
             icon.dataset.icon = iconName;
        }
        playPauseToggleButton.setAttribute('aria-label', label);
    }
}



// --- Hold-to-arm reset buttons ---
// A reset takes two deliberate steps: hold the button until its fill completes
// (it arms and vibrates), then tap it again within a few seconds. The release
// of the hold never counts as the confirming tap; moving the finger cancels.
// Pointer events cover touch, pen and mouse without duplicate handlers.
const HOLD_ARMED_MS = 4000; // armed buttons disarm after this
const holdButtons = new Map(); // button -> state

function setupLongPressResetButtons() {
    [resetCurrentPageVisualsButton, resetCurrentPageLayoutButton, resetAllSettingsButton].forEach(button => {
        if (!button) { console.warn("A reset button was not found."); return; }
        const st = {
            label: getLongPressLabel(button),
            duration: parseInt(button.dataset.longPressDuration) || 800,
            holdTimer: null, armTimer: null, hintTimer: null,
            armed: false, pressArmed: false, pointerId: null, x: 0, y: 0,
        };
        button.style.setProperty('--hold-duration', `${st.duration}ms`);
        holdButtons.set(button, st);

        button.addEventListener('pointerdown', (e) => {
            if (e.pointerType === 'mouse' && e.button !== 0) return;
            st.pointerId = e.pointerId;
            st.x = e.clientX; st.y = e.clientY;
            st.pressArmed = st.armed; // a press that starts armed is the confirming tap
            try { button.setPointerCapture(e.pointerId); } catch (_) { /* not supported */ }
            if (!st.armed) startHold(button, st);
        });
        button.addEventListener('pointermove', (e) => {
            if (e.pointerId !== st.pointerId) return;
            if (Math.abs(e.clientX - st.x) > 12 || Math.abs(e.clientY - st.y) > 12) {
                st.pointerId = null; // this press no longer counts
                if (!st.armed) stopHold(button, st, false);
            }
        });
        button.addEventListener('pointerup', (e) => {
            if (e.pointerId !== st.pointerId) return;
            st.pointerId = null;
            if (st.armed && st.pressArmed) confirmHold(button, st);
            else if (!st.armed) stopHold(button, st, true); // released too early
        });
        button.addEventListener('pointercancel', (e) => {
            if (e.pointerId !== st.pointerId) return;
            st.pointerId = null;
            if (!st.armed) stopHold(button, st, false);
        });
        button.addEventListener('contextmenu', (e) => e.preventDefault()); // long press menu
        button.addEventListener('click', (e) => e.preventDefault()); // actions run on pointerup
    });
}

function startHold(button, st) {
    clearTimeout(st.hintTimer);
    setLongPressLabel(button, st.label);
    button.classList.add('holding');
    st.holdTimer = setTimeout(() => armHold(button, st), st.duration);
}

// stopHold ends a hold before it armed; early shows a short hint.
function stopHold(button, st, early) {
    clearTimeout(st.holdTimer);
    button.classList.remove('holding');
    if (!early) return;
    setLongPressLabel(button, 'Keep holding');
    clearTimeout(st.hintTimer);
    st.hintTimer = setTimeout(() => { if (!st.armed) setLongPressLabel(button, st.label); }, 1500);
}

function armHold(button, st) {
    disarmAllResetButtons(button);
    st.armed = true;
    button.classList.remove('holding');
    button.classList.add('confirming');
    setLongPressLabel(button, 'Tap to confirm');
    if (navigator.vibrate) navigator.vibrate(50);
    st.armTimer = setTimeout(() => disarmHold(button, st), HOLD_ARMED_MS);
}

function disarmHold(button, st) {
    clearTimeout(st.holdTimer);
    clearTimeout(st.armTimer);
    clearTimeout(st.hintTimer);
    st.armed = false;
    st.pointerId = null;
    button.classList.remove('holding', 'confirming');
    setLongPressLabel(button, st.label);
}

function confirmHold(button, st) {
    disarmHold(button, st);
    console.log(`Confirmed action for: ${button.id}`);
    executeResetAction(button.id);
}

// disarmAllResetButtons resets every reset button except keep.
function disarmAllResetButtons(keep = null) {
    holdButtons.forEach((st, button) => { if (button !== keep) disarmHold(button, st); });
}

function getLongPressLabel(button) {
    const label = button.querySelector('span:not(.icon)');
    return (label ? label.textContent : button.textContent).trim();
}

// setLongPressLabel changes only the label, keeping the button's icon.
function setLongPressLabel(button, text) {
    const label = button.querySelector('span:not(.icon)');
    if (label) label.textContent = text; else button.textContent = text;
}

// --- Reset Action Execution (Keep existing code from previous step) ---
async function executeResetAction(buttonId) {
    const currentTabId = state.currentTab;
    if (!currentTabId && buttonId !== 'reset-all-settings') {
        console.error("Cannot perform page-specific reset: No current tab ID.");
        alert("Error: Could not determine the current tab.");
        return;
    }

    try {
        switch (buttonId) {
            case 'reset-current-page-visuals':
                console.log(`Resetting visuals for tab: ${currentTabId}`);
                await resetCurrentPageVisuals(currentTabId);
                break;
            case 'reset-current-page-layout':
                console.log(`Resetting layout for tab: ${currentTabId}`);
                await resetCurrentPageLayout(currentTabId);
                break;
            case 'reset-all-settings':
                console.log("Resetting ALL settings.");
                await resetAllSettings();
                break;
            default:
                console.warn(`Unknown reset action: ${buttonId}`);
        }
    } catch (error) {
        console.error(`Error during reset action (${buttonId}):`, error);
        alert(`An error occurred while resetting: ${error.message}`);
    }
}

async function resetCurrentPageVisuals(tabId) {
    const soundsOnPage = Array.from(soundsContainerEl?.querySelectorAll('.sound-card[data-sound-id]') || []);
    if (soundsOnPage.length === 0) {
        console.log("No sounds found on the current page to reset visuals for.");
        return;
    }

    console.log(`Found ${soundsOnPage.length} sounds on page ${tabId} to reset visuals.`);
    let settingsChanged = false;
    soundsOnPage.forEach(card => {
        const soundId = card.dataset.soundId;
        if (soundId) {
            const currentSettings = persistence.getSoundSettings(soundId);
            // Use clear functions which handle removing keys
            if (currentSettings.color && currentSettings.color !== 'default') {
                 persistence.clearSoundColor(soundId);
                 settingsChanged = true;
            }
            if (currentSettings.emoji) {
                 persistence.clearSoundEmoji(soundId);
                 settingsChanged = true;
            }
            // If we also want to reset volume on visual reset:
            // if (currentSettings.hasCustomVolume) {
            //     persistence.removeSoundSetting(soundPath, 'localVolume');
            //     persistence.removeSoundSetting(soundPath, 'remoteVolume');
            //     persistence.removeSoundSetting(soundPath, 'hasCustomVolume');
            //     settingsChanged = true;
            // }
        }
    });

    if (settingsChanged) {
        console.log("Visual settings were changed, reloading sounds.");
        reloadSoundsForCurrentTab(); // Call the global reload function
    }
}

async function resetCurrentPageLayout(tabId) {
    const currentLayout = persistence.getCurrentLayoutMode(tabId);
    if (!currentLayout) {
        console.error("Could not determine current layout mode for reset.");
        return; // Or default to a specific layout?
    }
    console.log(`Clearing layout order for tab ${tabId}, layout ${currentLayout}`);
    const orderExisted = persistence.getLayoutOrder(tabId, currentLayout) !== null;
    persistence.clearLayoutOrder(tabId, currentLayout);

    if (orderExisted) {
        console.log("Layout order existed and was cleared, reloading sounds.");
        reloadSoundsForCurrentTab(); // Call the global reload function
    }
}

async function resetAllSettings() {
    await persistence.resetAllSettings();
    state.settings = persistence.load(); // Reload settings into global state

    // Update UI elements tied to settings
    if (window.fullscreenManager && typeof state.settings.autoFullscreenEnabled !== 'undefined') {
        const fsToggle = document.getElementById('auto-fullscreen-toggle');
        if (fsToggle) fsToggle.checked = state.settings.autoFullscreenEnabled;
        fullscreenManager.setEnabled(state.settings.autoFullscreenEnabled);
    }

    // Reload tabs and sounds completely
    console.log("Reloading tabs after resetting all settings.");
    await loadTabs(); // This implicitly reloads sounds for the newly determined active tab

    // Close the settings modal after resetting all
    console.log("Closing settings modal after reset all.");
    closeAppSettingsModal();
}


// --- App Settings Modal (Keep existing code from previous step) ---
function openAppSettingsModal() {
    if (window.fullscreenManager) {
        const toggle = document.getElementById('auto-fullscreen-toggle');
        if (toggle && state.settings && typeof state.settings.autoFullscreenEnabled !== 'undefined') {
             toggle.checked = state.settings.autoFullscreenEnabled;
        } else if (toggle) {
             toggle.checked = false; // Default if setting missing
        }
    }
    disarmAllResetButtons(); // never open with a button still armed

    appSettingsModalOverlay?.classList.remove('hidden');
}

function closeAppSettingsModal() {
    appSettingsModalOverlay?.classList.add('hidden');
    disarmAllResetButtons();
}

// --- Tabs ---
async function loadTabs() {
    if (!tabsContainerEl) {
         console.error("Tabs container not found, cannot load tabs.");
         state.currentTab = null; // Ensure state reflects no tab selected
         return;
    }
    try {
        const tabsFromApi = await apiFetch('/api/tabs');
        state.tabs = tabsFromApi; // Update global state
        tabsContainerEl.innerHTML = ''; // Clear existing tabs

        // Always add Favorites tab first
        const favTabElement = createTabElement('', 'favorites', 'favorite');
        tabsContainerEl.appendChild(favTabElement);

        // Add tabs from API
        tabsFromApi.forEach(tab => {
            const tabElement = createTabElement(tab.name, String(tab.id)); // Ensure ID is string
            tabsContainerEl.appendChild(tabElement);
        });

        // Determine which tab to activate
        // Use persisted lastTabId, default to 'favorites' if not set or invalid
        let persistedTabId = String(state.settings.lastTabId || 'favorites'); // Ensure string

        // Verify the persisted tab ID still exists (either 'favorites' or in the API tabs)
        const isValidPersistedId = persistedTabId === 'favorites' || tabsFromApi.some(t => String(t.id) === persistedTabId);

        const targetTabId = isValidPersistedId ? persistedTabId : 'favorites'; // Fallback to favorites

        const tabToActivate = tabsContainerEl.querySelector(`[data-tab-id="${targetTabId}"]`);

        if (tabToActivate) {
            // setActiveTab will handle setting state.currentTab and loading sounds
            await setActiveTab(tabToActivate, targetTabId);
        } else if (tabsContainerEl.children.length > 0) {
            // Fallback: activate the first tab (which should be Favorites) if target wasn't found
            console.warn(`Target tab ID "${targetTabId}" not found, activating first tab.`);
            const firstTab = tabsContainerEl.children[0];
            await setActiveTab(firstTab, firstTab.dataset.tabId);
        } else {
            console.warn("No tabs found or available to activate.");
            state.currentTab = null;
            if(soundsContainerEl) soundsContainerEl.innerHTML = '<p class="no-sounds">No tabs available.</p>';
        }

    } catch (error) {
        console.error('Error loading tabs:', error);
        // No answer from the PC: the caller's first load isn't done, so it
        // runs again on reconnect.
        if (!error.status) throw error;
        if (tabsContainerEl) tabsContainerEl.innerHTML = '<p class="error-text">Failed to load tabs.</p>';
        state.currentTab = null; // Reset state on error
        if(soundsContainerEl) soundsContainerEl.innerHTML = '<p class="error-text">Failed to load tabs.</p>';
    }
}

function createTabElement(name, id, iconName = null) {
    const tabElement = document.createElement('button');
    tabElement.className = 'tab';
    tabElement.dataset.tabId = String(id); // Ensure ID is stored as string

    if (iconName) {
        const icon = document.createElement('span');
        icon.className = 'icon';
        icon.setAttribute('aria-hidden', 'true');
        // The favorites tab always shows the filled heart; colour comes from .tab / .tab.active
        icon.dataset.icon = String(id) === 'favorites' && iconName === 'favorite' ? 'favorite-fill' : iconName;
        tabElement.appendChild(icon);
        tabElement.setAttribute('aria-label', name || 'Favorites');
    }

    // Add text label ONLY if it's not the favorites tab
    if (String(id) !== 'favorites') {
        const textSpan = document.createElement('span');
        textSpan.textContent = name || `Tab ${id}`; // Use name or default Tab ID
        tabElement.appendChild(textSpan);
    } else {
         // Optionally add a specific class for styling the icon-only tab if needed
         tabElement.classList.add('tab-icon-only');
         // Ensure aria-label is set for accessibility even without visible text
         tabElement.setAttribute('aria-label', 'Favorites');
    }


    tabElement.addEventListener('click', () => {
        // Pass the string ID
        setActiveTab(tabElement, String(id));
    });
    return tabElement;
}

async function setActiveTab(tabElement, tabId) {
    const currentTabStr = state.currentTab; // Already a string or null
    const newTabIdStr = String(tabId); // Ensure incoming ID is string

    if (!soundsContainerEl) {
         console.error("Cannot set active tab: Sounds container not found.");
         return;
    }

    // Only proceed if the tab is actually changing
    if (currentTabStr === newTabIdStr) {
         // A rebuilt tab bar (loadTabs) still marks it.
         tabElement?.classList.add('active');
         return;
    }

    console.log(`Activating tab: ${newTabIdStr}`);

    // Update UI
    tabsContainerEl.querySelectorAll('.tab').forEach(tab => tab.classList.remove('active'));
    if (tabElement) {
        tabElement.classList.add('active');
         // Scroll into view after activation
         setTimeout(() => {
             if (window.tabManager?.isCurrentlySwiping()) return; // the next swipe moves the tab bar
             tabElement.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' });
         }, 50);
    }


    // Update global state and persistence
    state.currentTab = newTabIdStr;
    state.settings.lastTabId = newTabIdStr;
    persistence.save(state.settings); // Save the last active tab

    // Notify other modules about the change *before* loading sounds
    document.dispatchEvent(new CustomEvent('tabChanged', { detail: { tabId: newTabIdStr } }));

    // Load sounds for the new tab
    await loadSounds(newTabIdStr);
}

// --- Sounds ---
// Sound lists per tab, as last received from the PC. Tabs render from here at
// once (tab switches and swipes feel instant) and refresh in the background.
const tabSoundsCache = new Map(); // tabId (string) -> sounds from the API

async function fetchTabSounds(tabId) {
    const id = String(tabId);
    const endpoint = (id === 'favorites') ? '/api/favorites' : `/api/tabs/${id}/sounds`;
    const sounds = await apiFetch(endpoint);
    persistence.ingestSounds(sounds); // fresh colors, emoji, favorites from the PC
    tabSoundsCache.set(id, sounds);
    return sounds;
}

// orderSounds applies the tab's saved order for its current layout. Sound IDs
// stay the same when files are renamed or moved on the PC, so the order
// survives that; sounds not in the saved order are appended.
function orderSounds(tabId, soundsFromApi) {
    const currentLayout = persistence.getCurrentLayoutMode(tabId);
    const persistedOrderIds = persistence.getLayoutOrder(tabId, currentLayout);
    if (!Array.isArray(persistedOrderIds) || persistedOrderIds.length === 0) return soundsFromApi;
    const byId = new Map(soundsFromApi.map(sound => [String(sound.id), sound]));
    const ordered = [];
    persistedOrderIds.forEach(id => {
        if (byId.has(id)) {
            ordered.push(byId.get(id));
            byId.delete(id);
        }
    });
    return [...ordered, ...byId.values()];
}

const soundListKey = (sounds) => JSON.stringify((sounds || []).map(s => [s.id, s.name]));

async function loadSounds(tabId) {
    if (!soundsContainerEl) {
        console.error("Cannot load sounds: Sounds container not found.");
        return;
    }
    if (tabId === null || typeof tabId === 'undefined') {
        console.warn("loadSounds called with invalid tabId:", tabId);
        soundsContainerEl.innerHTML = '<p class="no-sounds">Select a tab.</p>';
        return;
    }
    if (state.searching) return; // the grid shows search results (search.js)
    const id = String(tabId);
    const cached = tabSoundsCache.get(id);
    if (cached) {
        displaySounds(orderSounds(id, cached), false); // instant, no fade
    } else {
        soundsContainerEl.innerHTML = '<p class="loading-sounds"></p>';
    }
    window.tabManager?.refreshNeighbors();

    try {
        const fresh = await fetchTabSounds(id);
        if (String(state.currentTab) !== id || state.searching) return; // the user moved on meanwhile
        if (!cached || soundListKey(fresh) !== soundListKey(cached)) {
            displaySounds(orderSounds(id, fresh), !cached);
        } else {
            // Same sounds; colors or favorites may have changed on the PC.
            soundsContainerEl.querySelectorAll('.sound-card').forEach(card => updateSoundCardDisplay(card.dataset.soundId, card));
        }
    } catch (error) {
        console.error(`Error loading sounds for tab ${id}:`, error);
        if (!cached && String(state.currentTab) === id) {
            soundsContainerEl.innerHTML = `<p class="error-text">Failed to load sounds for this tab.</p>`;
        }
    }
}

// --- Library changes on the PC ---
// After a refresh, added or removed folders, or an imported sound, the PC
// sends {t: 'event', e: 'library', rev} (rtc.js). The tab bar is rebuilt
// when tabs came or went, the open tab's sounds refresh in place from the
// cache (no placeholder, no fade), the other tabs' lists are fetched again
// when they are next shown, and an open search reloads its list. rev is the
// library's revision: an event for the revision already shown is skipped.
// Reloads don't overlap; one more runs if another event came meanwhile.
let libraryRev = null;
let libraryReload = null;
let libraryReloadAgain = null; // the rev of an event that came during a reload

const tabsKey = (tabs) => JSON.stringify((tabs || []).map(t => [String(t.id), t.name]));
const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms));

async function reloadLibrary(rev) {
    if (!connection.loaded) return; // the first load is still to come and reads everything
    if (rev && rev === libraryRev) return;
    if (libraryReload) { libraryReloadAgain = rev || ''; return; }
    libraryReload = (async () => {
        try {
            // Colours, emoji, favorites and layouts of new sounds.
            await persistence.sync().catch(e => console.warn('Library reload: settings not refreshed:', e));
            // A swipe in progress ends on the tabs it started with.
            while (window.tabManager?.isCurrentlySwiping()) await sleep(200);
            const current = String(state.currentTab);
            for (const id of [...tabSoundsCache.keys()]) {
                if (id !== current) tabSoundsCache.delete(id);
            }
            const tabs = await apiFetch('/api/tabs');
            if (tabsKey(tabs) !== tabsKey(state.tabs)) {
                await loadTabs(); // new, renamed or removed tabs; keeps the open one if it still exists
            }
            // In place; loadSounds also refetches the swipe previews' lists.
            if (String(state.currentTab) === current) await loadSounds(current);
            document.dispatchEvent(new CustomEvent('libraryChanged'));
            libraryRev = rev || null;
        } catch (e) {
            console.warn('Library reload failed:', e); // the next tab switch or event tries again
        }
    })();
    await libraryReload;
    libraryReload = null;
    if (libraryReloadAgain !== null) {
        const again = libraryReloadAgain;
        libraryReloadAgain = null;
        reloadLibrary(again || undefined);
    }
}
window.resoundRTC?.onEvent((msg) => { if (msg.e === 'library') reloadLibrary(msg.rev); });

function reloadSoundsForCurrentTab() {
    if (state.currentTab !== null && typeof state.currentTab !== 'undefined') {
        console.log(`Reloading sounds requested for current tab: ${state.currentTab}`);
        loadSounds(state.currentTab);
    } else {
        console.warn("Cannot reload sounds: No current tab is set.");
        if(soundsContainerEl) soundsContainerEl.innerHTML = '<p class="no-sounds">Select a tab.</p>';
    }
}

// displaySounds renders the live page; animate fades the cards in (first
// load), cached renders appear at once.
function displaySounds(sounds, animate = true) {
    if (!soundsContainerEl) return;
    soundsContainerEl.innerHTML = ''; // Clear previous sounds or loading message
    if (!sounds || sounds.length === 0) {
         const message = state.currentTab === 'favorites' ? "So empty..." : "No sounds found in this tab.";
         soundsContainerEl.innerHTML = `<p class="no-sounds">${message}</p>`;
        return;
    }

    // Create a document fragment for efficiency
    const fragment = document.createDocumentFragment();
    sounds.forEach(sound => {
        // Basic validation of sound object
        if (!sound || typeof sound.id === 'undefined' || !sound.path || !sound.name) {
             console.warn("Skipping invalid sound data during display:", sound);
             return; // Skip this invalid sound
        }
        const soundElement = createSoundCardElement(sound, { animate });
        if (soundElement) {
             fragment.appendChild(soundElement);
        } else {
             console.warn("Failed to create sound card element for:", sound);
        }
    });

    // Append the fragment to the container
    soundsContainerEl.appendChild(fragment);
    console.log(`Displayed ${sounds.length} sounds.`);
}

// createSoundCardElement builds a card. animate fades it in; tabId is the tab
// it is shown in (previews of other tabs pass theirs).
function createSoundCardElement(sound, { animate = true, tabId = state.currentTab } = {}) {
    const soundElement = document.createElement('div');
    soundElement.className = animate ? 'sound-card fade-in' : 'sound-card';
    soundElement.dataset.soundId = sound.id;
    soundElement.dataset.soundPath = sound.path; // informational; settings are keyed by soundId

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'sound-content-wrapper';

    const textElement = document.createElement('span');
    textElement.className = 'sound-text';
    textElement.textContent = sound.name;

    contentWrapper.appendChild(textElement);
    soundElement.appendChild(contentWrapper);

    // Initial application of visual settings (color, emoji, indicators)
    // Make sure persistence data is loaded *before* this might be called
    updateSoundCardDisplay(sound.id, soundElement, tabId);

    // Check if this sound is currently playing and apply visual state
    if (state.currentlyPlaying.has(sound.id)) {
        soundElement.classList.add('playing');
        // Get progress info if available in the tracking map
        const playingInstance = soundProgress.activeSounds.get(state.currentlyPlaying.get(sound.id));
        if (playingInstance && playingInstance.lengthInMs > 0) {
            const percentage = (playingInstance.readInMs / playingInstance.lengthInMs) * 100;
            // Apply gradient background only if not in list view
            if (!soundsContainerEl?.classList.contains('layout-list')) {
                setCardProgress(soundElement, percentage);
            }
        }
    }

    return soundElement;
}

// Card progress is a fill layer (.sound-card::after, styles.css) in a lighter
// shade of the card's colour. It glides between polls, fades in while the
// sound plays and fades out when it ends, then resets to empty unseen.
function setCardProgress(card, percentage) {
    card.style.setProperty('--progress', String(Math.max(0, Math.min(100, percentage)) / 100));
}

// resetCardProgress empties the fill at once (no animation back to zero).
function resetCardProgress(card) {
    card.classList.add('progress-reset');
    card.style.setProperty('--progress', '0');
    void card.offsetWidth; // apply without transition
    card.classList.remove('progress-reset');
}

// scheduleProgressReset empties the fill once it has faded out.
function scheduleProgressReset(card) {
    setTimeout(() => { if (!card.classList.contains('playing')) resetCardProgress(card); }, 400);
}

function updateSoundCardDisplay(soundId, cardElement = null, tabId = state.searching ? 'search' : state.currentTab) {
    const card = cardElement || soundsContainerEl?.querySelector(`.sound-card[data-sound-id="${soundId}"]`);
    if (!card) {
        // console.warn(`Cannot update display: Card element not found for soundId ${soundId}.`);
        return;
    }


    // Ensure state.settings is initialized before accessing persistence
    if (!state.settings) {
         console.error("Cannot update display: Global state.settings not initialized.");
         return;
    }
    const soundSettings = persistence.getSoundSettings(card.dataset.soundId);

    const contentWrapper = card.querySelector('.sound-content-wrapper');
    const textElement = card.querySelector('.sound-text');
    // The layout of the grid the card is in: a swipe preview or the sheet's
    // floating copy may differ from the live page. Not placed yet: the page's.
    const isList = (card.closest('.sounds-grid') || soundsContainerEl)?.classList.contains('layout-list');

    // --- Indicators ---
    let indicators = card.querySelector('.sound-indicators');
    if (!indicators) {
        indicators = document.createElement('div');
        indicators.className = 'sound-indicators';
        // Insert after content wrapper or append as fallback
        if (contentWrapper) card.insertBefore(indicators, contentWrapper.nextSibling);
        else card.appendChild(indicators);
    }
    indicators.innerHTML = ''; // Clear existing indicators

    // Indicators, left to right: custom volume, then the favourite in the
    // corner.
    // Custom Volume Indicator: quieter or louder than the default
    const hasCustomVolume = soundSettings.hasCustomVolume === true; // Explicitly check for true
     card.dataset.customVolume = hasCustomVolume ? 'true' : 'false'; // Set dataset attribute
    if (hasCustomVolume) {
        const volIcon = document.createElement('span');
        volIcon.className = 'indicator-icon icon volume-indicator';
        volIcon.dataset.icon = soundSettings.volumeDown ? 'volume_down' : 'volume_up';
        indicators.appendChild(volIcon);
    }

    // Favorite Indicator
    const isFavorite = soundSettings.favorite === true; // Explicitly check for true
    card.dataset.favorite = isFavorite ? 'true' : 'false'; // Set dataset attribute
    if (isFavorite && String(tabId) !== 'favorites') { // every card there is a favorite
        const favIcon = document.createElement('span');
        favIcon.className = 'indicator-icon icon favorite-indicator';
        favIcon.dataset.icon = 'favorite-fill';
        indicators.appendChild(favIcon);
    }

    // Remove indicator container if empty
    if (indicators.children.length === 0 && card.contains(indicators)) {
        card.removeChild(indicators);
    }

    // --- Apply Color ---
    let baseColor = 'var(--v-surface-dark)'; // Default background
    // Remove any existing color classes first
    card.classList.forEach(className => {
        if (className.startsWith('sound-color-')) card.classList.remove(className);
    });
    // Apply new color class if set and not default
    if (soundSettings.color && soundSettings.color !== 'default') {
        const colorClass = `sound-color-${soundSettings.color}`;
        card.classList.add(colorClass);
        baseColor = `var(--sound-color-${soundSettings.color}-bg)`; // Get the corresponding background variable
    }
    card.style.setProperty('--sound-base-color', baseColor); // Set CSS variable for gradient use
    card.style.setProperty('--sound-progress-color', soundSettings.color && soundSettings.color !== 'default'
        ? `var(--sound-color-${soundSettings.color}-progress)` : 'var(--sound-progress-default)');

    // --- Apply Emoji ---
    card.style.removeProperty('--sound-emoji'); // Remove potential grid emoji style
    // Remove existing list emoji span if present
    const existingEmojiSpan = contentWrapper?.querySelector('.sound-emoji-list');
    if (existingEmojiSpan) contentWrapper.removeChild(existingEmojiSpan);

    if (soundSettings.emoji && textElement && contentWrapper) {
        if (isList) {
            // Add emoji span before the text in list view
            const emojiSpan = document.createElement('span');
            emojiSpan.className = 'sound-emoji-list';
            emojiSpan.textContent = soundSettings.emoji;
            contentWrapper.insertBefore(emojiSpan, textElement);
        } else {
            // Apply emoji as background pseudo-element in grid view
            card.style.setProperty('--sound-emoji', `"${soundSettings.emoji}"`);
        }
    }

    // --- Background Reset ---
    // If the card isn't currently playing, ensure its background reflects the base color
    if (!card.classList.contains('playing')) {
        card.style.background = ''; // Clear any potential gradient
        // Apply the determined base color if it's not the default surface color
        if (baseColor !== 'var(--v-surface-dark)') {
            card.style.backgroundColor = baseColor;
        } else {
            card.style.backgroundColor = ''; // Use default CSS background
        }
    } else {
         // If it IS playing, re-apply the gradient with the current base color
         // This ensures the non-progress part matches the card color
         const playingInstance = soundProgress.activeSounds.get(state.currentlyPlaying.get(soundId));
         const percentage = (playingInstance && playingInstance.lengthInMs > 0)
                           ? (playingInstance.readInMs / playingInstance.lengthInMs) * 100
                           : 0;
         if (!isList) { // Only for grid layouts
              setCardProgress(card, percentage);
         } else {
              card.style.background = ''; // Ensure list items don't have gradient
         }
    }
}

// --- Playback Control ---
async function playSound(soundId) {
    if (editMode.isActive()) return; // Don't play in edit mode
    if (window.haptics) window.haptics.tap(); // device.js

    try {
        if (!soundsContainerEl) throw new Error("Sounds container not found.");

        const data = await apiFetch(`/api/sounds/${soundId}/play`, { method: 'POST' });
        if (data.success && typeof data.playingId !== 'undefined') {
            console.log(`Sound ${soundId} playback started with playingId ${data.playingId}`);
            trackPlaying(soundId, data);
        } else {
            console.error(`Failed to play sound ${soundId}:`, data.error || 'Unknown server error');
            state.currentlyPlaying.delete(soundId); // Ensure cleanup if play failed
            updatePlayingStateVisuals();
        }
    } catch (error) {
        console.error(`Error initiating playback for sound ${soundId}:`, error);
        state.currentlyPlaying.delete(soundId); // Ensure cleanup on fetch error
        updatePlayingStateVisuals();
        if (error.status) showPlayFailed(soundId, error.message); // the PC answered: this sound failed
    }
}

// trackPlaying follows a sound the PC started (a play, or a preview from the
// sound's sheet) with the progress polling: its card shows the fill, and
// every poll announces 'soundprogress' (the sheet's preview listens).
function trackPlaying(soundId, data) {
    state.currentlyPlaying.set(soundId, data.playingId); // soundId -> playingId
    const playedCard = soundsContainerEl?.querySelector(`.sound-card[data-sound-id="${soundId}"]`);
    if (playedCard) resetCardProgress(playedCard);
    const newSoundData = {
        id: data.playingId, // the instance
        soundId: soundId,   // the sound
        lengthInMs: data.lengthInMs || 0,
        readInMs: 0,
        name: data.name || 'Sound',
        paused: false,
        repeat: false,
    };
    soundProgress.activeSounds.set(data.playingId, newSoundData);
    updatePlayingStateVisuals(); // Update 'playing' class on cards
    handleSoundPlayedVisuals(newSoundData); // Update global UI (play icon)
    startProgressPolling(); // Start or ensure polling is active
}

const announceProgress = () => document.dispatchEvent(new CustomEvent('soundprogress'));

// showPlayFailed flashes the card and says why, e.g. for a damaged file.
function showPlayFailed(soundId, message) {
    const card = soundsContainerEl?.querySelector(`.sound-card[data-sound-id="${soundId}"]`);
    const name = card?.querySelector('.sound-text')?.textContent || 'This sound';
    showToast(`Can't play “${name}”. ${message || ''}`.trim());
    if (!card) return;
    card.classList.remove('play-failed');
    void card.offsetWidth; // restart the animation
    card.classList.add('play-failed');
    setTimeout(() => card.classList.remove('play-failed'), 700);
}

async function stopAllSounds() {
    if (window.haptics) window.haptics.tap();
    console.log("Stopping all sounds via API...");
    try {
        await apiFetch('/api/sounds/stop', { method: 'POST' });
        // Clear local tracking immediately for responsiveness
        state.currentlyPlaying.clear();
        soundProgress.activeSounds.clear();
        updatePlayingStateVisuals(); // Remove 'playing' class
        stopProgressPolling(); // Stop polling
        handleAllSoundsFinishedVisuals(); // Reset global UI
        announceProgress();
        console.log("All sounds stopped.");
    } catch (error) {
        console.error('Error stopping sounds:', error);
        // Even on error, try to clear local state for UI consistency
        state.currentlyPlaying.clear();
        soundProgress.activeSounds.clear();
        updatePlayingStateVisuals();
        stopProgressPolling();
        handleAllSoundsFinishedVisuals();
        announceProgress();
    }
}

// --- Progress Polling (Major Revision) ---
const soundProgress = {
    polling: false,
    interval: null,
    activeSounds: new Map(), // Key: playingId, Value: { id, soundId, name, lengthInMs, readInMs, paused, repeat }
    pollFrequency: 250, // ms
    errorCount: 0,
    maxErrors: 5,
    discover: false, // next poll runs even with nothing tracked
};

function startProgressPolling() {
    if (soundProgress.polling) return; // Already polling
    console.log("Starting progress polling.");
    soundProgress.polling = true;
    soundProgress.errorCount = 0; // Reset error count
    if (soundProgress.interval) clearInterval(soundProgress.interval); // Clear any old interval
    fetchSoundProgress(); // Poll immediately
    soundProgress.interval = setInterval(fetchSoundProgress, soundProgress.pollFrequency);
}

function stopProgressPolling(reason = "No active sounds") {
    if (!soundProgress.polling) return; // Already stopped
    console.log(`Stopping progress polling. Reason: ${reason}`);
    soundProgress.polling = false;
    if (soundProgress.interval) {
        clearInterval(soundProgress.interval);
        soundProgress.interval = null;
    }
     // Optionally reset global UI elements when stopping explicitly
     handleAllSoundsFinishedVisuals();
}

async function fetchSoundProgress() {
    // 1. Check polling conditions
    if (!soundProgress.polling) return;
    // Check size *before* fetching, slightly more efficient. A discovery poll
    // (on load or reconnect) runs anyway to find sounds started elsewhere.
    const discovering = soundProgress.discover;
    soundProgress.discover = false;
    if (soundProgress.activeSounds.size === 0 && !discovering) {
        stopProgressPolling("No active sounds remaining before fetch");
        return;
    }

    try {
        const playingSoundsData = await apiFetch('/api/sounds/progress');
        soundProgress.errorCount = 0; // Reset error count on success

        const currentPlayingIdsFromServer = new Set(playingSoundsData.map(s => s.id));
        let maxProgressPercentage = 0;
        // Determine overall playing state *from the latest API data*
        let anyPlayingNow = playingSoundsData.some(sound => !sound.paused);

        // 2. Update tracked sounds based on API response
        playingSoundsData.forEach(soundUpdate => {
            // Basic validation
            if (typeof soundUpdate.id === 'undefined' || typeof soundUpdate.soundId === 'undefined') {
                console.warn("Received progress update with missing ID:", soundUpdate);
                return;
            }

            let existingSound = soundProgress.activeSounds.get(soundUpdate.id);

            // Add if new, or update if existing
            if (!existingSound) {
                console.warn(`Progress update for untracked playingId ${soundUpdate.id} (Sound ${soundUpdate.soundId}). Adding to tracking.`);
                existingSound = { id: soundUpdate.id, soundId: soundUpdate.soundId, name: '?', lengthInMs: 0, readInMs: 0, paused: true, repeat: false };
                soundProgress.activeSounds.set(soundUpdate.id, existingSound);
                if (!state.currentlyPlaying.has(existingSound.soundId)) {
                    state.currentlyPlaying.set(existingSound.soundId, existingSound.id);
                }
            }

            // Update properties, providing defaults
            existingSound.readInMs = soundUpdate.readInMs ?? existingSound.readInMs;
            existingSound.paused = soundUpdate.paused ?? existingSound.paused;
            existingSound.repeat = soundUpdate.repeat ?? existingSound.repeat;
            existingSound.name = soundUpdate.name ?? existingSound.name;
            if (typeof soundUpdate.lengthInMs === 'number' && soundUpdate.lengthInMs > 0) {
                existingSound.lengthInMs = soundUpdate.lengthInMs;
            }

            // Calculate progress and update max percentage
            const isPaused = existingSound.paused;
            const lengthMs = existingSound.lengthInMs;
            const readMs = existingSound.readInMs;
            let percentage = 0;

            if (!isPaused && lengthMs > 0) {
                percentage = Math.min(100, Math.max(0, (readMs / lengthMs) * 100));
                maxProgressPercentage = Math.max(maxProgressPercentage, percentage); // Track max percentage

                // Update Individual Card Background (Only Grid View)
                if (soundsContainerEl && !soundsContainerEl.classList.contains('layout-list')) {
                    const soundCard = soundsContainerEl.querySelector(`.sound-card[data-sound-id="${existingSound.soundId}"]`);
                    if (soundCard) {
                        setCardProgress(soundCard, percentage);
                        if (!soundCard.classList.contains('playing')) {
                            soundCard.classList.add('playing');
                        }
                    }
                }
            } else {
                 // If paused or length is zero, ensure background is reset for grid items
                 if (soundsContainerEl && !soundsContainerEl.classList.contains('layout-list')) {
                     const soundCard = soundsContainerEl.querySelector(`.sound-card[data-sound-id="${existingSound.soundId}"]`);
                     // Only reset if it was visually playing before
                     if (soundCard && soundCard.classList.contains('playing')) {
                          // Don't remove 'playing' class if paused, just reset background
                          soundCard.style.background = '';
                          // updateSoundCardDisplay(existingSound.soundId, soundCard); // Avoid recursive updates if possible
                     }
                 }
            }
        });

        // 3. Handle sounds that finished
        const finishedSoundPlayingIds = [];
        soundProgress.activeSounds.forEach((soundData, playingId) => {
            if (!currentPlayingIdsFromServer.has(playingId)) {
                finishedSoundPlayingIds.push(playingId);
            }
        });

        finishedSoundPlayingIds.forEach(playingId => {
            const soundData = soundProgress.activeSounds.get(playingId);
            if (soundData) {
                console.log(`Sound instance ${playingId} (Sound ${soundData.soundId}) finished.`);
                state.currentlyPlaying.delete(soundData.soundId);
                soundProgress.activeSounds.delete(playingId);
                handleSoundFinishVisuals(soundData); // Reset individual card visuals
            }
        });

        // 4. Update Global State & UI
        // Update overall playing state based on the fetched data
        if (state.isAnythingPlayingUnpaused !== anyPlayingNow) {
            console.log(`Overall playing state changed based on API: ${state.isAnythingPlayingUnpaused} -> ${anyPlayingNow}`);
            state.isAnythingPlayingUnpaused = anyPlayingNow;
            // If playback stopped naturally (not via toggle button), ensure our toggle state reflects that
            if (!anyPlayingNow && state.playbackGloballyPausedByToggle) {
                 console.log("Playback stopped naturally, resetting playbackGloballyPausedByToggle state.");
                 state.playbackGloballyPausedByToggle = false;
            }
        }
        updatePlayPauseButtonIcon(); // Update button icon/visibility based on the new state
        updateTopBarProgressState(maxProgressPercentage); // <<<< CALL NEW FUNCTION HERE

        // 5. Final Check: Stop polling if nothing is left *after* cleanup
        if (soundProgress.activeSounds.size === 0) {
             state.isAnythingPlayingUnpaused = false; // Ensure state is false
             state.playbackGloballyPausedByToggle = false; // Ensure toggle state is false
             updatePlayPauseButtonIcon(); // Update UI one last time
             updateTopBarProgressState(0); // Reset progress bar
             stopProgressPolling("No active sounds after update");
        }
        announceProgress();

    } catch (error) {
        console.error('Error fetching sound progress:', error);
        soundProgress.errorCount++;
        if (soundProgress.errorCount >= soundProgress.maxErrors) {
             stopProgressPolling(`Max errors (${soundProgress.maxErrors}) reached`);
             state.currentlyPlaying.clear();
             soundProgress.activeSounds.clear();
             updatePlayingStateVisuals();
             handleAllSoundsFinishedVisuals();
             // The connection status comes from apiFetch; after a reconnect
             // checkForPlayingSounds starts polling again.
        }
        // Reset states on error
        state.isAnythingPlayingUnpaused = false;
        state.playbackGloballyPausedByToggle = false;
        updatePlayPauseButtonIcon();
        updateTopBarProgressState(0); // Reset progress bar on error
        announceProgress();
    }
}


// --- Visual Updates for Playback & Progress ---

function handleSoundPlayedVisuals(soundData) {
    // Only update the global indicator; individual card handled by polling
    updatePlayPauseButtonIcon(true);
}

function handleSoundFinishVisuals(soundData) {
    // Reset individual card background/style
    if (soundsContainerEl) {
        const soundCard = soundsContainerEl.querySelector(`.sound-card[data-sound-id="${soundData.soundId}"]`);
        if (soundCard) {
            soundCard.classList.remove('playing'); // Explicitly remove class (fades the fill out)
            scheduleProgressReset(soundCard);
            updateSoundCardDisplay(soundData.soundId, soundCard); // Re-apply base styles (color, emoji etc.)
        }
    }

    // Check if this was the *last* playing sound
    if (soundProgress.activeSounds.size === 0) {
         // Update global state and UI immediately
         state.isAnythingPlayingUnpaused = false;
         updatePlayPauseButtonIcon();
         updateTopBarProgressState(0); // Reset progress bar too
         console.log("Last sound finished, resetting global UI.");
    }
    // Global bar/icon reset is handled by handleAllSoundsFinishedVisuals or the next poll cycle
}

function handleAllSoundsFinishedVisuals() {
    console.log("Handling all sounds finished visuals (called by stopAll)");
    state.isAnythingPlayingUnpaused = false;
    state.isTalkThroughButtonPressed = false;
    state.playbackGloballyPausedByToggle = false; // Reset toggle pause state
    updatePlayPauseButtonIcon(); // Update icon AND visibility
    updateTopBarProgressState(0); // Reset progress bar state/visibility
    talkThroughButton?.classList.remove('active');

    // Reset all potentially playing sound card backgrounds and classes
    if (soundsContainerEl) {
        soundsContainerEl.querySelectorAll('.sound-card.playing').forEach(card => {
             card.classList.remove('playing');
             scheduleProgressReset(card);
             const soundId = parseInt(card.dataset.soundId);
             if (!isNaN(soundId)) {
                 updateSoundCardDisplay(soundId, card); // Re-apply base styles
             }
        });
    }

}

function updatePlayingStateVisuals() {
     // This function ensures the 'playing' class is correct based on state.currentlyPlaying
     // It's less critical now that polling handles background updates, but good for initial state
     if (!soundsContainerEl) return;
    soundsContainerEl.querySelectorAll('.sound-card').forEach(card => {
        const soundId = parseInt(card.dataset.soundId);
        if (isNaN(soundId)) return;

        const isPlaying = state.currentlyPlaying.has(soundId);
        if (card.classList.contains('playing') && !isPlaying) scheduleProgressReset(card);
        card.classList.toggle('playing', isPlaying);

        // If it's marked as not playing, ensure background is reset
        if (!isPlaying && card.style.background !== '') {
             card.style.background = '';
             updateSoundCardDisplay(soundId, card); // Re-apply base visuals
        }
    });
}

// Initial check for sounds already playing (called during init)
async function checkForPlayingSounds() {
    console.log("Checking for initially playing sounds...");
    // Start polling - the first fetch will sync the state
    soundProgress.discover = true;
    if (soundProgress.polling) fetchSoundProgress(); else startProgressPolling();
}

window.app = {
  reloadSoundsForCurrentTab: reloadSoundsForCurrentTab,
  updateSoundCardDisplay: updateSoundCardDisplay,
  // Used by tabs.js for the swipe previews.
  tabSoundsCache,
  fetchTabSounds,
  orderSounds,
  createSoundCardElement,
  // Used by sound-settings.js for the preview and its floating card.
  trackPlaying,
  playingSound: (playingId) => soundProgress.activeSounds.get(playingId) || null,
  setCardProgress,
  resetCardProgress,
  stopAllSounds,
  // Reset functions (keep if needed)
  // resetCurrentPageVisuals: resetCurrentPageVisuals,
  // resetCurrentPageLayout: resetCurrentPageLayout,
  // resetAllSettings: resetAllSettings,
  createSoundCardElement: createSoundCardElement // Expose if needed
};


// --- Initialization ---
document.addEventListener('DOMContentLoaded', init); // App init

