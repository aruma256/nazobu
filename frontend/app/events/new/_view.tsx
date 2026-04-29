"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import type { FormEvent } from "react";

import { eventClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  PrimaryButton,
  Section,
  SectionTitle,
} from "@/app/_components";

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function NewEventView() {
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [url, setUrl] = useState("");
  const [state, setState] = useState<SubmitState>({ kind: "idle" });

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (state.kind === "submitting") return;

    const trimmedTitle = title.trim();
    const trimmedUrl = url.trim();
    if (trimmedTitle === "" || trimmedUrl === "") {
      setState({ kind: "error", message: "タイトルと URL を入力してください" });
      return;
    }

    setState({ kind: "submitting" });
    try {
      await eventClient.createEvent({ title: trimmedTitle, url: trimmedUrl });
      router.push("/events");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        setState({
          kind: "error",
          message: "ログインが切れています。ログインし直してください。",
        });
        return;
      }
      const message =
        err instanceof Error ? err.message : "公演の登録に失敗しました";
      setState({ kind: "error", message });
    }
  };

  const submitting = state.kind === "submitting";

  return (
    <>
      <AppHeader brand="謎部" user="" />
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

            {state.kind === "error" && (
              <p className="text-sm text-amber-800">{state.message}</p>
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
