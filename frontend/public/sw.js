// Chrome はメニューからの「アプリをインストール」には SW を要求しなくなったが、
// 自動で出るインストールプロンプト（beforeinstallprompt）は今も
// fetch ハンドラを持つ SW の存在を条件にしている。

const VERSION = "v1";
const CACHE_NAME = `nazobu-${VERSION}`;

const OFFLINE_URL = "/offline.html";

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(CACHE_NAME);
      await cache.add(new Request(OFFLINE_URL, { cache: "reload" }));
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key)),
      );
      await self.clients.claim();
    })(),
  );
});

function isImmutableAsset(url) {
  return url.pathname.startsWith("/_next/static/");
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  if (request.mode === "navigate") {
    event.respondWith(
      (async () => {
        try {
          return await fetch(request);
        } catch {
          const offline = await caches.match(OFFLINE_URL);
          return offline ?? Response.error();
        }
      })(),
    );
    return;
  }

  if (isImmutableAsset(url)) {
    event.respondWith(cacheFirst(request));
    return;
  }
});

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached !== undefined) return cached;

  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(CACHE_NAME);
    await cache.put(request, response.clone());
  }
  return response;
}
