// The app's connection to the PC: a WebRTC data channel without a signaling
// server (see internal/server/rtc.go for the PC's side and the protocol).
//
// Loaded first on every page of the hosted app. On the remote (/app) it
// replaces fetch() for /api/ paths, so the remote's code speaks the REST API
// it was written for.
(() => {
    'use strict';

    const STORE_KEY = 'resound.pcs';
    const PROTOCOL = 1;
    const CHUNK = 16 * 1024; // characters per frame; well under every browser's limit
    const CONNECT_TIMEOUT = 8000;
    const REQUEST_TIMEOUT = 10000;

    // --- Paired PCs ----------------------------------------------------------
    // { current: <fingerprint>, pcs: { <fingerprint>: { name, addrs, port, fp, device, key } } }

    const loadStore = () => {
        try {
            const s = JSON.parse(localStorage.getItem(STORE_KEY));
            if (s && typeof s.pcs === 'object') return s;
        } catch (e) { /* ignore */ }
        return { current: null, pcs: {} };
    };
    const saveStore = (s) => {
        try { localStorage.setItem(STORE_KEY, JSON.stringify(s)); } catch (e) { /* ignore */ }
    };
    const currentPC = () => {
        const s = loadStore();
        return (s.current && s.pcs[s.current]) || null;
    };
    const savePC = (pc) => {
        const s = loadStore();
        s.pcs[pc.fp] = pc;
        s.current = pc.fp;
        saveStore(s);
    };
    const forgetPC = (fp) => {
        const s = loadStore();
        delete s.pcs[fp];
        if (s.current === fp) s.current = Object.keys(s.pcs)[0] || null;
        saveStore(s);
    };

    // parsePairLink reads a pairing link (the PC's QR code):
    // <app>/#pair&v=1&a=<ip>,<ip>&p=<port>&f=<fingerprint>&c=<code>&n=<name>
    const parsePairLink = (link) => {
        const i = link.indexOf('#pair');
        if (i < 0) return null;
        const q = new URLSearchParams(link.slice(i + 5).replace(/^&/, ''));
        const info = {
            v: Number(q.get('v')),
            addrs: (q.get('a') || '').split(',').filter(a => /^[0-9a-fA-F.:]+$/.test(a)),
            port: Number(q.get('p')),
            fp: q.get('f') || '',
            code: q.get('c') || '',
            name: q.get('n') || 'PC',
        };
        if (!info.addrs.length || !(info.port > 0 && info.port < 65536) || !info.fp || !info.code) return null;
        return info;
    };

    // --- Helpers -------------------------------------------------------------

    const ALNUM = 'abcdefghijklmnopqrstuvwxyz0123456789';
    const randomText = (n) => {
        const b = crypto.getRandomValues(new Uint8Array(n));
        return Array.from(b, x => ALNUM[x % ALNUM.length]).join('');
    };
    const toHex = (bytes) => Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');

    // pairPassword matches the PC's pairPassword (auth.go).
    const pairPassword = async (code) => {
        const sum = await crypto.subtle.digest('SHA-256', new TextEncoder().encode('resound-pair:' + code));
        return toHex(new Uint8Array(sum).slice(0, 16));
    };

    // fingerprintSDP turns the QR code's base64url fingerprint into SDP form.
    const fingerprintSDP = (fp) => {
        const b64 = fp.replace(/-/g, '+').replace(/_/g, '/');
        const raw = atob(b64 + '='.repeat((4 - b64.length % 4) % 4));
        return Array.from(raw, c => c.charCodeAt(0).toString(16).padStart(2, '0').toUpperCase()).join(':');
    };

    // answerSDP is the PC's answer, written here: the PC is an ICE-lite agent
    // at the QR code's addresses, with the ICE credentials we chose.
    const answerSDP = (offer, pc, ufrag, pwd) => {
        const mid = (/a=mid:(\S+)/.exec(offer) || [])[1] || '0';
        const ip4 = pc.addrs.find(a => a.includes('.')) || '0.0.0.0';
        const lines = [
            'v=0',
            'o=- ' + Date.now() + ' 0 IN IP4 0.0.0.0',
            's=-',
            't=0 0',
            'a=ice-lite',
            'a=group:BUNDLE ' + mid,
            'm=application ' + pc.port + ' UDP/DTLS/SCTP webrtc-datachannel',
            'c=IN IP4 ' + ip4,
            'a=mid:' + mid,
            'a=ice-ufrag:' + ufrag,
            'a=ice-pwd:' + pwd,
            'a=fingerprint:sha-256 ' + fingerprintSDP(pc.fp),
            'a=setup:passive',
            'a=sctp-port:5000',
            'a=max-message-size:262144',
        ];
        pc.addrs.forEach((a, i) => {
            lines.push(`a=candidate:${i + 1} 1 udp ${2130706431 - i} ${a} ${pc.port} typ host`);
        });
        lines.push('a=end-of-candidates', '');
        return lines.join('\r\n');
    };

    // --- Connection ----------------------------------------------------------

    // A Connection is one data channel to the PC. hello is the first message
    // (hello or pair); the PC answers with welcome or denied.
    class Connection {
        constructor() {
            this.pending = new Map();
            this.nextId = 1;
            this.inbuf = '';
            this.closed = false;
            this.onclose = null;
        }

        async open(target, ufrag, pwd, hello) {
            const pc = new RTCPeerConnection({ iceServers: [] });
            this.pc = pc;
            const dc = pc.createDataChannel('resound', { negotiated: true, id: 0 });
            this.dc = dc;
            const opened = new Promise((resolve, reject) => {
                dc.onopen = resolve;
                pc.onconnectionstatechange = () => {
                    if (pc.connectionState === 'failed' || pc.connectionState === 'closed') reject(new Error('connection ' + pc.connectionState));
                };
            });
            const offer = await pc.createOffer();
            await pc.setLocalDescription(offer);
            await pc.setRemoteDescription({ type: 'answer', sdp: answerSDP(offer.sdp, target, ufrag, pwd) });
            await withTimeout(opened, CONNECT_TIMEOUT, "Can't reach the PC");

            dc.onmessage = (e) => this.receive(e.data);
            dc.onclose = () => this.close();
            pc.onconnectionstatechange = () => {
                if (['failed', 'closed', 'disconnected'].includes(pc.connectionState)) this.close();
            };
            const welcome = new Promise((resolve) => { this.onwelcome = resolve; });
            this.send(hello);
            const reply = await withTimeout(welcome, CONNECT_TIMEOUT, 'The PC did not answer');
            if (reply.t !== 'welcome') {
                this.close();
                const err = new Error('The PC refused this device');
                err.denied = true;
                throw err;
            }
            return reply;
        }

        send(msg) {
            const text = JSON.stringify(msg);
            let i = 0;
            do {
                let end = Math.min(i + CHUNK, text.length);
                // Don't split a surrogate pair across frames.
                if (end < text.length && /[\uD800-\uDBFF]/.test(text[end - 1])) end--;
                this.dc.send((end < text.length ? '+' : '.') + text.slice(i, end));
                i = end;
            } while (i < text.length);
        }

        receive(frame) {
            if (typeof frame !== 'string' || !frame) return;
            this.inbuf += frame.slice(1);
            if (frame[0] === '+') return;
            let msg;
            try { msg = JSON.parse(this.inbuf); } catch (e) { msg = null; }
            this.inbuf = '';
            if (!msg) return;
            if (msg.t === 'welcome' || msg.t === 'denied') {
                if (this.onwelcome) { this.onwelcome(msg); this.onwelcome = null; return; }
                if (msg.t === 'denied') this.close(true);
                return;
            }
            if (msg.t === 'res') {
                const p = this.pending.get(msg.id);
                if (p) { this.pending.delete(msg.id); p.resolve(msg); }
            }
        }

        request(method, path, headers, body) {
            return new Promise((resolve, reject) => {
                if (this.closed) { reject(new TypeError('Not connected to the PC')); return; }
                const id = this.nextId++;
                const timer = setTimeout(() => {
                    this.pending.delete(id);
                    reject(new TypeError('The PC did not answer in time'));
                }, REQUEST_TIMEOUT);
                this.pending.set(id, {
                    resolve: (m) => { clearTimeout(timer); resolve(m); },
                    reject: (e) => { clearTimeout(timer); reject(e); },
                });
                const msg = { t: 'req', id, m: method, p: path };
                if (headers && Object.keys(headers).length) msg.h = headers;
                if (body !== undefined && body !== null) msg.b = body;
                try { this.send(msg); } catch (e) { this.pending.delete(id); clearTimeout(timer); reject(new TypeError(e.message)); }
            });
        }

        close(denied) {
            if (this.closed) return;
            this.closed = true;
            for (const p of this.pending.values()) p.reject(new TypeError('Connection to the PC lost'));
            this.pending.clear();
            try { this.pc.close(); } catch (e) { /* ignore */ }
            if (this.onclose) this.onclose(!!denied);
        }
    }

    const withTimeout = (promise, ms, message) => Promise.race([
        promise,
        new Promise((_, reject) => setTimeout(() => reject(new Error(message)), ms)),
    ]);

    // pair connects with a pairing link's code, stores the new device and
    // returns the PC's entry.
    // A device that is paired with this PC already keeps its pairing (the
    // code stays unused) and takes the link's addresses, which may be new.
    // While pairing, a new version of the app waits (see the service worker
    // below): reloading would lose the code. After a pairing the hold stays:
    // the page goes on to the remote, which takes the new version.
    let pairing = 0;
    let afterPairing = () => {};
    const pair = async (info) => {
        pairing++;
        try {
            return await pairOnce(info);
        } catch (e) {
            pairing--;
            afterPairing();
            throw e;
        }
    };
    const pairOnce = async (info) => {
        const known = loadStore().pcs[info.fp];
        if (known) {
            const entry = { ...known, addrs: info.addrs, port: info.port };
            try {
                (await connect(entry)).close();
                savePC(entry);
                return entry;
            } catch (e) {
                console.warn('Stored pairing failed, pairing again:', e.message);
            }
        }
        const conn = new Connection();
        const target = { addrs: info.addrs, port: info.port, fp: info.fp };
        const welcome = await conn.open(target, 'p' + randomText(16), await pairPassword(info.code),
            { t: 'pair', code: info.code, agent: navigator.userAgent });
        conn.close();
        if (!welcome.device || !welcome.key) throw new Error('The PC did not pair this device');
        const entry = { name: welcome.name || info.name, addrs: info.addrs, port: info.port, fp: info.fp, device: welcome.device, key: welcome.key };
        savePC(entry);
        return entry;
    };

    // connect opens a connection to a paired PC.
    const connect = async (entry) => {
        const conn = new Connection();
        try {
            const welcome = await conn.open(entry, 'd' + entry.device + randomText(8), entry.key, { t: 'hello', key: entry.key, v: PROTOCOL });
            if (welcome.name && welcome.name !== entry.name) {
                entry.name = welcome.name;
                savePC(entry);
            }
        } catch (e) {
            conn.close();
            throw e;
        }
        return conn;
    };

    // --- fetch() for the remote ------------------------------------------------

    let live = null;     // the open Connection
    let opening = null;  // a connect() in progress
    const statusListeners = new Set();

    const notify = (state, detail) => statusListeners.forEach(fn => { try { fn(state, detail); } catch (e) { /* ignore */ } });

    // page is the current page without .html: '/', '/app' or '/connect'.
    // Hosts differ: Cloudflare Pages redirects /app.html to /app, a plain
    // file server only knows /app.html; links use the short form.
    const page = location.pathname.replace(/\.html$/, '').replace(/^\/index$/, '/');

    const toConnectPage = (reason) => {
        const url = '/connect' + (reason ? '#' + reason : '');
        if (page !== '/connect') location.replace(url);
    };

    const ensureConnection = () => {
        if (live && !live.closed) return Promise.resolve(live);
        if (opening) return opening;
        const entry = currentPC();
        if (!entry) {
            toConnectPage();
            return Promise.reject(new TypeError('No PC paired'));
        }
        notify('connecting');
        opening = connect(entry).then((conn) => {
            live = conn;
            conn.onclose = (denied) => {
                if (live === conn) live = null;
                notify('down');
                if (denied) { forgetPC(entry.fp); toConnectPage('removed'); }
            };
            notify('up', entry);
            return conn;
        }, (e) => {
            notify('down', e.message);
            if (e.denied) { forgetPC(entry.fp); toConnectPage('removed'); }
            throw new TypeError(e.message); // like a failed fetch: the remote retries
        }).finally(() => { opening = null; });
        return opening;
    };

    const headersObject = (h) => {
        const out = {};
        if (!h) return out;
        new Headers(h).forEach((v, k) => { out[k] = v; });
        return out;
    };

    const nativeFetch = window.fetch.bind(window);

    const apiFetch = async (input, init = {}) => {
        const req = input instanceof Request ? input : null;
        const url = new URL(req ? req.url : String(input), location.href);
        const method = (init.method || (req && req.method) || 'GET').toUpperCase();
        const headers = headersObject(init.headers || (req && req.headers));
        let body = init.body;
        if (body === undefined && req && method !== 'GET' && method !== 'HEAD') body = await req.text();
        if (body !== undefined && body !== null && typeof body !== 'string') body = String(body);
        const conn = await ensureConnection();
        const res = await conn.request(method, url.pathname + url.search, headers, body);
        const noBody = res.s === 204 || res.s === 304 || method === 'HEAD';
        return new Response(noBody ? null : (res.b || ''), { status: res.s, headers: res.h || {} });
    };

    const installFetch = () => {
        window.fetch = (input, init) => {
            let url;
            try { url = new URL(input instanceof Request ? input.url : String(input), location.href); } catch (e) { return nativeFetch(input, init); }
            if (url.origin === location.origin && url.pathname.startsWith('/api/')) return apiFetch(input, init);
            return nativeFetch(input, init);
        };
    };

    window.resoundRTC = {
        parsePairLink, pair, connect, currentPC, forgetPC, loadStore, savePC,
        onStatus: (fn) => { statusListeners.add(fn); return () => statusListeners.delete(fn); },
        disconnect: () => { if (live) live.close(); },
    };

    // Offline start and installability (sw.js, generated by cmd/webapp). A
    // new version is looked for when the app comes to the foreground, and
    // hourly while it stays there (a remote may stay on screen for hours).
    // Once installed, it waits for a moment that interrupts nobody: the app
    // in the background, untouched since it came to the foreground (the
    // usual case: the check is done within a second), or untouched for
    // UPDATE_IDLE with no finger down (talk-through is held). Then it takes
    // over and the page reloads into it; playback on the PC is not affected.
    // Never while pairing: the code in the link works once.
    const UPDATE_IDLE = 30000;
    const UPDATE_CHECK = 60 * 60 * 1000;
    if ('serviceWorker' in navigator) {
        const sw = navigator.serviceWorker;
        let waiting = null;    // an installed new version
        let applying = false;
        let lastInput = 0;     // 0: untouched since it came to the foreground
        let pointers = 0;
        let timer = null;

        const apply = () => {
            clearTimeout(timer);
            if (!waiting || applying) return;
            const quiet = Date.now() - lastInput;
            if (pairing > 0 || !document.hidden && (pointers > 0 || quiet < UPDATE_IDLE)) {
                timer = setTimeout(apply, pointers > 0 || pairing > 0 ? 1000 : UPDATE_IDLE - quiet);
                return;
            }
            applying = true;
            waiting.postMessage('skipWaiting');
        };
        afterPairing = apply;
        sw.addEventListener('controllerchange', () => {
            if (applying) location.reload(); // not on the first install
        });

        const input = () => { lastInput = Date.now(); };
        addEventListener('pointerdown', () => { pointers++; input(); }, { capture: true, passive: true });
        const up = () => { pointers = Math.max(0, pointers - 1); input(); };
        addEventListener('pointerup', up, { capture: true, passive: true });
        addEventListener('pointercancel', up, { capture: true, passive: true });
        addEventListener('keydown', input, { capture: true, passive: true });

        sw.register('/sw.js').then((reg) => {
            const found = (w) => { waiting = w; apply(); };
            if (reg.waiting && sw.controller) found(reg.waiting);
            reg.addEventListener('updatefound', () => {
                const w = reg.installing;
                if (!w) return;
                w.addEventListener('statechange', () => {
                    if (w.state === 'installed' && sw.controller) found(w);
                });
            });
            document.addEventListener('visibilitychange', () => {
                if (document.hidden) {
                    apply();
                } else {
                    lastInput = 0;
                    pointers = 0;
                    apply();
                    reg.update().catch(() => {});
                }
            });
            setInterval(() => { if (!document.hidden) reg.update().catch(() => {}); }, UPDATE_CHECK);
        }).catch((e) => console.warn('Service worker:', e));
    }

    if (page === '/' || page === '/app') {
        // A pairing link (the PC's QR code opens <app>/#pair&…) goes to the
        // connect page, from the landing page or the remote.
        if (location.hash.startsWith('#pair')) location.replace('/connect' + location.hash);
    }

    // On the remote: an unpaired app goes to the connect page; otherwise
    // /api/ requests go over the data channel.
    if (page === '/app' && !location.hash.startsWith('#pair')) {
        if (!currentPC()) {
            toConnectPage();
        } else {
            installFetch();
            ensureConnection().catch(() => { /* the remote shows the state and retries */ });
            // The "PC" section of the remote's settings (app.html).
            document.addEventListener('DOMContentLoaded', () => {
                const name = document.getElementById('app-connection-name');
                const pc = currentPC();
                if (name && pc) name.textContent = pc.name;
                const button = document.getElementById('app-connection-switch');
                if (button) button.addEventListener('click', () => { location.href = '/connect'; });
            });
        }
    }
})();
