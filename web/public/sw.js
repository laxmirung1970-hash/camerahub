// Cache only the application shell. Never cache camera video, API responses or secrets.
const VERSION='camerahub-shell-v3';
self.addEventListener('install',event=>{event.waitUntil(caches.open(VERSION).then(cache=>cache.addAll(['/icon.svg','/manifest.webmanifest'])));self.skipWaiting();});
self.addEventListener('activate',event=>{event.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k!==VERSION).map(k=>caches.delete(k)))));self.clients.claim();});
self.addEventListener('fetch',event=>{const u=new URL(event.request.url);if(u.origin!==location.origin||u.pathname.startsWith('/api/')||event.request.method!=='GET')return;if(u.pathname==='/icon.svg'||u.pathname==='/manifest.webmanifest')event.respondWith(caches.match(event.request).then(r=>r||fetch(event.request)));});
