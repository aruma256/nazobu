"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import type {
  ExpenseParticipant,
  GetExpenseResponse,
} from "@/app/gen/nazobu/v1/expense_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { expenseClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  Badge,
  Mono,
  PageShell,
  Section,
  SectionTitle,
} from "@/app/_components";
import { formatDateJa, formatYen, parseDateOnly } from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      detail: GetExpenseResponse;
    };

export function ExpenseDetailView({ expenseId }: { expenseId: string }) {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [mutating, setMutating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const [me, detail] = await Promise.all([
        userClient.getMe({}),
        expenseClient.getExpense({ expenseId }),
      ]);
      setState({ kind: "ready", me, detail });
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/expenses/${expenseId}`);
        return;
      }
      if (err instanceof ConnectError && err.code === Code.NotFound) {
        setState({ kind: "not_found" });
        return;
      }
      const message =
        err instanceof Error ? err.message : "データの取得に失敗しました";
      setState({ kind: "error", message });
    }
  }, [router, expenseId]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (cancelled) return;
      await reload();
    })();
    return () => {
      cancelled = true;
    };
  }, [reload]);

  const handleMutation = useCallback(
    async (op: () => Promise<unknown>) => {
      if (mutating) return;
      setMutating(true);
      setError(null);
      try {
        await op();
        await reload();
      } catch (err) {
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, `/expenses/${expenseId}`);
          return;
        }
        const message = err instanceof Error ? err.message : "更新に失敗しました";
        setError(message);
      } finally {
        setMutating(false);
      }
    },
    [mutating, reload, router, expenseId],
  );

  const handleDelete = useCallback(async () => {
    if (mutating) return;
    if (
      !window.confirm("この精算を削除します。よろしいですか？（元に戻せません）")
    ) {
      return;
    }
    setMutating(true);
    setError(null);
    try {
      await expenseClient.deleteExpense({ expenseId });
      router.push("/expenses");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/expenses/${expenseId}`);
        return;
      }
      const message = err instanceof Error ? err.message : "削除に失敗しました";
      setError(message);
      setMutating(false);
    }
  }, [mutating, router, expenseId]);

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

  if (state.kind === "not_found") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <div className="space-y-4 pt-8 text-sm text-zinc-700">
            <p>指定された精算が見つかりませんでした。</p>
            <Link
              href="/expenses"
              className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
            >
              精算一覧に戻る
            </Link>
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

  const { me, detail } = state;
  const expense = detail.expense;
  if (!expense) {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            精算情報の取得に失敗しました
          </p>
        </PageShell>
      </>
    );
  }

  const displayName = me.displayName;
  const isAdmin = me.role === "admin";
  const occurredOn = parseDateOnly(expense.occurredOn);
  const canEdit = detail.canEdit;

  return (
    <>
      <AppHeader brand="謎部" user={displayName} isAdmin={isAdmin} />
      <PageShell>
        <Section>
          <SectionTitle>精算の詳細</SectionTitle>

          <div className="mt-3 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
            <div className="flex items-baseline gap-3 px-4 pt-4">
              <Mono className="text-sm font-semibold text-emerald-700">
                {formatDateJa(occurredOn)}
              </Mono>
              <Mono className="ml-auto text-sm font-semibold tracking-tight">
                {formatYen(expense.totalAmount)}
              </Mono>
            </div>
            <h3 className="px-4 pt-1 text-base leading-snug font-semibold">
              {expense.title}
            </h3>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 px-4 pt-3 pb-4 text-xs text-zinc-900">
              {expense.ticketId !== "" && expense.eventTitle !== "" && (
                <>
                  <dt>公演</dt>
                  <dd>
                    <Link
                      href={`/tickets/${expense.ticketId}`}
                      className="text-emerald-700 underline decoration-zinc-300 underline-offset-4 hover:decoration-emerald-700"
                    >
                      {expense.eventTitle}
                    </Link>
                  </dd>
                </>
              )}
              <dt>立替</dt>
              <dd>{expense.payerName}</dd>
              <dt>精算</dt>
              <dd>
                <Mono>
                  {expense.settledCount}
                  <span className="text-zinc-500"> / </span>
                  {expense.participantCount}
                </Mono>
                <span className="text-zinc-500"> 人</span>
              </dd>
            </dl>

            {canEdit && (
              <div className="flex flex-wrap gap-2 border-t border-zinc-200 px-4 py-3">
                <Link
                  href={`/expenses/${expense.id}/edit`}
                  className="inline-flex h-10 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50"
                >
                  編集
                </Link>
                <button
                  type="button"
                  onClick={handleDelete}
                  disabled={mutating}
                  className="inline-flex h-10 items-center justify-center rounded-lg border border-amber-200 bg-white px-4 text-sm font-semibold text-amber-800 hover:bg-amber-50 disabled:opacity-50"
                >
                  削除
                </button>
              </div>
            )}
          </div>
        </Section>

        <ParticipantsSection
          participants={detail.participants}
          myUserId={me.id}
          canEdit={canEdit}
          mutating={mutating}
          onToggleSettlement={(userId, settled) =>
            handleMutation(() =>
              expenseClient.updateExpenseParticipantSettlement({
                expenseId: expense.id,
                userId,
                settled,
              }),
            )
          }
        />

        {error !== null && (
          <Section>
            <p className="text-sm text-amber-800">エラー: {error}</p>
          </Section>
        )}

        <Section>
          <Link
            href="/expenses"
            className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
          >
            精算一覧に戻る
          </Link>
        </Section>
      </PageShell>
    </>
  );
}

function ParticipantsSection({
  participants,
  myUserId,
  canEdit,
  mutating,
  onToggleSettlement,
}: {
  participants: ExpenseParticipant[];
  myUserId: string;
  canEdit: boolean;
  mutating: boolean;
  onToggleSettlement: (userId: string, settled: boolean) => Promise<void>;
}) {
  const sortedParticipants = useMemo(
    () =>
      [...participants].sort((a, b) => {
        if (a.userId === myUserId) return -1;
        if (b.userId === myUserId) return 1;
        return a.name.localeCompare(b.name, "ja");
      }),
    [participants, myUserId],
  );

  return (
    <Section>
      <SectionTitle count={participants.length}>参加者</SectionTitle>

      {participants.length === 0 ? (
        <p className="mt-3 text-sm text-zinc-500">参加者がいません。</p>
      ) : (
        <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
          {sortedParticipants.map((p) => (
            <li key={p.userId} className="flex items-center gap-3 px-4 py-3">
              <span
                className={
                  p.userId === myUserId
                    ? "text-sm font-semibold text-emerald-700"
                    : "text-sm text-zinc-900"
                }
              >
                {p.name}
              </span>
              <Mono className="text-sm tracking-tight text-zinc-700">
                {formatYen(p.amount)}
              </Mono>
              {p.settled ? (
                <Badge tone="settled">精算済</Badge>
              ) : (
                <Badge tone="unsettled">未精算</Badge>
              )}
              {canEdit && (
                <div className="ml-auto flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => onToggleSettlement(p.userId, !p.settled)}
                    disabled={mutating}
                    className="inline-flex h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
                  >
                    {p.settled ? "未精算に戻す" : "精算済にする"}
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
