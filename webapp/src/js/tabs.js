// Swiping between tabs.
//
// The live page (#sounds-container) sits on a track between two preview
// pages: copies of the previous and next tab, built from the cached sound
// lists (app.js tabSoundsCache). Dragging moves the whole track with the
// finger, so the neighbour slides in at once. On release the track finishes
// the slide, then the live page switches to that tab (rendered from the
// cache, so instantly) and the track snaps back in the same frame. Swipes
// count when they pass a quarter of the width or are a quick flick.

let isTabSwiping = false; // true during a horizontal swipe gesture
const isCurrentlyTabSwiping = () => isTabSwiping;

const SWIPE_LOCK_PX = 10;        // movement before the direction is decided
const SWIPE_COMMIT_RATIO = 0.25; // of the width
const SWIPE_FLICK_SPEED = 0.35;  // px/ms
const SWIPE_FLICK_MIN_PX = 30;

let refreshNeighbors = () => {};

function initTabSwiping() {
    const wrapper = document.getElementById('tab-content-wrapper');
    const live = document.getElementById('sounds-container');
    const tabsContainer = document.getElementById('tabs-container');
    if (!wrapper || !live || !tabsContainer) {
        console.error('Tab swiping: page elements not found.');
        return;
    }

    // Track: [previous] [live] [next]
    const track = document.createElement('div');
    track.id = 'tab-track';
    const prevPage = document.createElement('div');
    const nextPage = document.createElement('div');
    prevPage.id = 'page-prev';
    nextPage.id = 'page-next';
    // Previews are only looked at while swiping: never focused (a preview
    // with more cards than fit is a scroller, which Chromium lets Tab focus;
    // the browser then scrolled the wrapper to show it, and the live page
    // stayed out of view), searched with find-in-page or read out.
    prevPage.inert = true;
    nextPage.inert = true;
    wrapper.insertBefore(track, live);
    track.append(prevPage, live, nextPage);
    // Only the track moves the pages. Should anything scroll the wrapper
    // itself (where overflow: clip is missing), undo it at once.
    wrapper.addEventListener('scroll', () => { wrapper.scrollLeft = 0; wrapper.scrollTop = 0; });

    const tabList = () => Array.from(tabsContainer.querySelectorAll('.tab'));
    const activeIndex = (tabs) => Math.max(0, tabs.findIndex(t => t.classList.contains('active')));
    const neighbour = (tabs, dir) => tabs.length > 1 ? tabs[(activeIndex(tabs) + dir + tabs.length) % tabs.length] : null;

    // renderPreview fills a preview page with a tab's cards (no listeners;
    // previews are only looked at while dragging).
    function renderPreview(page, tab) {
        page.innerHTML = '';
        if (!tab || !window.app?.orderSounds) return;
        const tabId = tab.dataset.tabId;
        const mode = persistence.getCurrentLayoutMode(tabId) || 'grid-3';
        page.className = `sounds-grid sound-page-preview layout-${mode}`;
        page.dataset.tabId = tabId;
        const raw = app.tabSoundsCache.get(tabId);
        if (!raw) {
            page.innerHTML = '<p class="loading-sounds"></p>';
            return;
        }
        const sounds = app.orderSounds(tabId, raw);
        if (sounds.length === 0) {
            page.innerHTML = `<p class="no-sounds">${tabId === 'favorites' ? 'So empty...' : 'No sounds found in this tab.'}</p>`;
            return;
        }
        const fragment = document.createDocumentFragment();
        sounds.forEach(sound => {
            if (sound && sound.id !== undefined && sound.name) {
                fragment.appendChild(app.createSoundCardElement(sound, { animate: false, tabId }));
            }
        });
        page.appendChild(fragment);
    }

    // refreshNeighbors rebuilds both previews: at once from the cache, and
    // again once a missing list has been fetched.
    refreshNeighbors = () => {
        const tabs = tabList();
        const pairs = [[prevPage, neighbour(tabs, -1)], [nextPage, neighbour(tabs, 1)]];
        pairs.forEach(([page, tab]) => {
            renderPreview(page, tab);
            const tabId = tab?.dataset.tabId;
            if (tabId && !app.tabSoundsCache.has(tabId)) {
                app.fetchTabSounds(tabId)
                    .then(() => { if (page.dataset.tabId === tabId) renderPreview(page, tab); })
                    .catch(() => { /* offline: the preview stays a placeholder */ });
            }
        });
    };

    // --- track movement ---
    let frame = 0;
    let pendingX = 0;
    const moveTrack = (x) => {
        pendingX = x;
        if (!frame) frame = requestAnimationFrame(() => {
            frame = 0;
            track.style.transform = `translate3d(${pendingX}px, 0, 0)`;
            if (drag) followSwipe(drag);
        });
    };

    let settle = null; // { timer, done } while the track animates after a release
    function animateTrack(x, onDone) {
        cancelAnimationFrame(frame);
        frame = 0;
        const tf = getComputedStyle(track).transform;
        const from = tf && tf !== 'none' ? new DOMMatrixReadOnly(tf).m41 : 0;
        const ms = Math.round(Math.min(260, Math.max(120, Math.abs(x - from) * 0.6)));
        track.style.transition = `transform ${ms}ms cubic-bezier(0.2, 0.8, 0.2, 1)`;
        track.style.transform = `translate3d(${x}px, 0, 0)`;
        const done = () => {
            if (!settle || settle.done !== done) return;
            clearTimeout(settle.timer);
            settle = null;
            track.removeEventListener('transitionend', done);
            onDone();
        };
        settle = { done, timer: setTimeout(done, ms + 60) }; // transitionend may not fire
        track.addEventListener('transitionend', done);
    }

    function resetTrack() {
        track.style.transition = 'none';
        track.style.transform = '';
    }

    // switchTo makes the neighbour the live tab; cached tabs render
    // synchronously, so the snap back below shows the same picture.
    function switchTo(tab) {
        tab.click(); // loadSounds also rebuilds the previews for the new neighbours
        markSwipeTarget(null);
        live.scrollTop = 0;
        resetTrack();
    }

    // --- gesture ---
    let drag = null;

    wrapper.addEventListener('touchstart', (e) => {
        if (typeof editMode !== 'undefined' && editMode.isActive()) return;
        if (window.state?.searching) return; // search results: no tabs to swipe to
        if (e.touches.length > 1) { drag = null; return; }
        if (settle) settle.done(); // finish the previous swipe now: quick successive swipes
        const t = e.touches[0];
        drag = { x0: t.clientX, y0: t.clientY, dx: 0, dir: null, lastX: t.clientX, lastT: e.timeStamp, v: 0 };
    }, { passive: true });

    wrapper.addEventListener('touchmove', (e) => {
        if (!drag || e.touches.length > 1) return;
        const t = e.touches[0];
        const dx = t.clientX - drag.x0;
        const dy = t.clientY - drag.y0;
        if (!drag.dir) {
            if (Math.abs(dx) < SWIPE_LOCK_PX && Math.abs(dy) < SWIPE_LOCK_PX) return;
            drag.dir = Math.abs(dx) > Math.abs(dy) * 1.2 ? 'h' : 'v';
            if (drag.dir === 'h') {
                isTabSwiping = true;
                wrapper.classList.add('swipe-active');
                track.style.transition = 'none';
                indicator.style.transition = 'none'; // it follows the finger
                drag.x0 = t.clientX; // start moving from here, without a jump
                drag.scroll0 = tabsContainer.scrollLeft;
            }
        }
        if (drag.dir !== 'h') return;
        e.preventDefault(); // keep the page from scrolling sideways or vertically
        const dt = e.timeStamp - drag.lastT;
        if (dt > 0) drag.v = 0.8 * ((t.clientX - drag.lastX) / dt) + 0.2 * drag.v;
        drag.lastX = t.clientX;
        drag.lastT = e.timeStamp;
        drag.dx = t.clientX - drag.x0;
        const single = tabList().length < 2;
        moveTrack(single ? drag.dx * 0.25 : drag.dx); // resist when there is nowhere to go
    }, { passive: false });

    const endDrag = (cancelled) => {
        const d = drag;
        drag = null;
        wrapper.classList.remove('swipe-active');
        setTimeout(() => { isTabSwiping = false; }, 0); // after the click that may follow
        if (!d || d.dir !== 'h') return;

        const width = wrapper.clientWidth || window.innerWidth;
        const tabs = tabList();
        const flick = Math.abs(d.v) > SWIPE_FLICK_SPEED && Math.abs(d.dx) > SWIPE_FLICK_MIN_PX && Math.sign(d.v) === Math.sign(d.dx);
        const commit = !cancelled && tabs.length > 1 && (Math.abs(d.dx) > width * SWIPE_COMMIT_RATIO || flick);
        if (!commit) {
            animateTrack(0, resetTrack);
            markSwipeTarget(null);
            releaseIndicator(tabs[activeIndex(tabs)]);
            return;
        }
        const dir = d.dx < 0 ? 1 : -1; // swipe left: next tab
        const target = neighbour(tabs, dir);
        animateTrack(dir > 0 ? -width : width, () => switchTo(target));
        markSwipeTarget(target); // a flick may not have marked it yet
        releaseIndicator(target);
    };
    wrapper.addEventListener('touchend', () => endDrag(false));
    wrapper.addEventListener('touchcancel', () => endDrag(true));

    // --- keep the active tab visible in the (scrollable) tab bar ---
    const scrollActiveTabIntoView = () => {
        if (drag && drag.dir === 'h') return; // the swipe moves the tab bar itself
        // A tab lifted or moved in edit mode (tab-order.js) changes classes
        // and the tab list; the bar must stay under the finger.
        if (window.tabOrder?.isDragging() || tabsContainer.querySelector('.tab-drag-chosen, .tab-drag-slot')) return;
        tabsContainer.querySelector('.tab.active')?.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' });
    };
    new MutationObserver(() => setTimeout(scrollActiveTabIntoView, 50))
        .observe(tabsContainer, { subtree: true, attributes: true, attributeFilter: ['class'], childList: true });

    // --- the active tab's indicator (.tab-indicator in styles.css) ---
    // It lives in the tab bar, so it scrolls with the tabs, and slides from
    // tab to tab. A rebuilt tab bar (app.js loadTabs) drops it; it comes back
    // in place, without sliding in from the left.
    const indicator = document.createElement('div');
    indicator.className = 'tab-indicator';
    indicator.setAttribute('aria-hidden', 'true');
    const setIndicator = (x, w) => {
        indicator.style.width = w + 'px';
        indicator.style.transform = `translateX(${x}px)`;
    };
    const placeIndicator = (slide) => {
        if (drag && drag.dir === 'h') return; // followSwipe places it
        const active = tabsContainer.querySelector('.tab.active');
        if (!indicator.isConnected) {
            tabsContainer.append(indicator);
            slide = false;
        }
        indicator.style.opacity = active ? '1' : '0';
        if (!active) return;
        if (!slide) indicator.style.transition = 'none';
        setIndicator(active.offsetLeft, active.offsetWidth);
        if (!slide) {
            void indicator.offsetWidth; // apply before the transition returns
            indicator.style.transition = '';
        }
    };

    // --- the tab bar during a swipe ---
    // The indicator moves from the active tab towards the one the swipe
    // heads for, as far as the page has moved, and the tab bar scrolls
    // along. Past the point where letting go switches tabs, the target takes
    // the accent colour. On release the indicator finishes its slide at
    // once, alongside the page, instead of after it.
    const between = (a, b, p) => a + (b - a) * p;
    const centred = (tab) => Math.max(0, Math.min(tabsContainer.scrollWidth - tabsContainer.clientWidth,
        tab.offsetLeft + tab.offsetWidth / 2 - tabsContainer.clientWidth / 2));

    function markSwipeTarget(target) {
        tabsContainer.toggleAttribute('data-swipe-commit', !!target);
        tabList().forEach(t => t.toggleAttribute('data-swipe-target', t === target));
    }

    function followSwipe(d) {
        const tabs = tabList();
        const from = tabs[activeIndex(tabs)];
        const to = neighbour(tabs, d.dx < 0 ? 1 : -1);
        if (!from || !to || !indicator.isConnected) return;
        const width = wrapper.clientWidth || window.innerWidth;
        const p = Math.min(1, Math.abs(d.dx) / width);
        setIndicator(between(from.offsetLeft, to.offsetLeft, p), between(from.offsetWidth, to.offsetWidth, p));
        tabsContainer.scrollLeft = between(d.scroll0, centred(to), p);
        markSwipeTarget(Math.abs(d.dx) > width * SWIPE_COMMIT_RATIO ? to : null);
    }

    function releaseIndicator(tab) {
        indicator.style.transition = '';
        if (!tab) return;
        setIndicator(tab.offsetLeft, tab.offsetWidth);
        tabsContainer.scrollTo({ left: centred(tab), behavior: 'smooth' });
    }
    new MutationObserver(() => placeIndicator(true))
        .observe(tabsContainer, { subtree: true, attributes: true, attributeFilter: ['class'], childList: true });
    // Tab widths change with the font and the screen.
    window.addEventListener('resize', () => placeIndicator(false));
    document.fonts?.ready.then(() => placeIndicator(false));
    placeIndicator(false);

    console.log('Tab swiping initialized.');
}

document.addEventListener('DOMContentLoaded', initTabSwiping);

window.tabManager = {
    isCurrentlySwiping: isCurrentlyTabSwiping,
    refreshNeighbors: () => refreshNeighbors(),
};
