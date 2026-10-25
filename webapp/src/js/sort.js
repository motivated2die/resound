// Sorting a tab (edit mode): the button left of the layout button cycles
// the open tab through its custom order, A–Z and Z–A by name. The choice is
// per tab and saved on the PC beside the layout (persistence.setSortMode),
// so all phones show it. Sorting happens on the phone, while the tab is
// shown (app.js orderSounds): the custom order is never changed by it, and
// switching back to Custom brings it back as it was. While a tab is sorted,
// its buttons can't be dragged (dragDrop.js).
const sortManager = (() => {
    const button = document.getElementById('sort-mode-button');
    const CUSTOM = '';
    const modes = [CUSTOM, 'name-asc', 'name-desc']; // the button's cycle
    const labels = { [CUSTOM]: 'Custom order', 'name-asc': 'A–Z', 'name-desc': 'Z–A' };

    // As the PC's overlay sorts (library.CompareNames): by name, ignoring
    // case and accents, numbers by value ("Clip 2" before "Clip 10").
    const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

    const known = (mode) => (modes.includes(mode) ? mode : CUSTOM);

    // sort returns sounds (in the tab's custom order) as the mode shows them.
    // Equal names keep their custom order.
    const sort = (sounds, mode) => {
        switch (known(mode)) {
            case 'name-asc': return [...sounds].sort((a, b) => collator.compare(a.name, b.name));
            case 'name-desc': return [...sounds].sort((a, b) => collator.compare(b.name, a.name));
            default: return sounds;
        }
    };

    const isCustom = (tabId) => known(persistence.getSortMode(tabId)) === CUSTOM;

    // The button shows the open tab's sorting: the sort icon for the custom
    // order, the letters in their order otherwise (highlighted: sorted).
    const show = (tabId) => {
        if (!button) return;
        const mode = known(persistence.getSortMode(tabId));
        button.dataset.sort = mode || 'custom';
        button.classList.toggle('active', mode !== CUSTOM);
        button.setAttribute('aria-label', `Sorting: ${labels[mode]}`);
        button.title = `Sorting: ${labels[mode]}`;
    };

    const cycle = () => {
        if (state.currentTab === null || typeof state.currentTab === 'undefined') return;
        const tabId = String(state.currentTab);
        const current = known(persistence.getSortMode(tabId));
        const next = modes[(modes.indexOf(current) + 1) % modes.length];
        persistence.setSortMode(tabId, next);
        show(tabId);
        editMode.flash(labels[next]);
        document.dispatchEvent(new CustomEvent('sortModeChanged', { detail: { tabId, mode: next } }));
        window.app?.reloadSoundsForCurrentTab();
    };

    const init = () => {
        button?.addEventListener('click', cycle);
        document.addEventListener('tabChanged', (e) => show(e.detail.tabId));
        document.addEventListener('appReady', () => show(state.currentTab));
        // Another phone or a reload may have changed it.
        document.addEventListener('libraryChanged', () => show(state.currentTab));
        document.addEventListener('settingsReset', () => show(state.currentTab));
    };

    return { init, sort, isCustom };
})();

document.addEventListener('DOMContentLoaded', sortManager.init);
