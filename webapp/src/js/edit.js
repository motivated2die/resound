// The edit page: Resound opens it in a browser window with a file it can't
// play (a video, or audio in another format), dropped onto a library folder.
// It reads the file from Resound on this PC, embeds the editor
// (audiocutter.online) with the file loaded, and sends every clip the user
// adds back to Resound, which saves it into the folder.
//
// Resound serves the file on 127.0.0.1 (internal/editbridge); the port and
// a token come in the fragment, which the browser never sends anywhere. The
// page talks to nothing but Resound and the editor (its Content-Security-
// Policy allows nothing else, not even this site), so the host never learns
// about the file or the clips. The window title never names the file: it
// ends up in the browser's history.
'use strict';

(() => {
    const EDITOR = 'https://audiocutter.online';
    // The editor's colours can't be styled from here (another origin), and
    // its session takes only an accent. Its controls all use one blue,
    // Tailwind's blue-600 (#155dfc, as of Oct 2026); turning the whole
    // frame's hue by HUE makes that a teal green (#008b70, white text at
    // 4.3:1), and greys and black stay as they are. The accent is that same
    // blue, so "Add to Resound" turns the same green as the editor's other
    // buttons; the logo is drawn turned back by HUE, so it ends up in its
    // own colours. Red warnings turn magenta.
    const HUE = -60;
    const ACCENT = '#155dfc';
    const STORE = 'resound.edit';
    // The user's OK to load the editor (once per browser): set by the
    // consent notice, read before anything reaches Resound.
    const CONSENT = 'resound.editorConsent';

    const $ = (id) => document.getElementById(id);
    const status = $('status');
    const notice = $('notice');

    // --- where Resound is -------------------------------------------------------

    // The fragment is moved into this tab's session storage and out of the
    // address bar (and so out of history); a reload finds it there.
    function readParams() {
        const hash = new URLSearchParams(location.hash.slice(1));
        let params = null;
        if (hash.get('v') === '1' && /^\d{1,5}$/.test(hash.get('p') || '') && /^[0-9a-f]{32}$/.test(hash.get('t') || '')) {
            params = { port: hash.get('p'), token: hash.get('t') };
            try { sessionStorage.setItem(STORE, JSON.stringify(params)); } catch { /* private mode: reloads lose it */ }
            history.replaceState(null, '', location.pathname);
        } else {
            try { params = JSON.parse(sessionStorage.getItem(STORE) || 'null'); } catch { params = null; }
        }
        return params;
    }

    const params = readParams();
    if (!params) {
        fail('This page opens from Resound. On the Sound library page in the Resound panel, drop a video or another audio file onto a folder, or click a sound’s scissors.');
        return;
    }
    const base = `http://127.0.0.1:${params.port}/v1/${params.token}`;

    // --- talking to Resound -----------------------------------------------------

    class ResoundError extends Error {}

    // resound fetches from Resound; a refusal carries Resound's own message.
    async function resound(path, init) {
        let res;
        try {
            res = await fetch(base + path, { cache: 'no-store', ...init });
        } catch {
            throw new ResoundError(await unreachableMessage());
        }
        if (!res.ok) {
            let msg = `Resound answered ${res.status}.`;
            try { msg = (await res.json()).error || msg; } catch { /* not JSON */ }
            throw new ResoundError(msg);
        }
        return res;
    }

    // A failed request can't tell a closed Resound from a browser that
    // blocks this page from reaching it; the permission, if readable, can.
    async function unreachableMessage() {
        for (const name of ['loopback-network', 'local-network-access', 'local-network']) {
            try {
                if ((await navigator.permissions.query({ name })).state === 'denied') {
                    return 'Your browser blocks this page from reaching Resound on this PC. Open the site settings of getresound.app ' +
                        '(the icon at the left of the address bar, or in the title bar), allow access to apps on this device, and reload.';
                }
            } catch { /* this browser doesn't know the name */ }
        }
        return "Can't reach Resound: it was closed, or editing this file was stopped in its panel. Drop the file onto a folder again to open a new editor.";
    }

    // download reads the file with progress; the editor gets it as a File,
    // so it knows its name and type.
    async function download(info) {
        const res = await resound('/source');
        const total = info.size || Number(res.headers.get('Content-Length')) || 0;
        const progress = $('progress');
        progress.hidden = total === 0;
        const reader = res.body.getReader();
        const chunks = [];
        let got = 0;
        for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            chunks.push(value);
            got += value.length;
            if (total) progress.value = got / total;
        }
        return new File(chunks, info.filename, { type: info.type || '' });
    }

    // --- the editor ---------------------------------------------------------------

    let editor = null;
    let sent = false;
    let info = null;

    function openEditor(file) {
        editor = document.createElement('iframe');
        editor.title = 'Audio editor';
        editor.allow = 'autoplay; clipboard-write; fullscreen';
        editor.referrerPolicy = 'no-referrer';
        editor.src = EDITOR + '/import/';
        editor.style.filter = `hue-rotate(${HUE}deg)`;
        // Its own scrolling would only reveal empty space (edit.css); it must
        // be set before the frame loads.
        editor.setAttribute('scrolling', 'no');
        notice.replaceWith(editor);
        document.body.classList.add('editing'); // the editor's banner carries the logo and title now

        // The import page waits 30 s for the file after saying it's ready;
        // the file is loaded already, so it goes at once.
        window.addEventListener('message', (event) => {
            if (event.origin !== EDITOR || event.source !== editor.contentWindow) return;
            const msg = event.data;
            if (!msg || typeof msg !== 'object' || msg.source !== 'audiocutter') return;
            switch (msg.type) {
            case 'ready':
                if (!sent) {
                    sent = true;
                    editor.contentWindow.postMessage({ type: 'session', manifest: manifest(file) }, EDITOR);
                    setStatus(info.replace
                        ? `Editing “${info.name}” in “${info.folder}”. Select the part to keep and press “Save to Resound”: it replaces the sound.`
                        : `Saving clips into “${info.folder}”. Select the part you want and press “Add to Resound”.`);
                }
                break;
            case 'result':
                addClip(msg.audio);
                break;
            case 'error':
                setStatus('The editor refused the file: ' + (msg.issues || []).join('; '), true);
                break;
            }
        });
    }

    // manifest is the editor's project: the one file, whole, with the
    // clips posted back to this window.
    function manifest(file) {
        return {
            version: 2,
            project: {
                files: [{ id: 'source', file }],
                fragments: [{ id: 'clip', fileId: 'source' }],
            },
            session: {
                ui: {
                    ...(info.replace ? {
                        title: 'Edit a sound for Resound',
                        instruction: 'Select the part to keep, then press “Save to Resound”. It replaces the sound in your library.',
                        submitLabel: 'Save to Resound',
                    } : {
                        title: 'Cut a sound for Resound',
                        instruction: 'Select the part you want, then press “Add to Resound”. You can add as many clips as you like.',
                        submitLabel: 'Add to Resound',
                    }),
                    accentColor: ACCENT,
                    logoUrl: logoData(),
                },
                output: { mode: 'postmessage', format: info.format || 'flac', targetOrigin: location.origin },
            },
        };
    }

    // logoData is the Resound logo as a data: URL, drawn from the copy this
    // page has loaded already. The editor shows it in its banner; a link to
    // the logo on this site would make the editor fetch it from the host
    // every time someone cuts a sound.
    function logoData() {
        const img = document.querySelector('.logo');
        if (!img || !img.complete || !img.naturalWidth) return undefined;
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 48; // the banner shows it 24 px high
        const ctx = canvas.getContext('2d');
        ctx.filter = `hue-rotate(${-HUE}deg)`; // the frame turns it back
        ctx.drawImage(img, 0, 0, 48, 48);
        return canvas.toDataURL('image/png');
    }

    // addClip sends a clip from the editor to Resound. Clips are sent one
    // after another, in the order they were added.
    let queue = Promise.resolve();
    function addClip(audio) {
        if (!(audio instanceof Blob) || audio.size === 0) {
            setStatus('The editor sent an empty clip.', true);
            return;
        }
        queue = queue.then(async () => {
            setStatus(info.replace ? 'Saving the sound in Resound…' : 'Adding the clip to Resound…');
            try {
                const res = await resound('/result', {
                    method: 'POST',
                    headers: { 'Content-Type': audio.type || 'audio/mpeg' },
                    body: audio,
                });
                const { name } = await res.json();
                showClip(name);
                setStatus(info.replace
                    ? `Saved “${name}” in “${info.folder}”. Keep editing and save again, or close this window.`
                    : `Added “${name}” to “${info.folder}”. Name it in the Resound panel, or cut another clip.`);
            } catch (e) {
                setStatus(e instanceof ResoundError ? e.message : 'The clip could not be sent to Resound.', true);
            }
        });
    }

    // showClip adds a saved clip's chip to the bar. The newest is
    // highlighted and scrolled into view; older ones slide out to the left.
    function showClip(name) {
        const clips = $('clips');
        clips.querySelector('.latest')?.classList.remove('latest');
        const li = document.createElement('li');
        li.className = 'latest';
        li.textContent = '✓ ' + name;
        li.title = name;
        clips.append(li);
        clips.classList.toggle('overflowing', clips.scrollWidth > clips.clientWidth);
        clips.scrollTo({ left: clips.scrollWidth, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
    }

    // --- page state -----------------------------------------------------------------

    function setStatus(text, bad = false) {
        status.textContent = text;
        status.classList.toggle('bad', bad);
        status.title = text;
    }

    function fail(text) {
        $('consent').hidden = true;
        notice.hidden = false;
        setStatus(text, true);
        notice.classList.add('bad');
        $('spinner').hidden = true;
        $('progress').hidden = true;
        $('notice-hint').hidden = true;
        $('notice-text').textContent = text;
    }

    // Tell Resound when the window goes away; a reload comes back within
    // its grace period and keeps the file open. Not before the user's OK:
    // reaching Resound is what makes the browser ask for its permission.
    let connected = false;
    window.addEventListener('pagehide', () => {
        if (connected) fetch(base + '/close', { method: 'POST', keepalive: true }).catch(() => {});
    });

    // First, once per browser, the consent notice: the editor is a third
    // party's, loaded from its site. Its OK starts everything else.
    function consented() {
        try { return localStorage.getItem(CONSENT) === '1'; } catch { return false; }
    }

    if (consented()) {
        start();
    } else {
        setStatus('The editor opens once you accept.');
        const consent = $('consent');
        consent.hidden = false;
        $('accept').focus();
        $('accept').addEventListener('click', () => {
            try { localStorage.setItem(CONSENT, '1'); } catch { /* asked again next time */ }
            consent.hidden = true;
            start();
        }, { once: true });
    }

    async function start() {
        notice.hidden = false;
        setStatus('Connecting to Resound…');
        connected = true;
        try {
            info = await (await resound('')).json();
            $('notice-text').textContent = `Loading “${info.filename}” from Resound…`;
            setStatus(`Loading “${info.filename}”…`);
            const file = await download(info);
            openEditor(file);
        } catch (e) {
            fail(e instanceof ResoundError ? e.message : 'Something went wrong: ' + e.message);
        }
    }
})();
