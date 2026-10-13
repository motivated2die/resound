// The connect page: pairs with a PC from its QR code (a link that opened
// this page, a code scanned here, or a pasted link) and lists paired PCs.
(() => {
    'use strict';

    const rtc = window.resoundRTC;
    const $ = (id) => document.getElementById(id);
    const show = (id, on) => { $(id).hidden = !on; };

    const setMessage = (text, error) => {
        const el = $('message');
        el.textContent = text || '';
        el.classList.toggle('error', !!error);
    };

    const standalone = () =>
        window.matchMedia('(display-mode: standalone)').matches ||
        window.matchMedia('(display-mode: fullscreen)').matches ||
        navigator.standalone === true;
    const isIOS = () => /iPhone|iPad|iPod/.test(navigator.userAgent) ||
        (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

    // --- Paired PCs ----------------------------------------------------------

    const renderPCs = () => {
        const store = rtc.loadStore();
        const entries = Object.values(store.pcs);
        const list = $('pc-list');
        list.textContent = '';
        for (const pc of entries) {
            const li = document.createElement('li');
            const open = document.createElement('button');
            open.type = 'button';
            open.className = 'open' + (pc.fp === store.current ? '' : ' other');
            open.textContent = pc.name;
            open.addEventListener('click', () => {
                rtc.savePC(pc); // makes it the current PC
                location.href = '/app';
            });
            const forget = document.createElement('button');
            forget.type = 'button';
            forget.className = 'forget';
            forget.title = 'Forget this PC';
            forget.setAttribute('aria-label', 'Forget ' + pc.name);
            forget.textContent = '✕';
            forget.addEventListener('click', () => {
                if (!confirm(`Forget “${pc.name}”? You can pair again with its QR code.`)) return;
                rtc.forgetPC(pc.fp);
                renderPCs();
            });
            li.append(open, forget);
            list.append(li);
        }
        show('pcs', entries.length > 0);
        show('intro', entries.length === 0);
    };

    // --- Pairing -------------------------------------------------------------

    let busy = false;

    const pairWith = async (link) => {
        const info = rtc.parsePairLink(link);
        if (!info) {
            setMessage('That is not a Resound pairing code.', true);
            return;
        }
        if (busy) return;
        busy = true;
        $('pairing-name').textContent = info.name;
        show('pairing', true);
        show('pcs', false);
        show('intro', false);
        $('scan-button').disabled = true;
        setMessage('');
        try {
            await rtc.pair(info);
            setMessage('Paired.');
            location.replace('/app');
        } catch (e) {
            console.error('Pairing failed:', e);
            if (e.denied) {
                setMessage('This code was already used or has expired. Show a new one on the PC and scan it again.', true);
            } else {
                setMessage(`Can't reach ${info.name}. Is this device on the same Wi-Fi as the PC, and is Resound running?`, true);
            }
            show('pairing', false);
            renderPCs();
        } finally {
            busy = false;
            $('scan-button').disabled = false;
        }
    };

    // --- Scanner -------------------------------------------------------------

    let stream = null;
    let scanning = false;

    const loadScript = (src) => new Promise((resolve, reject) => {
        const s = document.createElement('script');
        s.src = src;
        s.onload = resolve;
        s.onerror = () => reject(new Error('Could not load ' + src));
        document.head.append(s);
    });

    // decoder returns a function that reads a QR code from the video, or
    // null: BarcodeDetector where available, jsQR otherwise.
    const decoder = async (video) => {
        if ('BarcodeDetector' in window) {
            try {
                const formats = await window.BarcodeDetector.getSupportedFormats();
                if (formats.includes('qr_code')) {
                    const detector = new window.BarcodeDetector({ formats: ['qr_code'] });
                    return async () => {
                        const codes = await detector.detect(video);
                        return codes.length ? codes[0].rawValue : null;
                    };
                }
            } catch (e) { /* fall back to jsQR */ }
        }
        if (!window.jsQR) await loadScript('vendor/jsqr/jsQR.js');
        const canvas = document.createElement('canvas');
        const ctx = canvas.getContext('2d', { willReadFrequently: true });
        return async () => {
            const w = video.videoWidth, h = video.videoHeight;
            if (!w || !h) return null;
            const scale = Math.min(1, 720 / Math.max(w, h)); // fast enough on phones
            canvas.width = Math.round(w * scale);
            canvas.height = Math.round(h * scale);
            ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
            const img = ctx.getImageData(0, 0, canvas.width, canvas.height);
            const code = window.jsQR(img.data, img.width, img.height, { inversionAttempts: 'dontInvert' });
            return code ? code.data : null;
        };
    };

    const stopScanner = () => {
        scanning = false;
        if (stream) stream.getTracks().forEach(t => t.stop());
        stream = null;
        show('scanner', false);
    };

    const startScanner = async () => {
        if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
            setMessage('This browser has no camera access. Use your camera app, or paste the link.', true);
            return;
        }
        setMessage('');
        const video = $('scanner-video');
        try {
            stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' }, audio: false });
        } catch (e) {
            setMessage('No camera access. Allow the camera for this app, or use your camera app.', true);
            return;
        }
        video.srcObject = stream;
        show('scanner', true);
        await video.play().catch(() => {});
        let read;
        try {
            read = await decoder(video);
        } catch (e) {
            stopScanner();
            setMessage('The QR scanner could not start.', true);
            return;
        }
        scanning = true;
        const tick = async () => {
            if (!scanning) return;
            let text = null;
            try { text = await read(); } catch (e) { /* next frame */ }
            if (text && rtc.parsePairLink(text)) {
                stopScanner();
                pairWith(text);
                return;
            }
            setTimeout(tick, 150);
        };
        tick();
    };

    // --- Install -------------------------------------------------------------

    let installPrompt = null;
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault();
        installPrompt = e;
        show('install-button', true);
    });
    window.addEventListener('appinstalled', () => show('install-button', false));

    // --- Start ---------------------------------------------------------------

    document.addEventListener('DOMContentLoaded', () => {
        $('scan-button').addEventListener('click', startScanner);
        $('scanner-close').addEventListener('click', stopScanner);
        $('install-button').addEventListener('click', async () => {
            if (!installPrompt) return;
            installPrompt.prompt();
            await installPrompt.userChoice.catch(() => {});
            installPrompt = null;
            show('install-button', false);
        });
        $('paste-form').addEventListener('submit', (e) => {
            e.preventDefault();
            pairWith($('paste-input').value.trim());
        });
        show('ios-hint', isIOS() && !standalone());

        const hash = location.hash;
        // The pairing code works once; keep it out of the history.
        if (hash) history.replaceState(null, '', location.pathname);
        renderPCs();
        if (hash.startsWith('#pair')) {
            pairWith(hash);
        } else if (hash === '#removed') {
            setMessage('This device was signed out on the PC. Scan its QR code to pair again.', true);
        }
    });
})();
