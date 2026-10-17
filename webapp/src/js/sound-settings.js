// State management
let currentSoundId = null;
let currentSoundData = null; // Stores details fetched for the current sound
let startX = 0, startY = 0;
let currentX = 0, currentY = 0;
let isDragging = false;
let dragAxis = null; // side sheet: 'x' closes it, 'y' scrolls it; decided by the first move
let cardHeight = 0, cardWidth = 0;
let currentPreviewId = null;
let emojiPickerInstance = null; // Reference to the emoji picker element INSIDE THE MODAL
let emojiPickerInputElement = null; // To store the reference to the search input inside the shadow DOM of the picker in the modal

// Elements (Settings Card)
const settingsOverlay = document.getElementById('sound-settings-overlay'); // Main settings slide-in
const settingsCard = document.getElementById('settings-card');
const settingsBackdrop = document.getElementById('settings-backdrop');
const soundNameElement = document.getElementById('settings-sound-name');
const tabNameElement = document.getElementById('settings-tab-name');
const favoriteButton = document.getElementById('favorite-button');
const previewButton = document.getElementById('preview-button');
const volumeSlider = document.getElementById('volume-slider');
const volumeInfoButton = document.getElementById('volume-info-button');
const volumeInfo = document.getElementById('volume-info');
const colorSelector = document.getElementById('color-selector');
// Emoji Elements (Settings Card)
const emojiDisplayArea = document.getElementById('emoji-display-area'); // Area showing selected emoji + clear button in settings
const selectedEmojiButton = document.getElementById('selected-emoji-button'); // Button showing the selected emoji in settings (not interactive itself)
const currentEmojiDisplay = document.getElementById('current-emoji-display'); // Span inside the selected-emoji-button in settings
const clearEmojiButtonSettings = document.getElementById('clear-emoji-button-settings'); // The clear button in settings
const selectEmojiButton = document.getElementById('select-emoji-button'); // Button to open the modal

// Elements (Emoji Picker Modal)
const emojiPickerModalOverlay = document.getElementById('emoji-picker-modal-overlay');
const emojiPickerModal = document.getElementById('emoji-picker-modal');
const emojiPickerContainer = document.getElementById('emoji-picker-container'); // Container for <emoji-picker> IN THE MODAL
const closeEmojiPickerButton = document.getElementById('close-emoji-picker-button');


// Initialize the settings menu
function initSoundSettings() {
    if (!settingsOverlay) {
        console.error("Settings overlay element not found.");
        return;
    }
    console.log("Initializing Sound Settings...");

    setupDragBehavior();
    setupButtonActions();
    setupVolumeInfo();
    setupColorSelection();
    setupEmojiSelection(); // Now handles modal logic
    setupLongPressDetection();
    setupRightClickBehavior();
    setupLiftedCard();

    settingsBackdrop?.addEventListener('click', closeSettingsMenu);
    document.addEventListener('soundprogress', syncPreview); // app.js, every poll
    window.addEventListener('resize', placeLifted);
    SIDE_SHEET_QUERY.addEventListener?.('change', placeLifted);
    // Esc closes the emoji picker, else the sheet (on PCs and keyboards).
    document.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || settingsOverlay.classList.contains('hidden')) return;
        if (emojiPickerModalOverlay && !emojiPickerModalOverlay.classList.contains('hidden')) closeEmojiPickerModal();
        else closeSettingsMenu();
    });
}

// --- MODIFIED: Function to set the selected emoji (called from modal) ---
function setEmoji(emoji) {
    if (!currentSoundId || !currentSoundData) return;

    persistence.setSoundEmoji(currentSoundId, emoji);

    // Update UI in the SETTINGS CARD immediately
    if (currentEmojiDisplay) currentEmojiDisplay.textContent = emoji;
    if (emojiDisplayArea) emojiDisplayArea.classList.remove('hidden'); // Show display area
    if (selectEmojiButton) selectEmojiButton.classList.add('hidden'); // Hide select button
    if (clearEmojiButtonSettings) clearEmojiButtonSettings.classList.remove('hidden'); // Show clear button

    refreshCards(currentSoundId);

    // Close the modal
    closeEmojiPickerModal();
}

// --- MODIFIED: Function to clear the emoji (called from settings card) ---
function clearEmoji() {
    if (!currentSoundId || !currentSoundData) return;
    persistence.clearSoundEmoji(currentSoundId);

    // Update UI in the SETTINGS CARD immediately
    if (currentEmojiDisplay) currentEmojiDisplay.textContent = '❓'; // Reset display text just in case
    if (emojiDisplayArea) emojiDisplayArea.classList.add('hidden'); // Hide display area
    if (selectEmojiButton) selectEmojiButton.classList.remove('hidden'); // Show select button
    if (clearEmojiButtonSettings) clearEmojiButtonSettings.classList.add('hidden'); // Hide clear button

    refreshCards(currentSoundId);

    // DO NOT open the modal automatically
}


