// Service worker of the hosted app: every file is cached at install, so the
// app starts without the host (only the PC must be reachable). cmd/webapp
// fills in the version and the file list.
const VERSION = '__VERSION__';
const FILES = __FILES__;
const CACHE = 'resound-app-' + VERSION;

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE)
            .then((cache) => cache.addAll(FILES))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys()
            .then((keys) => Promise.all(keys.filter((k) => k.startsWith('resound-app-') && k !== CACHE).map((k) => caches.delete(k))))
            .then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', (event) => {
    const req = event.request;
    if (req.method !== 'GET') return;
    const url = new URL(req.url);
    if (url.origin !== self.location.origin) return;
    // Pages: the cached copy of the requested page (the fragment, e.g. a
    // pairing link, stays with the browser), index.html for anything else.
    if (req.mode === 'navigate') {
        event.respondWith(
            caches.match(url.pathname === '/' ? '/index.html' : url.pathname, { cacheName: CACHE })
                .then((hit) => hit || caches.match('/index.html', { cacheName: CACHE }))
                .then((hit) => hit || fetch(req))
        );
        return;
    }
    event.respondWith(caches.match(req, { cacheName: CACHE, ignoreSearch: true }).then((hit) => hit || fetch(req)));
});
