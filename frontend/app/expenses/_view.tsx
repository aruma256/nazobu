"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Expense } from "@/app/gen/nazobu/v1/expense_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { expenseClient, myPageClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  ExpenseCard,
  PageShell,
  Section,
  SectionTitle,
  UnsettledBanner,
} from "@/app/_components";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      expenses: Expense[];
      unsettledCount: number;
      receivablesCount: number;
    };

export function ExpensesView() {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      userClient.getMe({}),
      expenseClient.listExpenses({}),
      myPageClient.listMyUnsettledTickets({}),
      myPageClient.listMyUnsettledReceivables({}),
    ])
      .then(([me, res, unsettled, receivables]) => {
        if (!cancelled)
          setState({
            kind: "ready",
            me,
            expenses: res.expenses,
            unsettledCount: unsettled.tickets.length,
            receivablesCount: receivables.tickets.length,
          });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/expenses");
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

  const { me, expenses, unsettledCount, receivablesCount } = state;
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
          <SectionTitle count={expenses.length}>追加の精算</SectionTitle>
          <p className="mt-2 text-xs text-zinc-500">
            チケット代以外の精算（公演後の飲み会・打ち上げなど）を管理します。
          </p>
          <div className="mt-3">
            <Link
              href="/expenses/new"
              className="inline-flex h-11 w-full items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900"
            >
              精算を登録
            </Link>
          </div>
          {expenses.length === 0 ? (
            <p className="mt-4 text-sm text-zinc-500">
              まだ精算が登録されていません。
            </p>
          ) : (
            <ul className="mt-4 space-y-3">
              {expenses.map((e) => (
                <ExpenseCard key={e.id} expense={e} myName={displayName} />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}