// Set up long press detection
function setupLongPressDetection() {
    document.addEventListener('touchstart', (e) => {
        if (editMode.isActive()) return;
        handleTouchStart(e);
    }, { passive: false });
    document.addEventListener('touchmove', (e) => {
         if (editMode.isActive()) return;
         handleTouchMove(e);
     }, { passive: false });
    document.addEventListener('touchend', (e) => {
         if (editMode.isActive()) return;
         handleTouchEnd(e);
     }, { passive: false });
}

// Setup right-click
function setupRightClickBehavior() {
    document.addEventListener('contextmenu', (e) => {
        const soundCard = e.target.closest('.sound-card');
        if (soundCard) {
            e.preventDefault();
            if (!editMode.isActive() && !soundCard.closest('#sound-preview-stage')) {
                const soundId = parseInt(soundCard.dataset.soundId);
                if (window.soundSettings && typeof window.soundSettings.open === 'function') {
                   openSoundSettings(soundId);
                } else { console.error("Sound settings module not available."); }
           }
        }
        // Prevent context menu inside the settings card AND the emoji modal
        if (e.target.closest('#settings-card') || e.target.closest('#emoji-picker-modal')) {
            e.preventDefault();
        }
    });
}

// Touch variables for long press
let longPressTimer;
const longPressDuration = 500;
let longPressSoundCard = null;
let longPressStartPosition = null;
let hasMoved = false;
let longPressFired = false; // the timer opened the sheet during this touch

// Handle touch start for long press
function handleTouchStart(e) {
    // --- Check if the target is the PTT button or inside it ---
    if (e.target.closest('#talk-through-button')) {
        // console.log("Ignoring touchstart for long press detection on PTT button.");
        return; // DO NOT PROCEED for PTT button
    }
    // --- END Check ---

    // If we reach here, it's NOT the PTT button, proceed with sound card logic
    if (e.target.closest('#sound-preview-stage')) return; // the sheet's floating copy plays a preview
    if (e.touches.length > 1) { clearTimeout(longPressTimer); longPressSoundCard = null; return; } // two fingers: the search pull
    const soundCard = e.target.closest('.sound-card');
    if (!soundCard) {
        // console.log("Ignoring touchstart for long press detection - not on sound card.");
        return; // Ignore if not on a sound card either
    }

    // --- Start long press logic ONLY for sound cards ---
    // console.log("Starting long press detection for sound card."); // Optional debug
    longPressStartPosition = { x: e.touches[0].clientX, y: e.touches[0].clientY };
    hasMoved = false;
    longPressSoundCard = soundCard; // Track the target sound card

    // Clear any potentially existing timer from previous touches
    clearTimeout(longPressTimer);
    longPressTimer = null; // Explicitly nullify

    // Set the timer *only* for sound cards
    longPressTimer = setTimeout(() => {
        // --- THIS CHECK MUST REMAIN ---
        if (window.state && window.state.isTalkThroughButtonPressed) {
             console.log("Long press timer fired, but PTT is active. Aborting sound card long press action.");
             longPressSoundCard = null;
             return;
        }
        // Check conditions *when timer fires*
        if (!hasMoved && longPressSoundCard === soundCard) { // Ensure target hasn't changed
            // console.log("Long press timer fired for sound card."); // Optional debug
            // Vibration for sound card long press (KEEP THIS if you want haptic feedback for opening settings)
            // if (navigator.vibrate) navigator.vibrate(50);

            const soundId = parseInt(soundCard.dataset.soundId);
            // The click of this touch's release is not a tap (app.js, below).
            longPressFired = true;
            if (window.state) state.lastLongPressTime = Date.now();
            openSoundSettings(soundId);
        } else {
            // console.log("Long press timer fired, but conditions not met (moved or target changed)."); // Optional debug
        }
        // Clear target reference after timer execution
        longPressSoundCard = null;

    }, longPressDuration);
}


// Handle touch move for long press
function handleTouchMove(e) {
    if (!longPressSoundCard || !longPressStartPosition) return;
    const xDiff = Math.abs(e.touches[0].clientX - longPressStartPosition.x);
    const yDiff = Math.abs(e.touches[0].clientY - longPressStartPosition.y);
    if (xDiff > 10 || yDiff > 10) {
        hasMoved = true;
        clearTimeout(longPressTimer);
    }
}

// Handle touch end for long press. Ending a long press marks the time, so
// the click that may follow the release doesn't play the card (app.js) or
// preview the sheet's floating copy, which is under the finger by then. A
// plain tap leaves it alone.
function handleTouchEnd() {
    if (longPressFired && window.state) state.lastLongPressTime = Date.now();
    longPressFired = false;
    clearTimeout(longPressTimer);
    longPressSoundCard = null; longPressStartPosition = null; hasMoved = false;
}

