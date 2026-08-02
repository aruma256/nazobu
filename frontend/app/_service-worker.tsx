"use client";

import { useEffect } from "react";

// public/sw.js の登録。Android（Chrome）でインストールプロンプトを出すのに必要。
//
// next dev では登録しない。dev のビルド成果物はハッシュが固定ではないため、
// SW のキャッシュが HMR と噛み合わずに古いチャンクを返しうる。
// ローカルで SW の挙動を確認したいときは `npm run build && npm run start` を使う。
export function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") return;
    if (!("serviceWorker" in navigator)) return;

    navigator.serviceWorker
      .register("/sw.js", { scope: "/", updateViaCache: "none" })
      .catch((err: unknown) => {
        console.error("Service Worker の登録に失敗しました", err);
      });
  }, []);

  return null;
}
