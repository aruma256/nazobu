"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Ticket } from "@/app/gen/nazobu/v1/ticket_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { ticketClient, userClient } from "@/app/lib/rpc";

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
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; me: GetMeResponse; tickets: Ticket[] };

export function TicketsView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([userClient.getMe({}), ticketClient.listTickets({})])
      .then(([me, res]) => {
        if (!cancelled) setState({ kind: "ready", me, tickets: res.tickets });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/tickets");
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setState({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

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

  const { me, tickets } = state;
  const displayName = me.displayName !== "" ? me.displayName : me.username;

  return (
    <>
      <AppHeader brand="謎部" user={displayName} />
      <PageShell>
        <Section>
          <SectionTitle count={tickets.length}>チケット一覧</SectionTitle>
          {tickets.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              まだチケットが登録されていません。
            </p>
          ) : (
            <ul className="mt-3 space-y-3">
              {tickets.map((t) => (
                <TicketCard key={t.id} ticket={t} />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}

function TicketCard({ ticket }: { ticket: Ticket }) {
  const date = parseAttendedOn(ticket.attendedOn);
  return (
    <li className="overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      <div className="flex items-baseline gap-3 px-4 pt-4">
        <Mono className="text-sm font-semibold text-emerald-700">
          {formatDateJa(date)}
        </Mono>
        <Mono className="text-xs text-zinc-500">{ticket.meetingTime}</Mono>
        <Mono className="ml-auto text-sm font-semibold tracking-tight">
          {formatYen(ticket.pricePerPerson)}
        </Mono>
      </div>
      <h3 className="px-4 pt-1 text-base leading-snug font-semibold">
        {ticket.eventTitle}
      </h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 px-4 pt-3 pb-4 text-xs text-zinc-600">
        <dt className="text-zinc-400">集合</dt>
        <dd>{ticket.meetingPlace}</dd>
        <dt className="text-zinc-400">立替</dt>
        <dd>{ticket.purchaserName}</dd>
        {ticket.participantNames.length > 0 && (
          <>
            <dt className="text-zinc-400">参加</dt>
            <dd>{ticket.participantNames.join("・")}</dd>
          </>
        )}
      </dl>
    </li>
  );
}
