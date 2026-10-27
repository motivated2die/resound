// The edit page: Resound opens it in a browser window with a file it can't
// play (a video, or audio in another format), dropped onto a library folder.
// It reads the file from Resound on this PC, embeds the editor
// (audiocutter.online) with the file loaded, and sends every clip the user
// adds back to Resound, which saves it into the folder. Close (in the
// editor's banner), a saved sound of the library, or Stop editing in the
// panel end it and close the window.
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
    // Resound's colours for the editor, which its session takes for its
    // design tokens (session.ui.colors), in its dark theme: the panel's
    // greys, and its accent, a step darker than the panel's (#50dea8 scaled
    // to 80%), with the accent's dark text on buttons and selections
    // (5.8:1). Keys the editor doesn't know are dropped.
    const COLORS = {
        background: '#1c1d22',
        foreground: '#ececf0',
        card: '#2a2c33',
        popover: '#25272d',
        primary: '#40b286',
        primaryForeground: '#102a20',
        secondary: '#3a3d46',
        muted: '#2a2c33', // its banner, under this page's bar
        mutedForeground: '#9a9ca6',
        accent: '#343740',
        border: '#33353c',
    };
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
        notice.replaceWith(editor);
        document.body.classList.add('editing'); // the editor's banner carries the logo, title and Close now

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
                    startFinishing();
                }
                break;
            case 'result':
                addClip(msg);
                break;
            case 'submitted': // after its result: how many the editor has rendered
                rendered = Math.max(rendered, Number(msg.count) || 0);
                break;
            case 'done':
                queue.then(() => done(Number(msg.submitted) || 0));
                break;
            case 'error':
                setStatus('The editor refused the file: ' + (msg.issues || []).join('; '), true);
                break;
            }
        });
    }

    // manifest is the editor's project: the one file, whole, with the
    // clips posted back to this window. The editor is dark, in Resound's
    // colours, whatever theme it shows on its own site, and has a Close
    // button in its banner (its done button: it ends editing, it saves
    // nothing).
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
                        instruction: 'Select the part you want, then press “Add to Resound”: only what you add is saved. Add as many clips as you like.',
                        submitLabel: 'Add to Resound',
                    }),
                    doneLabel: 'Close',
                    theme: 'dark',
                    colors: COLORS,
                    logoUrl: logoData(),
                },
                output: {
                    mode: 'postmessage',
                    format: info.format || 'flac',
                    // New clips at the rate Resound plays at; a sound of
                    // the library keeps its own (rate 0).
                    ...(info.rate ? { formatOptions: { sampleRate: info.rate } } : {}),
                    targetOrigin: location.origin,
                },
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
        canvas.getContext('2d').drawImage(img, 0, 0, 48, 48);
        return canvas.toDataURL('image/png');
    }

    // addClip sends a clip from the editor (its result message) to Resound.
    // Clips are sent one after another, in the order they were added;
    // saved counts those Resound took, rendered those the editor made.
    let queue = Promise.resolve();
    let saved = 0;
    let rendered = 0;
    function addClip(msg) {
        const audio = msg.audio;
        if (!(audio instanceof Blob) || audio.size === 0) {
            setStatus('The editor sent an empty clip.', true);
            return;
        }
        if (msg.extension && msg.extension !== info.format) {
            setStatus(`The editor sent a .${msg.extension} file, not .${info.format}: Resound didn't save it. Use the editor's Download button and drop the file onto the folder.`, true);
            return;
        }
        queue = queue.then(async () => {
            setStatus(info.replace ? 'Saving the sound in Resound…' : 'Adding the clip to Resound…');
            try {
                const res = await resound('/result', {
                    method: 'POST',
                    headers: { 'Content-Type': msg.mimeType || audio.type || 'application/octet-stream' },
                    body: audio,
                });
                const { name } = await res.json();
                saved++;
                showClip(name);
                if (info.replace) {
                    closeSoon(name);
                } else {
                    setStatus(`Added “${name}” to “${info.folder}”. Name it in the Resound panel, or cut another clip; press Close when you're finished.`);
                }
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

    // --- finishing ------------------------------------------------------------------

    // Close in the editor's banner ends editing: Resound stops serving the
    // file at once and the window closes. A sound of the library is done
    // once it's saved: the window closes a moment later unless the user
    // wants to keep editing. Stop editing in the panel ends it too; the
    // page checks every second.
    //
    // The page closes its window itself: a window that a script didn't open
    // may be closed by its page while its history holds a single entry, as
    // here (this page replaces its fragment, the editor its import page),
    // in an Edge app window and in a tab Resound opened. It tells Resound
    // first: a request on the way out (pagehide) doesn't arrive (measured).
    // Should the history have grown, Resound closes the window, found by a
    // title set just for that (editbridge, ui); a tab in a normal browser
    // window it never closes, and the page says the tab can be closed.
    const CLOSE_DELAY = 2500;
    // How long a refused window.close() is waited for: a closing window
    // has hidden its page well before.
    const CLOSE_TRY = 500;
    let finished = false;
    let closing = 0;
    let checking = 0;

    function startFinishing() {
        $('keep').addEventListener('click', () => {
            clearTimeout(closing);
            $('keep').hidden = true;
            setStatus(`Saved in “${info.folder}”. Save again when you've changed it, or press Close.`);
        });
        checking = setInterval(async () => {
            try {
                const res = await fetch(base + '/state', { cache: 'no-store' });
                if (res.ok && (await res.json()).stopped) finish('stopped');
            } catch { /* Resound quit: the next request says so */ }
        }, 1000);
    }

    // done is the editor's Close, once the clips sent so far have arrived.
    // A clip the editor rendered that Resound didn't save had its error in
    // the bar, easily missed: the first Close says so and keeps the window;
    // a second one finishes all the same.
    let warned = false;
    function done(submitted) {
        const lost = Math.max(rendered, submitted) - saved;
        if (lost > 0 && !warned) {
            warned = true;
            setStatus(`${lost === 1 ? 'A clip' : `${lost} clips`} didn't reach Resound. Add ${lost === 1 ? 'it' : 'them'} again, or press Close again to close anyway.`, true);
            return;
        }
        finish('done');
    }

    // closeSoon closes the editor a moment after a sound was saved.
    function closeSoon(name) {
        setStatus(`Saved “${name}” in “${info.folder}”. Closing the editor…`);
        $('keep').hidden = false;
        clearTimeout(closing);
        closing = setTimeout(() => finish('saved'), CLOSE_DELAY);
    }

    function finish(why) {
        if (finished) return;
        finished = true;
        clearTimeout(closing);
        clearInterval(checking);
        $('keep').hidden = true;
        if (history.length > 1) {
            askToClose(why);
            return;
        }
        fetch(base + '/done', { method: 'POST' }).catch(() => {}).finally(() => {
            connected = false; // done: the file is gone, nothing to close
            window.close();
            setTimeout(() => closable(why), CLOSE_TRY);
        });
    }

    // askToClose: the browser wouldn't let the page close the window;
    // Resound closes it if it's an app window, found by its title.
    function askToClose(why) {
        const random = crypto.getRandomValues(new Uint8Array(8));
        const marker = 'Resound · Closing ' + Array.from(random, (b) => b.toString(16).padStart(2, '0')).join('');
        const title = document.title;
        document.title = marker;
        connected = false;
        fetch(base + '/done', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ window: marker }),
        }).catch(() => {});
        // Still here: not a window Resound closes (a tab of a normal browser
        // window), or Resound is gone.
        setTimeout(() => {
            document.title = title;
            closable(why);
        }, CLOSE_DELAY);
    }

    // closable: the window is still open after finishing; the page says it
    // can be closed.
    function closable(why) {
        const text = {
            done: info.replace ? 'Editing ended.' : `Editing ended. The clips you added are in “${info.folder}”.`,
            saved: `Saved “${info.name}” in “${info.folder}”.`,
            stopped: 'Editing was stopped in the Resound panel.',
        }[why] + ' You can close this page now.';
        editor?.replaceWith(notice);
        document.body.classList.remove('editing');
        notice.hidden = false;
        $('spinner').hidden = true;
        $('progress').hidden = true;
        $('notice-hint').hidden = true;
        $('notice-text').textContent = text;
        setStatus(text);
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
    // reaching Resound is what makes the browser ask for its permission;
    // not after finishing, which told it already.
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
