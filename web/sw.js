const CACHE = 'codex-runner-shell-v12';
const ASSETS = ['./', './index.html', './styles.css', './app.js', './manifest.webmanifest', './icon.svg'];
const scope = new URL(self.registration.scope);
const shellPaths = new Set(ASSETS.map((asset) => new URL(asset, scope).pathname));

self.addEventListener('install', (event) => event.waitUntil(
  caches.open(CACHE).then((cache) => cache.addAll(ASSETS)).then(() => self.skipWaiting()),
));

self.addEventListener('activate', (event) => event.waitUntil(
  caches.keys()
    .then((keys) => Promise.all(keys.filter((key) => key.startsWith('codex-runner-shell-') && key !== CACHE).map((key) => caches.delete(key))))
    .then(() => self.clients.claim()),
));

self.addEventListener('fetch', (event) => {
  const request = event.request;
  const url = new URL(request.url);
  if (request.method !== 'GET' || url.origin !== self.location.origin) return;
  if (url.pathname.startsWith('/v1/') || url.pathname.startsWith('/internal/')) return;
  // Never serve authenticated or dynamic API requests from the shell cache.
  if (!shellPaths.has(url.pathname) && request.mode !== 'navigate') return;

  // Online-first: deployed UI assets and navigations should not stay stale.
  // Use the previously installed shell only when the network is unavailable.
  event.respondWith((async () => {
    const cache = await caches.open(CACHE);
    try {
      const response = await fetch(request, {cache: 'no-store'});
      if (response.ok && response.type === 'basic') {
        const cacheRequest = request.mode === 'navigate' ? new Request(new URL('./index.html', scope)) : request;
        await cache.put(cacheRequest, response.clone());
      }
      return response;
    } catch (error) {
      const fallback = request.mode === 'navigate' ? new URL('./index.html', scope).toString() : request;
      const cached = await cache.match(fallback);
      if (cached) return cached;
      throw error;
    }
  })());
});
