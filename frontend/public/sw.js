// 謎部の Service Worker。
//
// 主目的は Android（Chrome）での PWA インストール対応。
// Chrome はメニューからの「アプリをインストール」には SW を要求しなくなったが、
// 自動で出るインストールプロンプト（beforeinstallprompt）は今も
// fetch ハンドラを持つ SW の存在を条件にしている。
// ついでにオフライン時の代替ページも出す。
//
// キャッシュ方針は保守的にする。本アプリはログイン必須で、画面には参加者名や
// 金額といった個人情報が載るため、ページ HTML と RPC レスポンスは一切キャッシュしない。
// キャッシュするのは内容ハッシュ付きで不変な `/_next/static/` 配下だけ。

// キャッシュを作り直したいときはここを上げる（activate 時に旧世代を消す）。
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

// 内容ハッシュ付きのビルド成果物だけをキャッシュ対象にする。
// URL が変わらない限り中身も変わらないので、cache-first で安全。
function isImmutableAsset(url) {
  return url.pathname.startsWith("/_next/static/");
}

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  // ページ遷移はネットワーク優先。オフラインのときだけ代替ページを返す。
  // （認証状態やデータが古いまま表示されるのを避けるため、成功レスポンスは保存しない）
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

  // それ以外（RPC・OG 画像・manifest など）は素通しでネットワークに任せる。
});

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached !== undefined) return cached;

  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(CACHE_NAME);
    // レスポンスは一度しか読めないので複製を保存する。
    await cache.put(request, response.clone());
  }
  return response;
}
