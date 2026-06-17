"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Ticket } from "@/app/gen/nazobu/v1/ticket_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { myPageClient, ticketClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  Section,
  SectionTitle,
  TicketCard,
  UnsettledBanner,
} from "@/app/_components";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      tickets: Ticket[];
      unsettledCount: number;
      receivablesCount: number;
    };

export function TicketsView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      userClient.getMe({}),
      ticketClient.listTickets({}),
      myPageClient.listMyUnsettledTickets({}),
      myPageClient.listMyUnsettledReceivables({}),
    ])
      .then(([me, res, unsettled, receivables]) => {
        if (!cancelled)
          setState({
            kind: "ready",
            me,
            tickets: res.tickets,
            unsettledCount: unsettled.tickets.length,
            receivablesCount: receivables.tickets.length,
          });
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

  const { me, tickets, unsettledCount, receivablesCount } = state;
  const displayName = me.displayName;
  const isAdmin = me.role === "admin";

  return (
    <>
      <AppHeader brand="謎部" user={displayName} isAdmin={isAdmin} />
      <PageShell>
        <UnsettledBanner
          unsettledCount={unsettledCount}
          receivablesCount={receivablesCount}
        />
        <Section>
          <SectionTitle count={tickets.length}>チケット一覧</SectionTitle>
          {tickets.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              まだチケットが登録されていません。
            </p>
          ) : (
            <ul className="mt-3 space-y-3">
              {tickets.map((t) => (
                <TicketCard key={t.id} ticket={t} myName={displayName} />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}