// --- Bottom sheet or side sheet ---
// Phones in portrait get a bottom sheet (at most 520 px wide). Landscape and
// wide screens get a side sheet from the right, with the button's copy
// floating to its left (styles.css, same query). The sheet's shown and
// hidden transforms differ; SHEET_SHOWN works for both.
const SIDE_SHEET_QUERY = window.matchMedia('(min-aspect-ratio: 1/1) and (min-width: 600px)');
const isSideSheet = () => SIDE_SHEET_QUERY.matches;
const SHEET_SHOWN = 'translate(0, 0)';
const sheetHidden = () => isSideSheet() ? 'translateX(100%)' : 'translateY(100%)';

// Setup drag behavior for the settings card: down closes the bottom sheet,
// right closes the side sheet (whose vertical moves scroll it).
function setupDragBehavior() {
    if (!settingsCard) return;
    settingsCard.addEventListener('touchstart', onCardTouchStart, { passive: true });
    settingsCard.addEventListener('touchmove', onCardTouchMove, { passive: false });
    settingsCard.addEventListener('touchend', onCardTouchEnd, { passive: true });
}

// Handle card touch start for dragging
function onCardTouchStart(e) {
    // Prevent drag if interacting with interactive elements within the card
    if (e.target === volumeSlider ||
        e.target.closest('.color-selector') ||
        e.target.closest('#select-emoji-button') || // Prevent drag on select button
        e.target.closest('#emoji-display-area')) // Prevent drag on display area (incl. clear button)
    {
        return;
    }
    startX = currentX = e.touches[0].clientX;
    startY = currentY = e.touches[0].clientY;
    cardHeight = settingsCard.offsetHeight;
    cardWidth = settingsCard.offsetWidth;
    dragAxis = null;
    isDragging = true;
    settingsCard.style.transition = 'none';
}

// Handle card touch move for dragging
function onCardTouchMove(e) {
    if (!isDragging) return;
    currentX = e.touches[0].clientX;
    currentY = e.touches[0].clientY;
    const deltaX = currentX - startX;
    const deltaY = currentY - startY;
    if (isSideSheet()) {
        if (!dragAxis) {
            if (Math.abs(deltaX) < 10 && Math.abs(deltaY) < 10) return;
            dragAxis = Math.abs(deltaX) > Math.abs(deltaY) ? 'x' : 'y';
        }
        if (dragAxis !== 'x' || deltaX <= 0) return; // vertical: the sheet scrolls
        e.preventDefault();
        const translateX = Math.min(deltaX, cardWidth);
        settingsCard.style.transform = `translateX(${translateX}px)`;
        if (settingsBackdrop) settingsBackdrop.style.opacity = 1 - (translateX / cardWidth) * 0.5;
        return;
    }
    if (deltaY > 0) { // Only allow dragging down
        e.preventDefault();
        const translateY = Math.min(deltaY, cardHeight);
        settingsCard.style.transform = `translateY(${translateY}px)`;
        if (settingsBackdrop) settingsBackdrop.style.opacity = 1 - (translateY / cardHeight) * 0.5;
    }
}

// Handle card touch end for dragging
function onCardTouchEnd() {
    if (!isDragging) return;
    isDragging = false;
    settingsCard.style.transition = `transform 0.3s ${SHEET_EASE}`;
    if (settingsBackdrop) settingsBackdrop.style.transition = 'opacity 0.3s ease';
    const side = isSideSheet();
    const moved = side ? (dragAxis === 'x' ? currentX - startX : 0) : currentY - startY;
    if (moved > (side ? cardWidth : cardHeight) * 0.3) { closeSettingsMenu(); }
    else {
        settingsCard.style.transform = SHEET_SHOWN;
        if (settingsBackdrop) settingsBackdrop.style.opacity = '1';
    }
    startX = startY = currentX = currentY = 0;
}

