"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import type {
  GetMyPageResponse,
  MonthlyTicket,
  UnsettledTicket,
  UpcomingTicket,
} from "@/app/gen/nazobu/v1/mypage_pb";
import { myPageClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  Badge,
  Mono,
  PageShell,
  Section,
  SectionTitle,
} from "@/app/_components";
import {
  daysFromToday,
  formatDateJa,
  formatMonoDate,
  formatYen,
  parseDateTime,
} from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; me: GetMeResponse; data: GetMyPageResponse };

export function HomeView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([userClient.getMe({}), myPageClient.getMyPage({})])
      .then(([me, data]) => {
        if (!cancelled) setState({ kind: "ready", me, data });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (
          err instanceof ConnectError &&
          err.code === Code.Unauthenticated
        ) {
          redirectToLogin(router, "/");
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

  const { me, data } = state;
  const displayName = me.displayName;
  const today = new Date();

  return (
    <>
      <AppHeader brand="謎部" user={displayName} />
      <PageShell>
        {data.unsettled.length > 0 && (
          <Section>
            <SectionTitle count={data.unsettled.length}>未精算</SectionTitle>
            <ul className="mt-3 space-y-3">
              {data.unsettled.map((s) => (
                <UnsettledItem key={s.ticketId} ticket={s} />
              ))}
            </ul>
          </Section>
        )}

        <Section>
          <SectionTitle count={data.upcoming.length}>今後の予定</SectionTitle>
          {data.upcoming.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              参加予定の公演はありません。
            </p>
          ) : (
            <ul className="mt-3 space-y-3">
              {data.upcoming.map((e) => (
                <UpcomingCard key={e.ticketId} ticket={e} today={today} />
              ))}
            </ul>
          )}
        </Section>

        <Section>
          <SectionTitle count={data.monthly.length}>
            {data.monthlyMonth} 月の履歴
          </SectionTitle>
          {data.monthly.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              今月参加した公演はまだありません。
            </p>
          ) : (
            <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
              {data.monthly.map((a) => (
                <MonthlyRow key={a.ticketId} ticket={a} />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}

function UnsettledItem({ ticket }: { ticket: UnsettledTicket }) {
  const date = parseDateTime(ticket.startAt);
  return (
    <li className="overflow-hidden rounded-2xl border border-amber-300 bg-amber-50 transition-colors hover:bg-amber-100">
      <Link href={`/tickets/${ticket.ticketId}`} className="block">
        <div className="flex items-baseline gap-3 px-4 pt-4">
          <Mono className="text-sm font-semibold text-amber-800">
            {formatDateJa(date)}
          </Mono>
          <Mono className="ml-auto text-base font-semibold tracking-tight">
            {formatYen(ticket.pricePerPerson)}
          </Mono>
        </div>
        <h3 className="px-4 pt-1 text-base leading-snug font-semibold">
          {ticket.eventTitle}
        </h3>
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 px-4 pt-3 pb-4 text-xs text-zinc-600">
          <dt className="text-zinc-400">立替</dt>
          <dd>{ticket.payeeName}</dd>
        </dl>
      </Link>
    </li>
  );
}

function UpcomingCard({
  ticket,
  today,
}: {
  ticket: UpcomingTicket;
  today: Date;
}) {
  const date = parseDateTime(ticket.startAt);
  const days = daysFromToday(date, today);
  const dayLabel =
    days <= 0 ? "本日" : days === 1 ? "明日" : `あと ${days} 日`;
  const sortedCompanions = [...ticket.companionNames].sort((a, b) =>
    a.localeCompare(b, "ja"),
  );
  return (
    <li className="overflow-hidden rounded-2xl border border-zinc-200 bg-white transition-colors hover:bg-zinc-50">
      <Link href={`/tickets/${ticket.ticketId}`} className="block">
        <div className="flex items-baseline gap-3 px-4 pt-4">
          <Mono className="text-sm font-semibold text-emerald-700">
            {formatDateJa(date)}
          </Mono>
          <span className="ml-auto text-xs text-zinc-500">{dayLabel}</span>
        </div>
        <h3 className="px-4 pt-1 text-base leading-snug font-semibold">
          {ticket.eventTitle}
        </h3>
        {sortedCompanions.length > 0 ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 px-4 pt-3 pb-4 text-xs text-zinc-600">
            <dt className="text-zinc-400">同行</dt>
            <dd>{sortedCompanions.join("・")}</dd>
          </dl>
        ) : (
          <div className="pb-4" />
        )}
      </Link>
    </li>
  );
}

function MonthlyRow({ ticket }: { ticket: MonthlyTicket }) {
  return (
    <li>
      <Link
        href={`/tickets/${ticket.ticketId}`}
        className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-zinc-50"
      >
        <Mono className="text-xs text-zinc-500">
          {formatMonoDate(parseDateTime(ticket.startAt))}
        </Mono>
        <span className="flex-1 truncate text-sm">{ticket.eventTitle}</span>
        <Badge tone={ticket.settled ? "settled" : "unsettled"}>
          {ticket.settled ? "精算済み" : "未精算"}
        </Badge>
      </Link>
    </li>
  );
}
