// The landing page (index.html). The installed app opens the soundboard
// (or the connect page while no PC is paired) without showing the page;
// earlier installs start here. In a browser the page stays, and its
// buttons follow what this device can do: open the soundboard when a PC is
// paired, install the app when the browser offers it. A pairing link
// (/#pair&…) is forwarded by rtc.js, loaded before this.
(() => {
    'use strict';

    const rtc = window.resoundRTC;
    const pc = rtc ? rtc.currentPC() : null;
    const installed = window.matchMedia('(display-mode: standalone)').matches ||
        (window.matchMedia('(display-mode: fullscreen)').matches && !document.fullscreenElement) || // not a fullscreened tab
        navigator.standalone === true;

    if (location.hash.startsWith('#pair')) return; // rtc.js is on its way
    if (installed) {
        location.replace(pc ? '/app' : '/connect');
        return;
    }

    // Phones come here to use the soundboard, PCs to download Resound.
    const phone = window.matchMedia('(pointer: coarse)').matches && !/Windows NT/.test(navigator.userAgent);

    let installPrompt = null;
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault(); // offered as a button instead of a banner
        installPrompt = e;
        document.querySelectorAll('[data-install]').forEach((b) => { b.hidden = false; });
    });
    window.addEventListener('appinstalled', () => {
        document.querySelectorAll('[data-install]').forEach((b) => { b.hidden = true; });
    });

    document.addEventListener('DOMContentLoaded', () => {
        const target = pc ? '/app' : '/connect';
        document.querySelectorAll('[data-open-app]').forEach((a) => { a.href = target; });
        document.querySelectorAll('[data-open-app-label]').forEach((s) => {
            s.textContent = pc ? 'Open soundboard' : 'Connect your phone';
        });
        if (pc) {
            document.querySelectorAll('[data-pc-name]').forEach((s) => { s.textContent = pc.name; });
            document.querySelectorAll('[data-paired-note]').forEach((p) => { p.hidden = false; });
        }

        // On a phone the soundboard comes first; on a PC the download.
        if (phone) {
            document.querySelectorAll('.cta-row').forEach((row) => {
                const open = row.querySelector('[data-open-app]');
                const download = row.querySelector('[data-download]');
                if (!open || !download) return;
                open.classList.replace('btn-secondary', 'btn-primary');
                download.classList.replace('btn-primary', 'btn-secondary');
                row.insertBefore(open, download);
            });
        }

        document.querySelectorAll('[data-install]').forEach((b) => {
            b.hidden = !installPrompt;
            b.addEventListener('click', async () => {
                if (!installPrompt) return;
                installPrompt.prompt();
                await installPrompt.userChoice.catch(() => {});
                installPrompt = null;
                document.querySelectorAll('[data-install]').forEach((x) => { x.hidden = true; });
            });
        });
    });
})();
