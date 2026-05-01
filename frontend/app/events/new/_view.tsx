"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";

import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { eventClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  PrimaryButton,
  Section,
  SectionTitle,
} from "@/app/_components";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "forbidden"; me: GetMeResponse }
  | { kind: "ready"; me: GetMeResponse };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function NewEventView() {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });
  const [title, setTitle] = useState("");
  const [url, setUrl] = useState("");
  const [submit, setSubmit] = useState<SubmitState>({ kind: "idle" });

  useEffect(() => {
    let cancelled = false;
    userClient
      .getMe({})
      .then((me) => {
        if (cancelled) return;
        if (me.role !== "admin") {
          setLoad({ kind: "forbidden", me });
          return;
        }
        setLoad({ kind: "ready", me });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/events/new");
          return;
        }
        const message =
          err instanceof Error ? err.message : "ユーザー情報の取得に失敗しました";
        setLoad({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submit.kind === "submitting") return;

    const trimmedTitle = title.trim();
    const trimmedUrl = url.trim();
    if (trimmedTitle === "" || trimmedUrl === "") {
      setSubmit({ kind: "error", message: "タイトルと URL を入力してください" });
      return;
    }

    setSubmit({ kind: "submitting" });
    try {
      await eventClient.createEvent({ title: trimmedTitle, url: trimmedUrl });
      router.push("/events");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, "/events/new");
        return;
      }
      const message =
        err instanceof Error ? err.message : "公演の登録に失敗しました";
      setSubmit({ kind: "error", message });
    }
  };

  if (load.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }

  if (load.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {load.message}
          </p>
        </PageShell>
      </>
    );
  }

  const displayName =
    load.me.displayName !== "" ? load.me.displayName : load.me.username;

  if (load.kind === "forbidden") {
    return (
      <>
        <AppHeader brand="謎部" user={displayName} />
        <PageShell>
          <Section>
            <SectionTitle>公演を登録</SectionTitle>
            <p className="mt-3 text-sm text-amber-800">
              公演の登録は管理者のみ行えます。
            </p>
            <div className="pt-4">
              <Link
                href="/events"
                className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
              >
                公演一覧へ戻る
              </Link>
            </div>
          </Section>
        </PageShell>
      </>
    );
  }

  const submitting = submit.kind === "submitting";

  return (
    <>
      <AppHeader brand="謎部" user={displayName} />
      <PageShell>
        <Section>
          <SectionTitle>公演を登録</SectionTitle>
          <form onSubmit={onSubmit} className="mt-3 space-y-4">
            <div>
              <label
                htmlFor="event-title"
                className="block text-sm font-medium text-zinc-700"
              >
                タイトル
              </label>
              <input
                id="event-title"
                type="text"
                required
                maxLength={255}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                disabled={submitting}
                className="mt-1 block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                placeholder="例: 〇〇からの脱出"
              />
            </div>

            <div>
              <label
                htmlFor="event-url"
                className="block text-sm font-medium text-zinc-700"
              >
                URL
              </label>
              <input
                id="event-url"
                type="url"
                required
                maxLength={512}
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                disabled={submitting}
                className="mt-1 block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                placeholder="https://..."
              />
            </div>

            {submit.kind === "error" && (
              <p className="text-sm text-amber-800">{submit.message}</p>
            )}

            <div className="space-y-3 pt-2">
              <PrimaryButton type="submit" disabled={submitting}>
                {submitting ? "登録中…" : "登録する"}
              </PrimaryButton>
              <Link
                href="/events"
                className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
              >
                キャンセル
              </Link>
            </div>
          </form>
        </Section>
      </PageShell>
    </>
  );
}
