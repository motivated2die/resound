// --- ENTIRE FILE REPLACE ---
const dragDropManager = (() => {
    let sortableInstance = null;
    const soundsContainer = document.getElementById('sounds-container');

    // --- Scrolling while dragging ---
    // With the finger in the band along the grid's top or bottom edge, the
    // grid scrolls that way: slowly where the band begins, faster towards
    // the edge, fastest on it or beyond (over the top bar or the tabs).
    // Sortable checks what is under the finger every 50 ms, so the outline
    // follows the scrolling buttons even while the finger rests.
    const autoScroll = (() => {
        const EDGE = 80;        // px: the band along each edge
        const MAX_SPEED = 900;  // px/s at the edge
        let active = false;
        let y = null;           // the finger's (or mouse's) clientY
        let last = 0;
        let carry = 0;          // sub-pixel scroll not applied yet

        const track = (e) => {
            const p = e.touches ? e.touches[0] : e;
            if (p && typeof p.clientY === 'number') y = p.clientY;
        };
        // speed for a finger `inside` px into the band (negative: past the edge)
        const speed = (inside) => MAX_SPEED * Math.min(1, Math.max(0, (EDGE - inside) / EDGE)) ** 2;

        const frame = (now) => {
            if (!active) return;
            const dt = last ? Math.min(50, now - last) / 1000 : 0;
            last = now;
            if (y !== null) {
                // The visible part of the grid: it runs on under the tab bar
                // and the bottom bar, which cover it.
                const r = soundsContainer.getBoundingClientRect();
                const tabs = document.getElementById('tabs-container')?.getBoundingClientRect();
                const top = r.top;
                const bottom = tabs && tabs.top > top ? Math.min(r.bottom, tabs.top) : r.bottom;
                const band = Math.min(EDGE, (bottom - top) / 3); // short grids keep a middle
                let v = 0;
                if (y - top < band) v = -speed((y - top) * EDGE / band);
                else if (bottom - y < band) v = speed((bottom - y) * EDGE / band);
                carry += v * dt;
                const step = Math.trunc(carry);
                if (step) {
                    soundsContainer.scrollTop += step;
                    carry -= step;
                }
                if (!v) carry = 0;
            }
            requestAnimationFrame(frame);
        };

        const start = (evt) => {
            if (active) return;
            active = true;
            y = null;
            last = 0;
            carry = 0;
            if (evt) track(evt);
            for (const t of ['touchmove', 'pointermove', 'mousemove']) document.addEventListener(t, track, { capture: true, passive: true });
            requestAnimationFrame(frame);
        };
        const stop = () => {
            active = false;
            for (const t of ['touchmove', 'pointermove', 'mousemove']) document.removeEventListener(t, track, { capture: true });
        };
        return { start, stop };
    })();

    const initSortable = () => {
        if (!soundsContainer || typeof Sortable === 'undefined') {
            console.error("Drag & Drop Error: Sounds container or SortableJS library not found.");
            return;
        }

        // Swapping: the held button stays in its place, raised
        // (.sortable-ghost / .sortable-chosen), and nothing follows the
        // finger; the button it would swap with shows a dashed outline. The
        // pointer-based drag (forceFallback) works alike with touch and mouse
        // and draws no browser drag image; its copy under the finger is
        // hidden (.sortable-fallback). Near the grid's top or bottom edge it
        // scrolls (autoScroll, below).
        sortableInstance = new Sortable(soundsContainer, {
            animation: 150,
            ghostClass: 'sortable-ghost',   // the held button, in its place
            chosenClass: 'sortable-chosen',  // the held button, from the press on
            dragClass: 'sortable-drag',    // the copy under the finger
            swap: true,                     // *** ENABLE SWAP BEHAVIOR ***
            swapClass: 'sortable-swap-highlight', // Class applied to the item being swapped *with*
            forceFallback: true,
            fallbackClass: 'sortable-fallback',
            fallbackOnBody: true,
            scroll: false,                  // autoScroll scrolls gradually instead

            delay: 100,                      // ms delay to prevent accidental drags on tap
            delayOnTouchOnly: true,
            touchStartThreshold: 8,         // Pixels tolerance for touch start

            disabled: !editMode.isActive(), // Start disabled if edit mode isn't active

            onStart: function (evt) {
                soundsContainer.style.cursor = 'grabbing';
                if (navigator.vibrate) {
                    navigator.vibrate(40); // Haptic feedback
                }
                autoScroll.start(evt.originalEvent);
            },

            onEnd: function (evt) {
                autoScroll.stop();
                // Reset cursor immediately
                soundsContainer.style.cursor = editMode.isActive() ? 'grab' : 'pointer';

                // Check if the item actually moved position logicaly
                // In swap mode, oldIndex and newIndex represent the items swapped
                if (evt.oldIndex === evt.newIndex) {
                    console.log("Item swap ended at the same logical position, no order change needed.");
                    return;
                }

                console.log(`Swap drag ended. Item at index ${evt.oldIndex} swapped with item at index ${evt.newIndex}.`);

                // Get the current tab and layout context
                const currentTabId = state.currentTab; // Assumes state.currentTab is string 'favorites' or tab ID string
                const currentLayout = persistence.getCurrentLayoutMode(currentTabId);

                if (currentTabId === null || typeof currentTabId === 'undefined' || !currentLayout) {
                    console.error("Cannot save swapped order: Invalid current tab or layout.", { currentTabId, currentLayout });
                    return;
                }

                // --- Get the CURRENT persisted order ---
                const persistedOrderPaths = persistence.getLayoutOrder(currentTabId, currentLayout); // sound IDs

                // --- Check if persisted order exists before attempting swap ---
                if (!Array.isArray(persistedOrderPaths)) {
                     console.warn(`Cannot save swapped order: No persisted order found for tab ${currentTabId}, layout ${currentLayout}. Initializing order from current DOM.`);
                     // Fallback: Read from DOM if persistence is missing (less ideal but recovers)
                     const soundCards = soundsContainer.querySelectorAll('.sound-card');
                     const currentDomPaths = Array.from(soundCards).map(card => card.dataset.soundId).filter(id => id);
                     if (currentDomPaths.length > 0) {
                         persistence.setLayoutOrder(currentTabId, currentLayout, currentDomPaths);
                         // Now try the swap again *if indices are valid* for the DOM order
                         if (evt.oldIndex < currentDomPaths.length && evt.newIndex < currentDomPaths.length) {
                             const temp = currentDomPaths[evt.oldIndex];
                             currentDomPaths[evt.oldIndex] = currentDomPaths[evt.newIndex];
                             currentDomPaths[evt.newIndex] = temp;
                             persistence.setLayoutOrder(currentTabId, currentLayout, currentDomPaths);
                             console.log("Initialized and saved swapped order from DOM.");
                         } else {
                             console.error("Swap indices out of bounds even for DOM order. Cannot save.");
                         }
                     } else {
                         console.error("Cannot initialize order from DOM: No sound paths found.");
                     }
                     return; // Stop here if we had to initialize
                }


                // --- Perform the swap on the PERSISTED order array ---
                const newOrderPaths = [...persistedOrderPaths]; // Create a copy

                // Validate indices against the persisted array length
                 if (evt.oldIndex < 0 || evt.oldIndex >= newOrderPaths.length || evt.newIndex < 0 || evt.newIndex >= newOrderPaths.length) {
                     console.error(`Cannot save swapped order: Indices (old: ${evt.oldIndex}, new: ${evt.newIndex}) are out of bounds for persisted order length (${newOrderPaths.length}).`);
                     // Optionally reload to fix visual state if it desynced
                     // window.app?.reloadSoundsForCurrentTab();
                     return;
                 }

                console.log(`Original persisted order for [${currentTabId}-${currentLayout}]:`, persistedOrderPaths);
                console.log(`Swapping indices ${evt.oldIndex} ('${newOrderPaths[evt.oldIndex]}') and ${evt.newIndex} ('${newOrderPaths[evt.newIndex]}')`);

                // The actual swap
                const temp = newOrderPaths[evt.oldIndex];
                newOrderPaths[evt.oldIndex] = newOrderPaths[evt.newIndex];
                newOrderPaths[evt.newIndex] = temp;

                console.log(`Saving swapped order for [${currentTabId}-${currentLayout}]:`, newOrderPaths);

                // --- Save the modified order back to persistence ---
                try {
                    persistence.setLayoutOrder(currentTabId, currentLayout, newOrderPaths);
                    console.log("Successfully saved swapped layout order.");
                } catch (error) {
                    console.error("Failed to save swapped layout order:", error);
                    // If saving fails, the visual state might be desynced from persistent state.
                    // Reloading is a safe way to recover.
                    // alert("Error saving layout change. Reloading sounds.");
                    // window.app?.reloadSoundsForCurrentTab();
                }

                // IMPORTANT: Do NOT call evt.preventDefault(). Let SortableJS finalize the visual swap.
                // The persisted data now matches the visual outcome of the swap.
            },
        });
        console.log("SortableJS initialized for Drag & Drop (Swap Mode).");
    };

    const enable = () => {
        if (sortableInstance) {
            sortableInstance.option('disabled', false);
             soundsContainer.style.cursor = 'grab';
             console.log("Drag & Drop enabled.");
        } else {
             console.warn("Attempted to enable Drag & Drop, but Sortable instance not found.");
        }
    };

    const disable = () => {
        if (sortableInstance) {
            sortableInstance.option('disabled', true);
            soundsContainer.style.cursor = 'pointer';
            console.log("Drag & Drop disabled.");
        } else {
             console.warn("Attempted to disable Drag & Drop, but Sortable instance not found.");
        }
    };

    const init = () => {
        // Ensure SortableJS is loaded and container exists before initializing
        document.addEventListener('appReady', () => {
            console.log("App ready, initializing SortableJS for Drag & Drop...");
            if (typeof Sortable !== 'undefined' && document.getElementById('sounds-container')) {
                initSortable();
                // Listen for edit mode changes AFTER initializing sortable
                document.addEventListener('editModeChanged', (event) => {
                    const isActive = event.detail.isActive;
                    if (sortableInstance) { // Check instance exists before enabling/disabling
                        if (isActive) {
                            enable();
                        } else {
                            disable();
                        }
                    }
                });
                // Initialize state based on current editMode status
                if (editMode.isActive() && sortableInstance) {
                     enable();
                 } else if (sortableInstance){
                     disable();
                 }
            } else {
                console.error("SortableJS library not loaded or sounds-container not found when appReady fired.");
            }
        });
    };

    return {
        init
    };
})();

// Initialize dragDropManager setup listener
document.addEventListener('DOMContentLoaded', dragDropManager.init);
// --- END OF FILE ---