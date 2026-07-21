"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import type { Expense, ExpenseParticipant } from "@/app/gen/nazobu/v1/expense_pb";
import type { Ticket } from "@/app/gen/nazobu/v1/ticket_pb";
import type { GetMeResponse, User } from "@/app/gen/nazobu/v1/user_pb";
import { expenseClient, ticketClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  Section,
  SectionTitle,
} from "@/app/_components";
import { redirectToLogin } from "@/app/lib/auth";

import { ExpenseForm } from "../../_form";

type LoadState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "forbidden" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      expense: Expense;
      participants: ExpenseParticipant[];
      users: User[];
      tickets: Ticket[];
    };

export function ExpenseEditView({ expenseId }: { expenseId: string }) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [me, detail, usersRes, ticketsRes] = await Promise.all([
          userClient.getMe({}),
          expenseClient.getExpense({ expenseId }),
          userClient.listUsers({}),
          ticketClient.listTickets({}),
        ]);
        if (cancelled) return;
        if (!detail.canEdit) {
          setLoad({ kind: "forbidden" });
          return;
        }
        if (!detail.expense) {
          setLoad({ kind: "not_found" });
          return;
        }
        setLoad({
          kind: "ready",
          me,
          expense: detail.expense,
          participants: detail.participants,
          users: usersRes.users,
          tickets: ticketsRes.tickets,
        });
      } catch (err: unknown) {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, `/expenses/${expenseId}/edit`);
          return;
        }
        if (err instanceof ConnectError && err.code === Code.NotFound) {
          setLoad({ kind: "not_found" });
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [router, expenseId]);

  if (load.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }
  if (load.kind === "not_found") {
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
  if (load.kind === "forbidden") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <div className="space-y-4 pt-8 text-sm text-zinc-700">
            <p>この精算を編集する権限がありません。</p>
            <Link
              href={`/expenses/${expenseId}`}
              className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
            >
              詳細に戻る
            </Link>
          </div>
        </PageShell>
      </>
    );
  }
  if (load.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {load.message}
          </p>
        </PageShell>
      </>
    );
  }

  const { me, expense, participants, users, tickets } = load;

  return (
    <>
      <AppHeader
        brand="謎部"
        user={me.displayName}
        isAdmin={me.role === "admin"}
      />
      <PageShell>
        <Section>
          <SectionTitle>精算を編集</SectionTitle>
          <ExpenseForm
            mode="edit"
            users={users}
            tickets={tickets}
            myUserId={me.id}
            initialTitle={expense.title}
            initialOccurredOn={expense.occurredOn}
            initialTicketId={expense.ticketId}
            initialPaidByUserId={expense.paidByUserId}
            initialParticipants={participants.map((p) => ({
              userId: p.userId,
              amount: p.amount,
              settled: p.settled,
            }))}
            submitLabel="更新する"
            submittingLabel="更新中…"
            cancelHref={`/expenses/${expenseId}`}
            onSubmit={async (data) => {
              try {
                await expenseClient.updateExpense({
                  expenseId,
                  ticketId: data.ticketId,
                  title: data.title,
                  occurredOn: data.occurredOn,
                  paidByUserId: data.paidByUserId,
                  participants: data.participants,
                });
                router.push(`/expenses/${expenseId}`);
              } catch (err) {
                if (
                  err instanceof ConnectError &&
                  err.code === Code.Unauthenticated
                ) {
                  redirectToLogin(router, `/expenses/${expenseId}/edit`);
                  return;
                }
                throw err;
              }
            }}
          />
        </Section>
      </PageShell>
    </>
  );
}
