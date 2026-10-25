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

    // Buttons move only in edit mode, and only on a tab in its custom
    // order: a sorted tab (sort.js) is arranged by its sorting.
    const canDrag = () => editMode.isActive() && sortManager.isCustom(state.currentTab);

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

            disabled: !canDrag(), // only in edit mode, on a tab in its custom order

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

                // The swap is in the page already (Sortable's swap plugin
                // runs before onEnd), so the new custom order is the order on
                // screen. Sounds of the saved order that aren't on screen
                // (missing on the PC for now) stay in it, at the end.
                const currentTabId = state.currentTab; // 'favorites' or a tab ID string
                const currentLayout = persistence.getCurrentLayoutMode(currentTabId);
                if (currentTabId === null || typeof currentTabId === 'undefined' || !currentLayout) {
                    console.error("Cannot save swapped order: Invalid current tab or layout.", { currentTabId, currentLayout });
                    return;
                }
                const shown = Array.from(soundsContainer.querySelectorAll('.sound-card'))
                    .map(card => card.dataset.soundId)
                    .filter(id => id);
                if (!shown.length) return;
                const onScreen = new Set(shown);
                const saved = persistence.getLayoutOrder(currentTabId, currentLayout) || [];
                persistence.setLayoutOrder(currentTabId, currentLayout, [...shown, ...saved.filter(id => !onScreen.has(id))]);

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
                // Edit mode, the open tab or its sorting changed.
                const update = () => { if (sortableInstance) (canDrag() ? enable : disable)(); };
                ['editModeChanged', 'tabChanged', 'sortModeChanged', 'libraryChanged', 'settingsReset'].forEach(name => document.addEventListener(name, update));
                update();
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