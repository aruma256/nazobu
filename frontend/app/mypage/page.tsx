import type { Metadata } from "next";

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
  formatTimeJa,
  formatYen,
  sampleArchive,
  sampleSettlements,
  sampleUpcoming,
  sampleUser,
} from "./_sample";

export const metadata: Metadata = {
  title: "マイページ | 謎部",
};

// 比較用デモのため「今日」を固定する。バックエンド接続時に差し替える。
const TODAY = new Date("2026-04-29T12:00:00+09:00");

export default function MyPage() {
  return (
    <>
      <AppHeader brand="謎部" user={sampleUser.displayName} />
      <PageShell>
        {sampleSettlements.length > 0 && (
          <Section>
            <AlertCard title={`未精算 ${sampleSettlements.length} 件`}>
              {sampleSettlements.map((s) => {
                const days = daysFromToday(s.dueOn, TODAY);
                return (
                  <AlertItem key={s.id}>
                    <div className="flex items-baseline justify-between gap-3">
                      <p className="text-base font-medium">
                        {s.performanceTitle}
                      </p>
                      <Mono className="text-base font-semibold tracking-tight">
                        {formatYen(s.amount)}
                      </Mono>
                    </div>
                    <p className="mt-2 text-xs text-zinc-600">
                      立替: {s.payee}
                      <span className="mx-1.5 text-zinc-300">/</span>
                      <Mono>{formatMonoDate(s.dueOn)}</Mono> まで
                      {days >= 0 && (
                        <span className="ml-1.5 text-amber-700">
                          (あと {days} 日)
                        </span>
                      )}
                    </p>
                  </AlertItem>
                );
              })}
            </AlertCard>
          </Section>
        )}

        <Section>
          <SectionTitle count={sampleUpcoming.length}>今後の予定</SectionTitle>
          <ListCard>
            {sampleUpcoming.map((e) => {
              const days = daysFromToday(e.startsAt, TODAY);
              const dayLabel =
                days === 0 ? "本日" : days === 1 ? "明日" : `あと ${days} 日`;
              return (
                <li key={e.id} className="px-4 py-4">
                  <div className="flex items-baseline gap-3">
                    <Mono className="text-sm font-semibold text-emerald-700">
                      {formatDateJa(e.startsAt)}
                    </Mono>
                    <Mono className="text-sm text-zinc-600">
                      {formatTimeJa(e.startsAt)}
                    </Mono>
                    <span className="ml-auto text-xs text-zinc-500">
                      {dayLabel}
                    </span>
                  </div>
                  <p className="mt-1.5 text-base leading-snug font-medium">
                    {e.title}
                  </p>
                  <p className="mt-1 text-xs text-zinc-500">
                    {e.organizer}
                    <span className="mx-1.5 text-zinc-300">·</span>
                    {e.venue}
                  </p>
                  <p className="mt-2 text-xs text-zinc-600">
                    <span className="text-zinc-400">同行</span>{" "}
                    {e.members.join("・")}
                  </p>
                </li>
              );
            })}
          </ListCard>
        </Section>

        <Section>
          <SectionTitle count={sampleArchive.length}>4 月の履歴</SectionTitle>
          <ListCard>
            {sampleArchive.map((a) => (
              <li key={a.id} className="flex items-center gap-3 px-4 py-3">
                <Mono className="text-xs text-zinc-500">
                  {formatMonoDate(a.attendedOn)}
                </Mono>
                <span className="flex-1 truncate text-sm">{a.title}</span>
                <Badge tone={a.settled ? "settled" : "unsettled"}>
                  {a.settled ? "精算済み" : "未精算"}
                </Badge>
              </li>
            ))}
          </ListCard>
        </Section>
      </PageShell>
    </>
  );
}
