"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";

import type { Event as NazobuEvent } from "@/app/gen/nazobu/v1/event_pb";
import { eventClient } from "@/app/lib/rpc";

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
  | { kind: "not_found" }
  | { kind: "error"; message: string }
  | { kind: "ready"; event: NazobuEvent };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function EventEditView({ eventId }: { eventId: string }) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    eventClient
      .getEvent({ eventId })
      .then((res) => {
        if (cancelled) return;
        if (!res.event) {
          setLoad({ kind: "not_found" });
          return;
        }
        setLoad({ kind: "ready", event: res.event });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, `/events/${eventId}/edit`);
          return;
        }
        if (err instanceof ConnectError && err.code === Code.NotFound) {
          setLoad({ kind: "not_found" });
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [eventId, router]);

  if (load.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" isAdmin />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }
  if (load.kind === "not_found") {
    return (
      <>
        <AppHeader brand="謎部" user="" isAdmin />
        <PageShell>
          <div className="space-y-4 pt-8 text-sm text-zinc-700">
            <p>指定された公演が見つかりませんでした。</p>
            <Link
              href="/events"
              className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
            >
              公演一覧に戻る
            </Link>
          </div>
        </PageShell>
      </>
    );
  }
  if (load.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" isAdmin />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {load.message}
          </p>
        </PageShell>
      </>
    );
  }

  return <Form router={router} event={load.event} />;
}

function Form({
  router,
  event,
}: {
  router: ReturnType<typeof useRouter>;
  event: NazobuEvent;
}) {
  const [title, setTitle] = useState(event.title);
  const [url, setUrl] = useState(event.url);
  const [catchphrase, setCatchphrase] = useState(event.catchphrase);
  const [doorsOpen, setDoorsOpen] = useState(
    event.doorsOpenMinutesBefore !== undefined
      ? String(event.doorsOpenMinutesBefore)
      : "",
  );
  const [entryDeadline, setEntryDeadline] = useState(
    event.entryDeadlineMinutesBefore !== undefined
      ? String(event.entryDeadlineMinutesBefore)
      : "",
  );
  const [expectedDuration, setExpectedDuration] = useState(
    String(event.expectedDurationMinutes),
  );
  const [submit, setSubmit] = useState<SubmitState>({ kind: "idle" });

  const submitting = submit.kind === "submitting";

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    const trimmedTitle = title.trim();
    const trimmedUrl = url.trim();
    const trimmedCatchphrase = catchphrase.trim();
    if (trimmedTitle === "" || trimmedUrl === "") {
      setSubmit({ kind: "error", message: "タイトルと URL を入力してください" });
      return;
    }
    if (trimmedCatchphrase.length > 255) {
      setSubmit({ kind: "error", message: "キャッチコピーは 255 文字以内で入力してください" });
      return;
    }

    const parsedDoorsOpen = parseOptionalNonNegativeInt(doorsOpen);
    if (parsedDoorsOpen === "invalid") {
      setSubmit({ kind: "error", message: "開場は 0 以上の整数で入力してください" });
      return;
    }
    const parsedEntryDeadline = parseOptionalNonNegativeInt(entryDeadline);
    if (parsedEntryDeadline === "invalid") {
      setSubmit({ kind: "error", message: "入場締切は 0 以上の整数で入力してください" });
      return;
    }
    const parsedExpectedDuration = parsePositiveInt(expectedDuration);
    if (parsedExpectedDuration === "invalid") {
      setSubmit({ kind: "error", message: "想定所要時間は 1 以上の整数で入力してください" });
      return;
    }

    setSubmit({ kind: "submitting" });
    try {
      await eventClient.updateEvent({
        eventId: event.id,
        title: trimmedTitle,
        url: trimmedUrl,
        catchphrase: trimmedCatchphrase,
        doorsOpenMinutesBefore: parsedDoorsOpen,
        entryDeadlineMinutesBefore: parsedEntryDeadline,
        expectedDurationMinutes: parsedExpectedDuration,
      });
      router.push("/events");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/events/${event.id}/edit`);
        return;
      }
      const message =
        err instanceof Error ? err.message : "公演の更新に失敗しました";
      setSubmit({ kind: "error", message });
    }
  };

  return (
    <>
      <AppHeader brand="謎部" user="" isAdmin />
      <PageShell>
        <Section>
          <SectionTitle>公演を編集</SectionTitle>
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
              <p className="mt-1 text-xs text-zinc-500">
                <code className="font-mono">realdgame.jp</code> /{" "}
                <code className="font-mono">escape.id</code> の URL を入れると、自動でカード画像も取得します。
              </p>
            </div>

            <div>
              <label
                htmlFor="event-catchphrase"
                className="block text-sm font-medium text-zinc-700"
              >
                キャッチコピー（任意）
              </label>
              <input
                id="event-catchphrase"
                type="text"
                maxLength={255}
                value={catchphrase}
                onChange={(e) => setCatchphrase(e.target.value)}
                disabled={submitting}
                className="mt-1 block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                placeholder="例: 限られた時間で謎を解け！"
              />
            </div>

            <div>
              <label
                htmlFor="event-doors-open"
                className="block text-sm font-medium text-zinc-700"
              >
                開場（任意・開始の何分前か）
              </label>
              <div className="mt-1 flex items-center gap-2">
                <input
                  id="event-doors-open"
                  type="number"
                  min={0}
                  step={1}
                  inputMode="numeric"
                  value={doorsOpen}
                  onChange={(e) => setDoorsOpen(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  placeholder="例: 15"
                />
                <span className="text-sm text-zinc-600">分前</span>
              </div>
            </div>

            <div>
              <label
                htmlFor="event-entry-deadline"
                className="block text-sm font-medium text-zinc-700"
              >
                入場締切（任意・開始の何分前か）
              </label>
              <div className="mt-1 flex items-center gap-2">
                <input
                  id="event-entry-deadline"
                  type="number"
                  min={0}
                  step={1}
                  inputMode="numeric"
                  value={entryDeadline}
                  onChange={(e) => setEntryDeadline(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  placeholder="例: 2"
                />
                <span className="text-sm text-zinc-600">分前</span>
              </div>
              <p className="mt-1 text-xs text-zinc-500">
                これを過ぎると参加できなくなる時刻。
              </p>
            </div>

            <div>
              <label
                htmlFor="event-expected-duration"
                className="block text-sm font-medium text-zinc-700"
              >
                想定所要時間
              </label>
              <div className="mt-1 flex items-center gap-2">
                <input
                  id="event-expected-duration"
                  type="number"
                  required
                  min={1}
                  step={1}
                  inputMode="numeric"
                  value={expectedDuration}
                  onChange={(e) => setExpectedDuration(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
                <span className="text-sm text-zinc-600">分</span>
              </div>
            </div>

            {submit.kind === "error" && (
              <p className="text-sm text-amber-800">{submit.message}</p>
            )}

            <div className="space-y-3 pt-2">
              <PrimaryButton type="submit" disabled={submitting}>
                {submitting ? "更新中…" : "更新する"}
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

function parseOptionalNonNegativeInt(raw: string): number | undefined | "invalid" {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n) || n < 0) return "invalid";
  return n;
}

// 必須 / 1 以上の整数。
function parsePositiveInt(raw: string): number | "invalid" {
  const trimmed = raw.trim();
  if (trimmed === "") return "invalid";
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n) || n < 1) return "invalid";
  return n;
}