// Setup button actions within the settings card
function setupButtonActions() {
    if (favoriteButton) {
        favoriteButton.addEventListener('click', () => {
            if (!currentSoundId || !currentSoundData) return;
            const newFavoriteState = !favoriteButton.classList.contains('active');
            fetch(`/api/sounds/${currentSoundId}/favorite`, { method: 'POST' })
            .then(response => response.json())
            .then(data => {
                if (data.success) {
                    favoriteButton.classList.toggle('active', data.isFavorite === true);
                    persistence.setSoundFavorite(currentSoundId, data.isFavorite === true);
                    if (window.app) {
                        refreshCards(currentSoundId);
                        app.tabSoundsCache.delete('favorites'); // the favorites tab changed
                        window.tabManager?.refreshNeighbors();
                    }
                }
            }).catch(err => console.error("Error toggling favorite:", err));
        });
    } else { console.error("Favorite button not found."); }

    if (previewButton) {
        previewButton.addEventListener('click', togglePreview);
    } else { console.error("Preview button not found."); }

    if (volumeSlider) {
        // Live while dragging: playing sounds (and previews) follow at once.
        volumeSlider.addEventListener('input', () => {
            let sliderPos = parseInt(volumeSlider.value);
            if (Math.abs(sliderPos) <= VOLUME_SNAP) { // magnetic centre
                sliderPos = 0;
                volumeSlider.value = 0;
            }
            if (sliderPos === 0 && lastSliderPos !== 0 && navigator.vibrate) navigator.vibrate(15);
            lastSliderPos = sliderPos;
            updateSliderFill();
            if (currentSoundId) updateVolumeWithSlider(sliderPos);
        });
        volumeSlider.addEventListener('change', () => {
            if (currentSoundId) updateVolumeWithSlider(parseInt(volumeSlider.value));
        });
    } else { console.error("Volume slider not found."); }


    // Clear button listener remains here as it's in the settings card
    if (clearEmojiButtonSettings) {
        clearEmojiButtonSettings.addEventListener('click', clearEmoji);
    } else { console.error("Clear emoji button (settings) not found."); }
}

// --- Custom button volume: the (i) after its title ---
// Tapping it shows what a custom volume does; the next tap anywhere, the
// sheet closing or another sound's sheet hides it.
function setupVolumeInfo() {
    if (!volumeInfoButton || !volumeInfo) return;
    volumeInfoButton.addEventListener('click', () => showVolumeInfo(volumeInfo.hidden));
    document.addEventListener('pointerdown', (e) => {
        if (!volumeInfo.hidden && !volumeInfoButton.contains(e.target)) showVolumeInfo(false);
    }, true);
}

function showVolumeInfo(show) {
    if (!volumeInfoButton || !volumeInfo) return;
    volumeInfo.hidden = !show;
    volumeInfoButton.setAttribute('aria-expanded', String(show));
}

// Setup color selection
function setupColorSelection() {
    if (!colorSelector) {
        console.error("Color selector element not found.");
        return;
    }
    colorSelector.addEventListener('click', (e) => {
        const swatch = e.target.closest('.color-swatch');
        if (swatch && currentSoundId && currentSoundData) {
            const color = swatch.dataset.color;
            persistence.setSoundColor(currentSoundId, color);
            colorSelector.querySelectorAll('.color-swatch').forEach(s => s.classList.remove('selected'));
            swatch.classList.add('selected');
            refreshCards(currentSoundId);
        }
    });
}

// --- MODIFIED: Setup Emoji Selection (Modal Logic) ---
function setupEmojiSelection() {
    // Check required elements for both settings card and modal
    if (!emojiDisplayArea || !clearEmojiButtonSettings || !currentEmojiDisplay || !selectEmojiButton ||
        !emojiPickerModalOverlay || !emojiPickerModal || !emojiPickerContainer || !closeEmojiPickerButton) {
        console.error("One or more required emoji elements (settings or modal) not found.");
        return;
    }

    // Get the picker instance from the MODAL
    emojiPickerInstance = emojiPickerContainer.querySelector('emoji-picker');
    if (!emojiPickerInstance) {
        console.error("emoji-picker element not found within the modal container.");
        return;
    }

    // 1. "Select Background Emoji" (no emoji yet), or the current emoji: the
    // picker opens, and the emoji chosen there replaces it.
    selectEmojiButton.addEventListener('click', openEmojiPickerModal);
    selectedEmojiButton?.addEventListener('click', openEmojiPickerModal);

    // 2. Listener for selecting an emoji within the MODAL
    emojiPickerInstance.addEventListener('emoji-click', event => {
        const selectedEmoji = event.detail.unicode;
        console.log("Emoji selected in modal:", selectedEmoji);
        setEmoji(selectedEmoji); // This will update state, update settings UI, and close the modal
    });

    // 3. Listener for the "Cancel" button in the MODAL
    closeEmojiPickerButton.addEventListener('click', closeEmojiPickerModal);

    // 4. Listener for clicking the modal overlay (backdrop) to close
    emojiPickerModalOverlay.addEventListener('click', (event) => {
        // Only close if the click is directly on the overlay, not the modal content
        if (event.target === emojiPickerModalOverlay) {
            closeEmojiPickerModal();
        }
    });

    // 5. Listener for the clear button remains in setupButtonActions as it's part of the main settings card.

    // 6. Find the search input within the picker's shadow DOM (for focusing later)
    // Use requestAnimationFrame or a small delay if shadow DOM isn't ready immediately
    requestAnimationFrame(() => {
        if (emojiPickerInstance && emojiPickerInstance.shadowRoot) {
            emojiPickerInputElement = emojiPickerInstance.shadowRoot.querySelector('input[type="search"]');
            if (emojiPickerInputElement) {
                console.log("Emoji search input found in modal picker.");
            } else {
                console.warn("Could not find search input within modal emoji-picker shadow DOM.");
            }
        } else {
             console.warn("Modal emoji picker instance or its shadow DOM not ready for input search binding.");
        }
    });
}

