// Arranging the tabs: in edit mode, a long press on a tab in the tab bar
// lifts it, and dragging moves it to another place. Favorites stays first.
//
// The order is stored on the PC, for every phone (POST
// /api/library/tab-order); the PC sends the tabs in that order from then on
// (/api/tabs, /api/library), so swiping, the swipe previews and the search
// follow it without knowing about it. The PC also places tabs the order
// doesn't know yet (a new subfolder goes after its parent folder) and keeps
// the place of a removed folder in case it comes back, so the phone only
// ever sends what it shows.
const tabOrder = (() => {
    const bar = document.getElementById('tabs-container');
    let sortable = null;
    let dragging = false;
    let droppedAt = 0; // the bar stays put for a moment after a drop, too
    let before = null; // the order when the drag started

    const shownIds = () => Array.from(bar.querySelectorAll('.tab:not(.tab-icon-only)')).map(t => t.dataset.tabId);

    // save sends the shown order. state.tabs follows it at once, so the
    // library event the PC sends next finds the tabs unchanged and leaves
    // the bar alone (app.js reloadLibrary).
    async function save(ids) {
        const byId = new Map((state.tabs || []).map(t => [String(t.id), t]));
        state.tabs = ids.map(id => byId.get(id)).filter(Boolean);
        try {
            await apiFetch('/api/library/tab-order', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ order: ids.map(Number) }), // IDs stay below 2^53
            });
        } catch (error) {
            console.error('Saving the tab order failed:', error);
            showToast("The new tab order couldn't be saved.");
            await loadTabs().catch(() => { }); // back to the order the PC has
        }
    }

    function init() {
        if (!bar || typeof Sortable === 'undefined') {
            console.error('Tab order: tab bar or SortableJS not found.');
            return;
        }
        sortable = new Sortable(bar, {
            draggable: '.tab:not(.tab-icon-only)', // not Favorites, not the indicator
            direction: 'horizontal',
            animation: 150,
            // A long press, so the tab bar still scrolls, tabs still open in
            // edit mode and a resting finger doesn't reorder by accident.
            delay: 600,
            delayOnTouchOnly: true,
            touchStartThreshold: 6,
            // The lifted tab follows the finger (a copy on the body, above
            // the bottom bar); its place in the bar shows as a slot.
            forceFallback: true,
            fallbackOnBody: true,
            fallbackClass: 'tab-drag-copy',
            dragClass: 'tab-drag-copy',
            ghostClass: 'tab-drag-slot',
            chosenClass: 'tab-drag-chosen',
            // Near either end, the bar scrolls to the tabs out of view,
            // slowly enough to drop a tab precisely.
            scroll: true,
            scrollSensitivity: 56,
            scrollSpeed: 6,
            bubbleScroll: false,
            disabled: !editMode.isActive(),
            // Nothing goes before Favorites.
            onMove: (evt) => !(evt.related.classList.contains('tab-icon-only') && !evt.willInsertAfter),
            onStart: () => {
                dragging = true;
                before = shownIds();
                bar.classList.add('tab-dragging');
                if (navigator.vibrate) navigator.vibrate(40);
            },
            onEnd: () => {
                dragging = false;
                droppedAt = performance.now();
                bar.classList.remove('tab-dragging');
                const ids = shownIds();
                if (before && ids.join() !== before.join()) save(ids);
                before = null;
            },
        });
        document.addEventListener('editModeChanged', (e) => sortable.option('disabled', !e.detail.isActive));
    }

    document.addEventListener('DOMContentLoaded', init);

    return {
        // A library reload waits for the drag to end before it rebuilds the
        // bar, and the bar doesn't scroll to the active tab meanwhile (tabs.js).
        isDragging: () => dragging || performance.now() - droppedAt < 500,
    };
})();

window.tabOrder = tabOrder;
