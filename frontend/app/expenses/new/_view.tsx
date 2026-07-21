"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

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

import { ExpenseForm } from "../_form";
import type { ExpenseFormInitialParticipant } from "../_form";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      users: User[];
      tickets: Ticket[];
      // query の ticketId から引き当てた参加者（自分は除く）。
      initialParticipants: ExpenseFormInitialParticipant[];
    };

export function NewExpenseView({
  initialTicketId,
}: {
  initialTicketId: string;
}) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  const currentPath =
    initialTicketId !== ""
      ? `/expenses/new?ticketId=${initialTicketId}`
      : "/expenses/new";

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [me, usersRes, ticketsRes] = await Promise.all([
          userClient.getMe({}),
          userClient.listUsers({}),
          ticketClient.listTickets({}),
        ]);
        // query で ticketId が渡っていれば、そのチケットの参加者を初期選択する。
        let initialParticipants: ExpenseFormInitialParticipant[] = [];
        if (initialTicketId !== "") {
          const ticketRes = await ticketClient.getTicket({
            ticketId: initialTicketId,
          });
          initialParticipants = ticketRes.participants
            .filter((p) => p.userId !== me.id)
            .map((p) => ({ userId: p.userId, amount: 0, settled: false }));
        }
        if (cancelled) return;
        setLoad({
          kind: "ready",
          me,
          users: usersRes.users,
          tickets: ticketsRes.tickets,
          initialParticipants,
        });
      } catch (err: unknown) {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, currentPath);
          return;
        }
        // 紐付け対象の ticketId が存在しない場合でも、紐付けなしで登録できるように
        // 初期参加者だけ諦めて続行する余地はあるが、ここでは素直にエラー表示する。
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [router, initialTicketId, currentPath]);

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

  const { me, users, tickets, initialParticipants } = load;

  return (
    <>
      <AppHeader
        brand="謎部"
        user={me.displayName}
        isAdmin={me.role === "admin"}
      />
      <PageShell>
        <Section>
          <SectionTitle>精算を登録</SectionTitle>
          <p className="mt-2 text-xs text-zinc-500">
            立替者はあなたになります。あなた以外の参加者と負担額を登録してください。
          </p>
          <ExpenseForm
            mode="create"
            users={users}
            tickets={tickets}
            myUserId={me.id}
            initialTicketId={initialTicketId}
            initialParticipants={initialParticipants}
            submitLabel="登録する"
            submittingLabel="登録中…"
            cancelHref="/expenses"
            onSubmit={async (data) => {
              try {
                const res = await expenseClient.createExpense({
                  ticketId: data.ticketId,
                  title: data.title,
                  occurredOn: data.occurredOn,
                  participants: data.participants,
                });
                const newId = res.expense?.id ?? "";
                router.push(newId !== "" ? `/expenses/${newId}` : "/expenses");
              } catch (err) {
                if (
                  err instanceof ConnectError &&
                  err.code === Code.Unauthenticated
                ) {
                  redirectToLogin(router, currentPath);
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