// --- NEW: Function to open the emoji picker modal ---
function openEmojiPickerModal() {
    if (!emojiPickerModalOverlay) return;
    console.log("Opening emoji picker modal.");
    emojiPickerModalOverlay.classList.remove('hidden');
    clearEmojiSearch();

    // Focus the search input after the modal is potentially visible
    focusEmojiSearchInput();
}

// clearEmojiSearch empties the picker's search, so it opens on all emoji
// rather than the last search. The picker reacts to its input event.
function clearEmojiSearch() {
    const input = emojiPickerInputElement || emojiPickerInstance?.shadowRoot?.querySelector('input[type="search"]');
    if (!input || !input.value) return;
    input.value = '';
    input.dispatchEvent(new Event('input', { bubbles: true }));
}

// --- NEW: Function to close the emoji picker modal ---
function closeEmojiPickerModal() {
    if (!emojiPickerModalOverlay) return;
    console.log("Closing emoji picker modal.");
    // Optional: Add animation class for fade out before hiding
    emojiPickerModalOverlay.classList.add('hidden');
    // Optional: Remove animation class after transition
    // emojiPickerModal.classList.remove('visible');
}

// --- NEW: Function to focus the search input inside the modal picker ---
function focusEmojiSearchInput() {
    // Use a small delay or RAF to ensure the element is visible and focusable after modal opens
    requestAnimationFrame(() => {
        if (emojiPickerInputElement) {
            emojiPickerInputElement.focus();
            console.log("Attempted to focus emoji search input.");
        } else {
            console.warn("Emoji search input element not available for focus.");
            // Retry after a short delay if it wasn't found initially
            setTimeout(() => {
                 if (emojiPickerInstance && emojiPickerInstance.shadowRoot && !emojiPickerInputElement) {
                    emojiPickerInputElement = emojiPickerInstance.shadowRoot.querySelector('input[type="search"]');
                    if(emojiPickerInputElement) {
                        emojiPickerInputElement.focus();
                        console.log("Attempted to focus emoji search input after delay.");
                    } else {
                         console.error("Still couldn't find emoji search input after delay.");
                    }
                 }
            }, 200);
        }
    });
}

// --- REMOVED Floating Logic (handleEmojiInputFocus, handleEmojiInputBlur) ---

// --- Volume slider ---
const VOLUME_SNAP = 4;     // positions around the centre that snap to it
const SLIDER_THUMB_PX = 22; // matches the thumb width in styles.css
let lastSliderPos = 0;

// updateSliderFill colours the track from the centre to the thumb. The thumb
// travels thumb-width less than the track, hence the correction.
function updateSliderFill() {
    if (!volumeSlider) return;
    const p = (parseInt(volumeSlider.value) + 50) / 100; // 0..1
    const at = `calc(${p * 100}% + ${(0.5 - p) * SLIDER_THUMB_PX}px)`;
    volumeSlider.style.setProperty('--fill-from', p < 0.5 ? at : '50%');
    volumeSlider.style.setProperty('--fill-to', p < 0.5 ? '50%' : at);
}

function setSliderPosition(pos) {
    lastSliderPos = pos;
    if (volumeSlider) volumeSlider.value = pos;
    updateSliderFill();
}

// updateVolumeWithSlider sends the slider position to the PC, which applies
// it to the saved volume and to the sound if it is playing. One request at a
// time: while one is under way only the newest position waits, so updates
// stay in order and the volume follows the finger at network speed.
let volumeInFlight = false;
let volumePending = null; // { soundId, pos }
function updateVolumeWithSlider(sliderPos) {
    if (!currentSoundId || !currentSoundData) return;
    const job = { soundId: currentSoundId, pos: sliderPos };
    if (volumeInFlight) { volumePending = job; return; }
    volumeInFlight = true;
    fetch(`/api/sounds/${job.soundId}/volume`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ sliderPosition: job.pos })
    })
    .then(response => response.json())
    .then(data => {
        if (!data.success) return;
        persistence.setSoundCustomVolume(job.soundId, job.pos !== 0, job.pos);
        refreshCards(job.soundId);
        if (job.soundId === currentSoundId && currentSoundData) {
            currentSoundData.hasCustomVolume = (job.pos !== 0);
            currentSoundData.sliderPosition = job.pos;
        }
    })
    .catch(error => console.error('Error updating volume:', error))
    .finally(() => {
        volumeInFlight = false;
        const next = volumePending;
        volumePending = null;
        if (next && !(next.soundId === job.soundId && next.pos === job.pos) && next.soundId === currentSoundId) {
            updateVolumeWithSlider(next.pos);
        }
    });
}

