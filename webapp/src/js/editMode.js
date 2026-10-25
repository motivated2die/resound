// --- NEW FILE START ---
const editMode = (() => {
    let isEditModeActive = false;
    const editButton = document.getElementById('edit-mode-button');
    const layoutButton = document.getElementById('layout-mode-button');
    const sortButton = document.getElementById('sort-mode-button');
    const searchButton = document.getElementById('search-button');
    const topBar = document.getElementById('top-bar');
    const soundsContainer = document.getElementById('sounds-container');
    // --- NEW CODE START ---
    const editModeInfo = document.getElementById('edit-mode-info');
    // --- NEW CODE END ---
    const editModeLabel = editModeInfo?.querySelector('span');
    const EDIT_MODE_TEXT = 'EDIT MODE';
    let flashTimer = null;

    // flash shows what a top bar button just switched to ("3 COLUMNS",
    // "A–Z") in place of EDIT MODE, for two seconds.
    const flash = (text) => {
        if (!editModeLabel || !isEditModeActive) return;
        clearTimeout(flashTimer);
        editModeLabel.textContent = String(text).toUpperCase();
        editModeInfo.classList.add('flashing');
        flashTimer = setTimeout(endFlash, 2000);
    };
    const endFlash = () => {
        clearTimeout(flashTimer);
        flashTimer = null;
        if (editModeLabel) editModeLabel.textContent = EDIT_MODE_TEXT;
        editModeInfo?.classList.remove('flashing');
    };

    const toggleEditMode = () => {
        isEditModeActive = !isEditModeActive;
        console.log("Edit mode toggled:", isEditModeActive);
        updateUI();
        // Notify other modules (e.g., dragDrop, sound-settings)
        document.dispatchEvent(new CustomEvent('editModeChanged', { detail: { isActive: isEditModeActive } }));
    };

    const updateUI = () => {
        document.body.classList.toggle('edit-mode', isEditModeActive);
        topBar?.classList.toggle('edit-mode-active', isEditModeActive);
        layoutButton?.classList.toggle('hidden', !isEditModeActive);
        sortButton?.classList.toggle('hidden', !isEditModeActive);
        // Search and edit mode exclude each other (opening search leaves
        // edit mode), so search has no button while editing.
        searchButton?.classList.toggle('hidden', isEditModeActive);
        if (!isEditModeActive) endFlash();
        editButton?.classList.toggle('active', isEditModeActive);

        // --- NEW CODE START ---
        editModeInfo?.classList.toggle('hidden', !isEditModeActive); // Show/hide info text
        // --- NEW CODE END ---

        // Change icon based on state
        const editIcon = editButton?.querySelector('.icon');
        if (editIcon) {
            editIcon.dataset.icon = isEditModeActive ? 'done' : 'edit';
        }

        // Update cursor for sound cards only if the container exists
        if (soundsContainer) {
             soundsContainer.style.cursor = isEditModeActive ? 'grab' : 'pointer';
        }
    };

    const isActive = () => isEditModeActive;

    const init = () => {
        if (editButton) {
            editButton.addEventListener('click', toggleEditMode);
        } else {
            console.error("Edit mode button not found");
        }
        // Ensure initial UI state is correct (edit mode off)
        updateUI();
    };

    return {
        init,
        isActive,
        flash,
        // toggleEditMode // Expose if needed externally, but button click is primary way
    };
})();

document.addEventListener('DOMContentLoaded', editMode.init);