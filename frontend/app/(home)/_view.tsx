"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import type {
  GetMyPageResponse,
  MonthlyTicket,
} from "@/app/gen/nazobu/v1/mypage_pb";
import { myPageClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  Badge,
  Mono,
  PageShell,
  Section,
  SectionTitle,
  TicketCard,
} from "@/app/_components";
import {
  formatMonoDate,
  parseDateTime,
} from "@/app/_format";
import { canOrganize, redirectToLogin } from "@/app/lib/auth";

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

type CopyState = "idle" | "copied" | "failed";

export function HomeView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [monthView, setMonthView] = useState<MonthView | null>(null);
  const [copyState, setCopyState] = useState<CopyState>("idle");

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
  const canManageEvents = canOrganize(me.role);
  const currentYM = { year: data.currentYear, month: data.currentMonth };

  const switchMonth = (diff: number) => {
    if (!monthView || monthView.loading) return;
    setCopyState("idle");
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

  const copyTitles = async () => {
    if (!monthView) return;
    const text = monthView.monthly.map((t) => t.eventTitle).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
    window.setTimeout(() => setCopyState("idle"), 2000);
  };

  return (
    <>
      <AppHeader brand="謎部" user={displayName} canManageEvents={canManageEvents} />
      <PageShell>
        {data.unsettled.length > 0 && (
          <Section>
            <SectionTitle count={data.unsettled.length}>未精算</SectionTitle>
            <ul className="mt-3 space-y-3">
              {data.unsettled.map((t) => (
                <TicketCard
                  key={t.id}
                  ticket={t}
                  myName={displayName}
                  tone="alert"
                />
              ))}
            </ul>
          </Section>
        )}

        <Section>
          <SectionTitle count={data.upcoming.length}>あなたの今後の予定</SectionTitle>
          {data.upcoming.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              参加予定の公演はありません。
            </p>
          ) : (
            <ul className="mt-3 space-y-3">
              {data.upcoming.map((t) => (
                <TicketCard key={t.id} ticket={t} myName={displayName} />
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
              <>
                <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
                  {monthView.monthly.map((a) => (
                    <MonthlyRow key={a.ticketId} ticket={a} />
                  ))}
                </ul>
                <div className="mt-2 flex justify-end">
                  <CopyTitlesButton state={copyState} onClick={copyTitles} />
                </div>
              </>
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
          あなたの <Mono>{monthView.year}</Mono> 年 <Mono>{monthView.month}</Mono> 月の履歴
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

function CopyTitlesButton({
  state,
  onClick,
}: {
  state: CopyState;
  onClick: () => void;
}) {
  const label =
    state === "copied"
      ? "コピーしました"
      : state === "failed"
        ? "コピーに失敗しました"
        : "公演名をコピー";
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-zinc-700 transition-colors hover:bg-zinc-50"
    >
      {state === "copied" ? <CheckIcon /> : <CopyIcon />}
      {label}
    </button>
  );
}

function CopyIcon() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 20 20"
      fill="currentColor"
      className="size-4 text-zinc-500"
    >
      <path d="M7 3.5A1.5 1.5 0 0 1 8.5 2h6A1.5 1.5 0 0 1 16 3.5v9a1.5 1.5 0 0 1-1.5 1.5H13v-1h1.5a.5.5 0 0 0 .5-.5v-9a.5.5 0 0 0-.5-.5h-6a.5.5 0 0 0-.5.5V5H7V3.5Z" />
      <path d="M4 7.5A1.5 1.5 0 0 1 5.5 6h6A1.5 1.5 0 0 1 13 7.5v9a1.5 1.5 0 0 1-1.5 1.5h-6A1.5 1.5 0 0 1 4 16.5v-9Zm1.5-.5a.5.5 0 0 0-.5.5v9a.5.5 0 0 0 .5.5h6a.5.5 0 0 0 .5-.5v-9a.5.5 0 0 0-.5-.5h-6Z" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 20 20"
      fill="currentColor"
      className="size-4 text-emerald-700"
    >
      <path
        fillRule="evenodd"
        d="M16.704 5.29a1 1 0 0 1 .006 1.414l-7.5 7.6a1 1 0 0 1-1.42.005l-3.5-3.5a1 1 0 1 1 1.414-1.414l2.79 2.79 6.793-6.881a1 1 0 0 1 1.417-.014Z"
        clipRule="evenodd"
      />
    </svg>
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