// Open settings menu and load data
function openSoundSettings(soundId) {
    if (!settingsOverlay) return;
    currentSoundId = soundId;

    resetMenuState(); // Reset visuals first

    fetch(`/api/sounds/${soundId}`)
    .then(response => response.json())
    .then(sound => {
        if (!sound || !sound.id || !sound.path) throw new Error("Sound data not found or invalid.");

        currentSoundData = sound;
        persistence.ingestSounds([sound]); // the PC's current color, emoji and favorite
        const savedSoundSettings = persistence.getSoundSettings(sound.id);

        // Populate basic info
        if (soundNameElement) soundNameElement.textContent = sound.name || 'Unknown Sound';
        if (tabNameElement) tabNameElement.textContent = sound.tabName || 'Unknown Tab';
        if (favoriteButton) favoriteButton.classList.toggle('active', sound.isFavorite === true);

        // Set color state
        if (colorSelector) {
            colorSelector.querySelectorAll('.color-swatch').forEach(s => s.classList.remove('selected'));
            const color = savedSoundSettings.color || 'default';
            const activeSwatch = colorSelector.querySelector(`.color-swatch[data-color="${color}"]`);
            if (activeSwatch) activeSwatch.classList.add('selected');
        }

        // Set volume slider state
        const sliderPosition = sound.sliderPosition ?? 0;
        setSliderPosition(sliderPosition);

        // --- MODIFIED: Emoji Visibility Logic (Settings Card) ---
        const currentEmoji = savedSoundSettings.emoji;
        if (currentEmoji) {
            // Emoji IS set: Show display area, hide select button
            if (currentEmojiDisplay) currentEmojiDisplay.textContent = currentEmoji;
            if (emojiDisplayArea) emojiDisplayArea.classList.remove('hidden');
            if (selectEmojiButton) selectEmojiButton.classList.add('hidden');
            if (clearEmojiButtonSettings) clearEmojiButtonSettings.classList.remove('hidden');
        } else {
            // NO emoji set: Hide display area, show select button
            if (currentEmojiDisplay) currentEmojiDisplay.textContent = '❓'; // Reset just in case
            if (emojiDisplayArea) emojiDisplayArea.classList.add('hidden');
            if (selectEmojiButton) selectEmojiButton.classList.remove('hidden');
            if (clearEmojiButtonSettings) clearEmojiButtonSettings.classList.add('hidden');
        }
        // --- End Modified Emoji Visibility Logic ---

        // The sheet slides in (up, or from the right) while the button lifts
        // out of the grid into the space beside it.
        settingsOverlay.classList.remove('hidden');
        settingsCard.scrollTop = 0; // a side sheet scrolls
        liftCard(sound);
        requestAnimationFrame(() => {
            if (settingsBackdrop) settingsBackdrop.style.opacity = '1';
            settingsCard.style.transform = SHEET_SHOWN;
        });

    })
    .catch(error => {
        console.error('Failed to fetch sound details:', error);
        currentSoundId = null; currentSoundData = null;
        closeSettingsMenu(); // Close the main settings menu on error
        closeEmojiPickerModal(); // Ensure modal is also closed if it was somehow open
        console.error("Failed to load sound details.");
    });
}


// Reset the menu state to default
function resetMenuState() {
    if (!settingsOverlay) return;

    // Reset Buttons and Slider
    if (currentPreviewId !== null) stopPreview(); else showPreviewStopped();
    showVolumeInfo(false);

    setSliderPosition(0);
    if (favoriteButton) favoriteButton.classList.remove('active');

    // Reset Color Selector
    if (colorSelector) {
        colorSelector.querySelectorAll('.color-swatch').forEach(s => s.classList.remove('selected'));
        const defaultSwatch = colorSelector.querySelector('.color-swatch[data-color="default"]');
        if (defaultSwatch) defaultSwatch.classList.add('selected');
    }

    // --- MODIFIED: Emoji Reset Logic ---
    // Default to showing the "Select Emoji" button and hiding the display area
    if (currentEmojiDisplay) currentEmojiDisplay.textContent = '❓'; // Reset display text
    if (emojiDisplayArea) emojiDisplayArea.classList.add('hidden');
    if (selectEmojiButton) selectEmojiButton.classList.remove('hidden'); // Show select button by default
    if (clearEmojiButtonSettings) clearEmojiButtonSettings.classList.add('hidden');

    // Reset Text Fields
    if (soundNameElement) soundNameElement.textContent = 'Loading...';
    if (tabNameElement) tabNameElement.textContent = '';

    // --- REMOVED Floating Picker State Cleanup ---
}


