// The launchpad (the PC's app/launchpad.go). While its key is held on the
// PC, the remote in use labels the buttons of its tab with the keys that
// play them. The PC sends the labels by sound ID, so a label always names the
// key that plays its button, whatever this screen's size, layout or sorting.
// Meanwhile the talk button turns into a yellow launchpad button, the bottom
// bar and the tab highlight blend to yellow, and tab switches made on the PC
// (arrow keys, wheel) show here.
//
// The PC needs to know which remote is in use and what it shows. Each
// remote reports its tab (POST /api/remote/view) when it connects, when its
// tab or the tab's order changes, and, at most every few seconds, when it is
// used; the one used last is the one in use. Each launchpad message carries
// a number, and reports carry the newest one applied here: a report that
// crossed a tab switch from the PC can't take the PC back to the old tab.
const launchpad = (() => {
    const USE_EVERY_MS = 2000; // reports of use alone, at most this often
    const INPUT_MS = 1500;     // a tab change this soon after input is the user's
    const YELLOW_RIPPLE = 'rgba(255, 201, 60, 0.35)';

    let supported = true; // an older PC answers 404
    let ready = false;    // the app has loaded its tabs
    let seq = 0;          // the newest launchpad message applied
    let active = false;
    let keys = new Map(); // sound ID (string) -> key label
    let lastInput = 0;
    let lastUseReport = 0;
    let following = null; // a tab being opened for the PC

    const container = document.getElementById('sounds-container');
    const talkButton = document.getElementById('talk-through-button');
    const header = document.querySelector('header');

    const report = (used) => {
        if (!supported || !ready || state.currentTab === null || state.currentTab === undefined) return;
        if (used) lastUseReport = Date.now();
        apiFetch('/api/remote/view', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ tab: String(state.currentTab), seq, used }),
        }).catch((error) => {
            if (error.status === 404) supported = false;
        });
    };

    const input = () => {
        lastInput = Date.now();
        if (lastInput - lastUseReport >= USE_EVERY_MS) report(true);
    };

    // decorate puts the keys on the cards shown, or takes them off.
    const decorate = () => {
        container?.querySelectorAll('.sound-card[data-sound-id]').forEach((card) => {
            let label = card.querySelector(':scope > .launchpad-key');
            const key = active ? keys.get(card.dataset.soundId) : undefined;
            if (!key) {
                label?.remove();
                return;
            }
            if (!label) {
                label = document.createElement('span');
                label.className = 'launchpad-key';
                label.setAttribute('aria-hidden', 'true');
                card.appendChild(label);
            }
            label.textContent = key;
        });
    };

    const setActive = (on) => {
        if (on === active) return;
        active = on;
        document.body.classList.toggle('launchpad-active', on);
        header?.classList.toggle('launchpad-bg', on);
        const icon = talkButton?.querySelector('.icon');
        if (icon) icon.dataset.icon = on ? 'apps' : 'mic';
        if (talkButton) talkButton.title = on ? 'Launchpad on: press the keys shown on the buttons' : '';
        if (on && talkButton) {
            const r = talkButton.getBoundingClientRect();
            window.app?.ripple({ clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 }, talkButton, YELLOW_RIPPLE);
        }
        decorate();
    };

    const onLaunchpad = (msg) => {
        seq = Math.max(seq, Number(msg.seq) || 0);
        if (!msg.on) {
            keys = new Map();
            setActive(false);
            return;
        }
        keys = new Map(Object.entries(msg.keys || {}));
        if (window.soundSearch?.isOpen()) window.soundSearch.close(); // the tab's buttons carry the keys
        const tab = String(msg.tab);
        if (tab !== String(state.currentTab) && window.app?.showTab(tab)) following = tab;
        setActive(true);
        decorate();
    };

    const init = () => {
        // Any input counts as use. Capture: the page's own handlers may stop it.
        ['pointerdown', 'keydown', 'wheel'].forEach((type) =>
            addEventListener(type, input, { capture: true, passive: true }));
        document.addEventListener('visibilitychange', () => { if (!document.hidden) report(true); });
        document.addEventListener('appReady', () => { ready = true; report(false); });
        document.addEventListener('tabChanged', (e) => {
            const theirs = following !== null && String(e.detail.tabId) === following;
            following = null;
            report(!theirs && Date.now() - lastInput < INPUT_MS);
        });
        // A new order or sorting changes the keys: reported once the PC has it.
        document.addEventListener('layoutSaved', (e) => {
            if (String(e.detail.tabKey) === String(state.currentTab)) report(true);
        });
        // Cards are built anew on every tab load and library reload.
        if (container) new MutationObserver(() => { if (active) decorate(); }).observe(container, { childList: true });
        window.resoundRTC?.onEvent((msg) => { if (msg.e === 'launchpad') onLaunchpad(msg); });
        window.resoundRTC?.onStatus((status) => {
            if (status === 'up') report(false); // a new connection: the PC knows it by its report
            else if (status === 'down') { keys = new Map(); setActive(false); }
        });
    };

    return { init, isActive: () => active };
})();

document.addEventListener('DOMContentLoaded', launchpad.init);
