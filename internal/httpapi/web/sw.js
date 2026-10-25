const CACHE_NAME = "fp-shell-v35";
const DOCUMENT_CACHE_NAME = "fp-documents-v1";
const TFR_CACHE_NAME = "fp-faa-tfr-v1";
const APP_SHELL = ["/", "/app.css", "/app.js", "/manifest.webmanifest", "/icon.svg", "/icon-180.png", "/icon-192.png", "/icon-512.png"];

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(APP_SHELL)));
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(
    keys.filter((key) => key !== CACHE_NAME && key !== DOCUMENT_CACHE_NAME && key !== AIRSPACE_CACHE_NAME && key !== TFR_CACHE_NAME).map((key) => caches.delete(key))
  )));
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.origin !== self.location.origin) return;
  const offlineChart = /^\/api\/v1\/(?:procedures\/[A-Z0-9]{3,4}\/files\/[A-Za-z0-9_-]+\.pdf|vfr\/files\/[A-Za-z0-9_-]+\.pdf)$/.test(url.pathname);
  if (offlineChart) {
    event.respondWith((async () => {
      const cache = await caches.open(DOCUMENT_CACHE_NAME);
      try {
        const response = await fetch(event.request);
        if (response.ok && response.headers.get("content-type")?.includes("application/pdf")) {
          try {
            await cache.put(event.request, response.clone());
          } catch {
            // Storage quota or policy may prevent a local copy; keep the online response usable.
          }
        }
        return response;
      } catch {
        return await cache.match(event.request) || Response.error();
      }
    })());
    return;
  }
  if (url.pathname === "/api/v1/tfr") {
    event.respondWith((async () => {
      const cache = await caches.open(TFR_CACHE_NAME);
      try {
        const response = await fetch(event.request);
        if (response.ok) {
          const body = await response.clone().json();
          if (body?.type === "FeatureCollection" && Array.isArray(body.features)) await cache.put(event.request, response.clone());
        }
        return response;
      } catch {
        const fallback = await cache.match(event.request);
        if (!fallback) return Response.error();
        const headers = new Headers(fallback.headers);
        headers.set("X-FP-Offline-Cache", "true");
        return new Response(fallback.body, { status: fallback.status, statusText: fallback.statusText, headers });
      }
    })());
    return;
  }

  if (url.pathname === "/api/v1/airspace") {
    event.respondWith((async () => {
      const cache = await caches.open(AIRSPACE_CACHE_NAME);
      try {
        const response = await fetch(event.request);
        if (response.ok) {
          const body = await response.clone().json();
          if (body?.type === "FeatureCollection" && Array.isArray(body.features)) {
            await Promise.all((await cache.keys()).map((request) => cache.delete(request)));
            await cache.put(event.request, response.clone());
          }
        }
        return response;
      } catch {
        const fallback = await cache.match(event.request);
        if (!fallback) return Response.error();
        const headers = new Headers(fallback.headers);
        headers.set("X-FP-Offline-Cache", "true");
        return new Response(fallback.body, { status: fallback.status, statusText: fallback.statusText, headers });
      }
    })());
    return;
  }
  if (url.pathname.startsWith("/api/")) return;
  event.respondWith(fetch(event.request).then((response) => {
    if (response.ok) caches.open(CACHE_NAME).then((cache) => cache.put(event.request, response.clone()));
    return response;
  }).catch(async () => (await caches.match(event.request)) || Response.error()));
});
