// What to check when this device can't reach the PC, on the remote and the
// connect page. From here every cause looks the same: the PC doesn't answer
// a device that isn't on its network, is blocked by its firewall, or holds an
// outdated pairing or code (internal/server/rtc.go). So the list goes from
// the most likely cause to the least, in the words Resound's panel uses.
(() => {
    'use strict';

    const STEPS = {
        running: ['Resound is running on the PC',
            'The PC is awake, and Resound’s Remote App page shows a QR code.'],
        wifi: ['Same Wi-Fi as the PC',
            'Not mobile data, a guest network or a VPN.'],
        rescan: ['Scan the QR code again',
            'In Resound, open Remote App and scan its code. This picks up a changed address.'],
        fresh: ['Use a new code',
            'Each code works once, for 10 minutes. Remote App in Resound shows a new one.'],
        firewall: ['Let it through the firewall',
            'In Remote App, click “Allow phones through the firewall”. In Windows’ network settings, the network must be Private.'],
    };

    // note is what this device knows about its own network, if anything.
    const note = () => {
        if (navigator.onLine === false) return 'This device is offline.';
        if (navigator.connection && navigator.connection.type === 'cellular') return 'This device is on mobile data.';
        return '';
    };

    // render fills el with the checklist. pairing: for a new pairing code
    // instead of a paired PC.
    const render = (el, pairing) => {
        el.textContent = '';
        const hint = note();
        if (hint) {
            const p = document.createElement('p');
            p.className = 'help-note';
            p.textContent = hint;
            el.append(p);
        }
        const list = document.createElement('ol');
        list.className = 'help-steps';
        for (const key of ['running', 'wifi', pairing ? 'fresh' : 'rescan', 'firewall']) {
            const [title, text] = STEPS[key];
            const li = document.createElement('li');
            if (key === 'wifi' && hint) li.className = 'flagged';
            const strong = document.createElement('strong');
            strong.textContent = title;
            const span = document.createElement('span');
            span.textContent = text;
            li.append(strong, span);
            list.append(li);
        }
        el.append(list);
    };

    window.resoundHelp = { render };
})();
