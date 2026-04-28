"use client";

import { useEffect, useState } from "react";

type Me = {
  id: string;
  username: string;
  display_name: string | null;
  avatar_url: string | null;
};

type LoadState =
  | { kind: "loading" }
  | { kind: "anonymous" }
  | { kind: "logged_in"; me: Me };

export default function Home() {
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    fetch("/api/me", { credentials: "same-origin" })
      .then(async (r) => {
        if (cancelled) return;
        if (r.status === 401) {
          setState({ kind: "anonymous" });
          return;
        }
        if (r.ok) {
          const me = (await r.json()) as Me;
          setState({ kind: "logged_in", me });
          return;
        }
        setState({ kind: "anonymous" });
      })
      .catch(() => {
        if (!cancelled) setState({ kind: "anonymous" });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-6 p-8">
      <h1 className="text-4xl font-bold tracking-tight">謎部</h1>
      <p className="text-sm text-zinc-500 dark:text-zinc-400">
        謎解き仲間のための身内 Web サービス
      </p>

      {state.kind === "loading" && (
        <p className="text-sm text-zinc-500 dark:text-zinc-400">読み込み中…</p>
      )}

      {state.kind === "anonymous" && (
        <a
          href="/auth/discord/login"
          className="mt-4 inline-flex h-11 items-center justify-center rounded-md bg-indigo-600 px-6 font-medium text-white transition-colors hover:bg-indigo-500"
        >
          Discord でログイン
        </a>
      )}

      {state.kind === "logged_in" && (
        <div className="mt-4 flex flex-col items-center gap-3">
          <p className="text-base">
            ログイン中:{" "}
            <span className="font-semibold">
              {state.me.display_name ?? state.me.username}
            </span>
          </p>
          <button
            type="button"
            onClick={() =>
              fetch("/auth/logout", { method: "POST" }).then(() =>
                setState({ kind: "anonymous" }),
              )
            }
            className="rounded bg-zinc-200 px-4 py-2 text-sm hover:bg-zinc-300 dark:bg-zinc-800 dark:hover:bg-zinc-700"
          >
            ログアウト
          </button>
        </div>
      )}
    </main>
  );
}