// Close settings menu
function closeSettingsMenu() {
    if (!settingsOverlay || !settingsCard) return;
    settingsCard.style.transform = sheetHidden();
    if (settingsBackdrop) settingsBackdrop.style.opacity = '0';
    dropCard();
    showVolumeInfo(false);

    if (currentPreviewId !== null) stopPreview();

    // Ensure the emoji modal is also closed when the main settings menu closes
    closeEmojiPickerModal();

    setTimeout(() => {
        settingsOverlay.classList.add('hidden');
        currentSoundId = null; currentSoundData = null;
        // Reset styles after transition
        if (settingsCard) settingsCard.style.transform = '';
        if (settingsBackdrop) settingsBackdrop.style.opacity = '';
        // Ensure modal is definitely hidden after animation
        if (emojiPickerModalOverlay) emojiPickerModalOverlay.classList.add('hidden');

    }, 300); // Match transition duration
}

// --- Preview ---
// A preview plays the sound to "You hear" only. The progress polling follows
// it (app.trackPlaying): while it plays, the preview button reads "Stop" and
// fills with its progress, like the floating card; when it ends, both return
// to normal. The PC stops sounds only all at once, so "Stop" stops all.

async function togglePreview() {
    if (!currentSoundId) return;
    if (currentPreviewId !== null) {
        stopPreview();
        return;
    }
    window.haptics?.tap();
    const soundId = currentSoundId;
    try {
        const response = await fetch(`/api/sounds/${soundId}/preview`, { method: 'POST' });
        const data = await response.json();
        if (!data.success || data.playingId === undefined || soundId !== currentSoundId) return;
        currentPreviewId = data.playingId;
        showPreviewPlaying();
        app.trackPlaying(soundId, data);
    } catch (err) {
        console.error('Error starting preview:', err);
    }
}

function stopPreview() {
    currentPreviewId = null;
    showPreviewStopped();
    app.stopAllSounds(); // clears the playing state at once
}

// syncPreview follows the preview on every progress poll.
function syncPreview() {
    if (currentPreviewId === null) return;
    const playing = app.playingSound(currentPreviewId);
    if (!playing) { // finished
        currentPreviewId = null;
        showPreviewStopped();
        return;
    }
    const p = playing.lengthInMs > 0 ? Math.min(1, playing.readInMs / playing.lengthInMs) : 0;
    previewButton?.style.setProperty('--progress', String(p));
    if (lifted) {
        lifted.card.classList.add('playing');
        app.setCardProgress(lifted.card, p * 100);
    }
}

function showPreviewPlaying() {
    if (!previewButton) return;
    resetPreviewFill();
    previewButton.setAttribute('data-state', 'stop');
    previewButton.querySelector('.icon').dataset.icon = 'stop_circle';
    previewButton.querySelector('.button-text').textContent = 'Stop';
}

function showPreviewStopped() {
    if (previewButton) {
        previewButton.setAttribute('data-state', 'preview');
        previewButton.querySelector('.icon').dataset.icon = 'volume_up';
        previewButton.querySelector('.button-text').textContent = 'Preview';
    }
    if (lifted) {
        const card = lifted.card;
        card.classList.remove('playing'); // the fill fades out, then empties
        setTimeout(() => { if (!card.classList.contains('playing')) app.resetCardProgress(card); }, 400);
    }
}

// resetPreviewFill empties the button's fill without animating back.
function resetPreviewFill() {
    previewButton.classList.add('progress-reset');
    previewButton.style.setProperty('--progress', '0');
    void previewButton.offsetWidth;
    previewButton.classList.remove('progress-reset');
}

// --- The button above the sheet ---
// While the sheet is open, a copy of the sound's button floats above it: it
// lifts out of the grid as the sheet slides up and settles back into place
// when it closes. It is built and updated like the grid's cards, so colour,
// emoji, volume and favourite changes show on it at once, and tapping it
// previews the sound. It has the page's layout: a list row in the list
// layout, as wide as the row and not enlarged.

const SHEET_EASE = 'cubic-bezier(0.2, 0.9, 0.25, 1)';
const LIFT_MS = 320;
let lifted = null; // { card, soundId, w, h, list }
let previewStage = null;

function setupLiftedCard() {
    previewStage = document.createElement('div');
    previewStage.id = 'sound-preview-stage';
    settingsOverlay.append(previewStage); // above the backdrop and the sheet
    previewStage.addEventListener('click', (e) => {
        if (!e.target.closest('.sound-card')) return;
        // The copy rises under the finger that long-pressed the card: the
        // click of that release is not a tap on it (like app.js's grid).
        if (Date.now() - (window.state?.lastLongPressTime || 0) < 350) return;
        togglePreview();
    });
}

// gridCard is the sound's card on the live page, if it is there.
const gridCard = (soundId) =>
    document.querySelector(`#sounds-container .sound-card[data-sound-id="${soundId}"]`);

// onScreen: a rect that is at least partly visible.
const onScreen = (r) => r.bottom > 0 && r.top < window.innerHeight && r.width > 0;

