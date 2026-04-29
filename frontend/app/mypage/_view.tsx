"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useEffect, useState } from "react";

import type {
  GetMeResponse,
} from "@/app/gen/nazobu/v1/user_pb";
import type {
  GetMyPageResponse,
} from "@/app/gen/nazobu/v1/mypage_pb";
import { myPageClient, userClient } from "@/app/lib/rpc";

import {
  AlertCard,
  AlertItem,
  AppHeader,
  Badge,
  ListCard,
  Mono,
  PageShell,
  Section,
  SectionTitle,
} from "./_components";
import {
  daysFromToday,
  formatDateJa,
  formatMonoDate,
  formatYen,
  parseAttendedOn,
} from "./_format";

type LoadState =
  | { kind: "loading" }
  | { kind: "unauthenticated" }
  | { kind: "error"; message: string }
  | { kind: "ready"; me: GetMeResponse; data: GetMyPageResponse };

export function MyPageView() {
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
          <div className="pt-8 space-y-4 text-sm text-zinc-700">
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

  const { me, data } = state;
  const displayName = me.displayName !== "" ? me.displayName : me.username;
  const today = new Date();

  return (
    <>
      <AppHeader brand="謎部" user={displayName} />
      <PageShell>
        {data.unsettled.length > 0 && (
          <Section>
            <AlertCard title={`未精算 ${data.unsettled.length} 件`}>
              {data.unsettled.map((s) => (
                <AlertItem key={s.ticketId}>
                  <div className="flex items-baseline justify-between gap-3">
                    <p className="text-base font-medium">{s.eventTitle}</p>
                    <Mono className="text-base font-semibold tracking-tight">
                      {formatYen(s.pricePerPerson)}
                    </Mono>
                  </div>
                  <p className="mt-2 text-xs text-zinc-600">
                    立替: {s.payeeName}
                    <span className="mx-1.5 text-zinc-300">/</span>
                    <Mono>{formatMonoDate(parseAttendedOn(s.attendedOn))}</Mono>{" "}
                    参加分
                  </p>
                </AlertItem>
              ))}
            </AlertCard>
          </Section>
        )}

        <Section>
          <SectionTitle count={data.upcoming.length}>今後の予定</SectionTitle>
          {data.upcoming.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              参加予定の公演はありません。
            </p>
          ) : (
            <ListCard>
              {data.upcoming.map((e) => {
                const date = parseAttendedOn(e.attendedOn);
                const days = daysFromToday(date, today);
                const dayLabel =
                  days <= 0
                    ? "本日"
                    : days === 1
                      ? "明日"
                      : `あと ${days} 日`;
                return (
                  <li key={e.ticketId} className="px-4 py-4">
                    <div className="flex items-baseline gap-3">
                      <Mono className="text-sm font-semibold text-emerald-700">
                        {formatDateJa(date)}
                      </Mono>
                      <span className="ml-auto text-xs text-zinc-500">
                        {dayLabel}
                      </span>
                    </div>
                    <p className="mt-1.5 text-base leading-snug font-medium">
                      {e.eventUrl !== "" ? (
                        <a
                          href={e.eventUrl}
                          target="_blank"
                          rel="noreferrer noopener"
                          className="underline decoration-zinc-300 underline-offset-4 hover:decoration-zinc-500"
                        >
                          {e.eventTitle}
                        </a>
                      ) : (
                        e.eventTitle
                      )}
                    </p>
                    {e.companionNames.length > 0 && (
                      <p className="mt-2 text-xs text-zinc-600">
                        <span className="text-zinc-400">同行</span>{" "}
                        {e.companionNames.join("・")}
                      </p>
                    )}
                  </li>
                );
              })}
            </ListCard>
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
            <ListCard>
              {data.monthly.map((a) => (
                <li
                  key={a.ticketId}
                  className="flex items-center gap-3 px-4 py-3"
                >
                  <Mono className="text-xs text-zinc-500">
                    {formatMonoDate(parseAttendedOn(a.attendedOn))}
                  </Mono>
                  <span className="flex-1 truncate text-sm">
                    {a.eventTitle}
                  </span>
                  <Badge tone={a.settled ? "settled" : "unsettled"}>
                    {a.settled ? "精算済み" : "未精算"}
                  </Badge>
                </li>
              ))}
            </ListCard>
          )}
        </Section>
      </PageShell>
    </>
  );
}
