"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useEffect, useState } from "react";

import type { Event as NazobuEvent, EventTicket } from "@/app/gen/nazobu/v1/event_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { eventClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  Mono,
  PageShell,
  Section,
  SectionTitle,
} from "@/app/_components";
import {
  formatDateJa,
  formatYen,
  parseAttendedOn,
} from "@/app/_format";

type LoadState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "error"; message: string }
  | { kind: "ready"; me: GetMeResponse; events: NazobuEvent[] };

export function EventsView() {
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([userClient.getMe({}), eventClient.listEvents({})])
      .then(([me, res]) => {
        if (!cancelled) setState({ kind: "ready", me, events: res.events });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          setState({ kind: "unauthenticated" });
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setState({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (state.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }

  if (state.kind === "unauthenticated") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <div className="space-y-4 pt-8 text-sm text-zinc-700">
            <p>このページを表示するにはログインが必要です。</p>
            <a
              href="/auth/discord/login"
              className="inline-flex h-11 items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white hover:bg-emerald-800"
            >
              Discord でログイン
            </a>
          </div>
        </PageShell>
      </>
    );
  }

  if (state.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {state.message}
          </p>
        </PageShell>
      </>
    );
  }

  const { me, events } = state;
  const displayName = me.displayName !== "" ? me.displayName : me.username;

  return (
    <>
      <AppHeader brand="謎部" user={displayName} />
      <PageShell>
        <Section>
          <Link
            href="/events/new"
            className="inline-flex h-11 w-full items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900"
          >
            公演を登録
          </Link>
        </Section>

        <Section>
          <SectionTitle count={events.length}>公演一覧</SectionTitle>
          {events.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              まだ公演が登録されていません。
            </p>
          ) : (
            <ul className="mt-3 space-y-4">
              {events.map((e) => (
                <EventCard key={e.id} event={e} />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}

function EventCard({ event }: { event: NazobuEvent }) {
  return (
    <li className="overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      <div className="px-4 pt-4">
        <h3 className="text-base leading-snug font-semibold">
          {event.url !== "" ? (
            <a
              href={event.url}
              target="_blank"
              rel="noreferrer noopener"
              className="underline decoration-zinc-300 underline-offset-4 hover:decoration-zinc-500"
            >
              {event.title}
            </a>
          ) : (
            event.title
          )}
        </h3>
      </div>

      {event.tickets.length === 0 ? (
        <p className="px-4 pt-3 text-sm text-zinc-500">チケットはまだありません。</p>
      ) : (
        <ul className="mt-3 divide-y divide-zinc-200 border-t border-zinc-200">
          {event.tickets.map((t) => (
            <TicketRow key={t.id} ticket={t} />
          ))}
        </ul>
      )}

      <div className="px-4 pt-4 pb-4">
        <button
          type="button"
          disabled
          className="inline-flex h-11 w-full cursor-not-allowed items-center justify-center rounded-lg border border-dashed border-zinc-300 bg-zinc-50 px-4 text-sm font-semibold text-zinc-500"
          aria-label="チケットを登録（準備中）"
        >
          チケットを登録（準備中）
        </button>
      </div>
    </li>
  );
}

function TicketRow({ ticket }: { ticket: EventTicket }) {
  const date = parseAttendedOn(ticket.attendedOn);
  return (
    <li className="px-4 py-3">
      <div className="flex items-baseline gap-3">
        <Mono className="text-sm font-semibold text-emerald-700">
          {formatDateJa(date)}
        </Mono>
        <Mono className="ml-auto text-sm font-semibold tracking-tight">
          {formatYen(ticket.pricePerPerson)}
        </Mono>
      </div>
      <p className="mt-1 text-xs text-zinc-600">
        <span className="text-zinc-400">立替</span> {ticket.purchaserName}
      </p>
      {ticket.participantNames.length > 0 && (
        <p className="mt-1 text-xs text-zinc-600">
          <span className="text-zinc-400">参加</span>{" "}
          {ticket.participantNames.join("・")}
        </p>
      )}
    </li>
  );
}