function liftCard(sound) {
    dropCard(true);
    if (!previewStage || !window.app) return;
    const container = document.getElementById('sounds-container');
    const layout = [...(container?.classList || [])].find(c => c.startsWith('layout-')) || 'layout-grid-3';
    const list = layout === 'layout-list';
    previewStage.className = 'sounds-grid ' + layout;

    const source = gridCard(sound.id);
    const rect = source?.getBoundingClientRect();
    // A list row keeps its height and gets the room's width, at most the
    // sheet's (a row on a PC spans the screen); a grid button keeps its size.
    const area = floatArea();
    const w = list ? Math.min(rect ? rect.width : window.innerWidth - 20, area.right - area.left, 520)
        : rect ? rect.width : Math.min(160, window.innerWidth * 0.4);
    const h = rect ? rect.height : list ? 44 : 85;

    // Card colours, emoji and marks show in every layout (app.js).
    const card = app.createSoundCardElement(sound, { animate: false, tabId: null }); // tabId null: shows the favourite mark
    card.style.width = w + 'px';
    card.style.height = h + 'px';
    card.setAttribute('aria-label', 'Preview ' + (sound.name || 'sound'));
    previewStage.append(card);
    lifted = { card, soundId: sound.id, w, h, list };

    const to = floatTransform(w, h, list);
    card.style.transform = to.transform;

    let from;
    if (rect && onScreen(rect)) {
        from = { transform: `translate(${rect.left + (rect.width - w) / 2}px, ${rect.top}px) scale(1)`, opacity: 1 };
        source.classList.add('lifted'); // the copy takes its place
    } else {
        from = { transform: `translate(${to.x}px, ${to.y + 40}px) scale(${to.scale * 0.9})`, opacity: 0 };
    }
    card.animate([from, { transform: to.transform, opacity: 1 }], { duration: LIFT_MS, easing: SHEET_EASE });
}

// floatArea is where the copy floats: the space above a bottom sheet, or
// left of a side sheet.
function floatArea() {
    const top = parseFloat(getComputedStyle(previewStage).paddingTop) + 16;
    if (isSideSheet()) {
        return { left: 16, right: window.innerWidth - settingsCard.offsetWidth - 16, top, bottom: window.innerHeight - 16 };
    }
    return { left: 10, right: window.innerWidth - 10, top, bottom: window.innerHeight - settingsCard.offsetHeight - 16 };
}

// floatTransform centres a w×h copy in floatArea: a grid button enlarged
// (up to 1.6×, with a margin), a list row at its size.
function floatTransform(w, h, list) {
    const a = floatArea();
    const roomW = a.right - a.left - (list ? 0 : 28);
    const roomH = a.bottom - a.top;
    const scale = Math.max(0.5, Math.min(list ? 1 : 1.6, roomW / w, roomH / h));
    const x = a.left + (a.right - a.left - w * scale) / 2;
    const y = a.top + Math.max(0, (roomH - h * scale) / 2);
    return { x, y, scale, transform: `translate(${x}px, ${y}px) scale(${scale})` };
}

// placeLifted moves the copy when the screen turns or the window resizes,
// which may also switch between bottom and side sheet.
function placeLifted() {
    if (lifted) lifted.card.style.transform = floatTransform(lifted.w, lifted.h, lifted.list).transform;
}

// dropCard settles the copy back onto its card in the grid (or fades it
// out where that card isn't on screen). instant: remove it at once.
function dropCard(instant) {
    const l = lifted;
    lifted = null;
    if (!l) return;
    const done = () => {
        l.card.remove();
        document.querySelectorAll('#sounds-container .sound-card.lifted').forEach(c => c.classList.remove('lifted'));
    };
    if (instant) { done(); return; }
    const source = gridCard(l.soundId);
    const rect = source?.getBoundingClientRect();
    const to = rect && onScreen(rect)
        ? { transform: l.list // a row back to its place, at its width
            ? `translate(${rect.left + (rect.width - l.w) / 2}px, ${rect.top}px) scale(1)`
            : `translate(${rect.left}px, ${rect.top}px) scale(${rect.width / l.w})`, opacity: 1 }
        : { transform: l.card.style.transform + ' scale(0.85)', opacity: 0 };
    const anim = l.card.animate([{ transform: l.card.style.transform, opacity: 1 }, to],
        { duration: 280, easing: SHEET_EASE, fill: 'forwards' });
    let finished = false;
    const finish = () => { if (!finished) { finished = true; done(); } };
    anim.onfinish = finish;
    setTimeout(finish, 400); // in case the animation never reports back
}

// refreshCards shows a changed setting on the sound's grid card and on the
// floating copy.
function refreshCards(soundId) {
    if (!window.app) return;
    app.updateSoundCardDisplay(soundId);
    if (lifted && String(lifted.soundId) === String(soundId)) app.updateSoundCardDisplay(soundId, lifted.card, null);
}

// Initialize when DOM content is loaded
document.addEventListener('DOMContentLoaded', initSoundSettings);

// Expose openSoundSettings globally
window.soundSettings = { open: openSoundSettings };
