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

type MonthView = {
  year: number;
  month: number;
  monthly: MonthlyTicket[];
  loading: boolean;
};

// 当月（サーバ基準の現在年月）から相対的に diff ヶ月ぶんずらした年月を返す。
function shiftMonth(
  base: { year: number; month: number },
  diff: number,
): { year: number; month: number } {
  const total = base.year * 12 + (base.month - 1) + diff;
  return { year: Math.floor(total / 12), month: (total % 12) + 1 };
}

export function HomeView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [monthView, setMonthView] = useState<MonthView | null>(null);

  useEffect(() => {
    let cancelled = false;
    Promise.all([userClient.getMe({}), myPageClient.getMyPage({})])
      .then(([me, data]) => {
        if (cancelled) return;
        setState({ kind: "ready", me, data });
        setMonthView({
          year: data.monthlyYear,
          month: data.monthlyMonth,
          monthly: data.monthly,
          loading: false,
        });
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
  const currentYM = { year: data.monthlyYear, month: data.monthlyMonth };

  const switchMonth = (diff: number) => {
    if (!monthView || monthView.loading) return;
    const next = shiftMonth({ year: monthView.year, month: monthView.month }, diff);
    setMonthView({ ...monthView, ...next, loading: true });
    myPageClient
      .listMonthlyTickets(next)
      .then((res) => {
        setMonthView({
          year: res.year,
          month: res.month,
          monthly: res.monthly,
          loading: false,
        });
      })
      .catch((err: unknown) => {
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/");
          return;
        }
        // 失敗時は読み込み中を解除して直前の表示を維持する。
        setMonthView((prev) => (prev ? { ...prev, loading: false } : prev));
      });
  };

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

        {monthView && (
          <Section>
            <MonthlyHeader
              monthView={monthView}
              currentYM={currentYM}
              onPrev={() => switchMonth(-1)}
              onNext={() => switchMonth(1)}
            />
            {monthView.monthly.length === 0 ? (
              <p className="mt-3 text-sm text-zinc-500">
                {monthView.year === currentYM.year && monthView.month === currentYM.month
                  ? "今月参加した公演はまだありません。"
                  : "この月に参加した公演はありません。"}
              </p>
            ) : (
              <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
                {monthView.monthly.map((a) => (
                  <MonthlyRow key={a.ticketId} ticket={a} />
                ))}
              </ul>
            )}
          </Section>
        )}
      </PageShell>
    </>
  );
}

function MonthlyHeader({
  monthView,
  currentYM,
  onPrev,
  onNext,
}: {
  monthView: MonthView;
  currentYM: { year: number; month: number };
  onPrev: () => void;
  onNext: () => void;
}) {
  const atCurrent =
    monthView.year === currentYM.year && monthView.month === currentYM.month;
  const navButtonClass =
    "inline-flex h-9 w-9 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-100 hover:text-zinc-900 disabled:cursor-not-allowed disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-zinc-500";
  return (
    <div className="flex items-baseline justify-between">
      <div className="flex items-center gap-1">
        <button
          type="button"
          aria-label="前の月"
          onClick={onPrev}
          disabled={monthView.loading}
          className={navButtonClass}
        >
          <ChevronIcon direction="left" />
        </button>
        <h2 className="text-sm font-semibold tracking-wider text-zinc-700 uppercase">
          <Mono>{monthView.year}</Mono> 年 <Mono>{monthView.month}</Mono> 月の履歴
        </h2>
        <button
          type="button"
          aria-label="次の月"
          onClick={onNext}
          disabled={monthView.loading || atCurrent}
          className={navButtonClass}
        >
          <ChevronIcon direction="right" />
        </button>
      </div>
      <span className="font-mono text-xs tabular-nums text-zinc-500">
        {monthView.loading ? "…" : `${monthView.monthly.length} 件`}
      </span>
    </div>
  );
}

function ChevronIcon({ direction }: { direction: "left" | "right" }) {
  return (
    <svg
      aria-hidden
      viewBox="0 0 20 20"
      fill="currentColor"
      className="size-4"
    >
      <path
        fillRule="evenodd"
        d={
          direction === "left"
            ? "M12.707 4.293a1 1 0 0 1 0 1.414L8.414 10l4.293 4.293a1 1 0 1 1-1.414 1.414l-5-5a1 1 0 0 1 0-1.414l5-5a1 1 0 0 1 1.414 0Z"
            : "M7.293 4.293a1 1 0 0 1 1.414 0l5 5a1 1 0 0 1 0 1.414l-5 5a1 1 0 0 1-1.414-1.414L11.586 10 7.293 5.707a1 1 0 0 1 0-1.414Z"
        }
        clipRule="evenodd"
      />
    </svg>
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
