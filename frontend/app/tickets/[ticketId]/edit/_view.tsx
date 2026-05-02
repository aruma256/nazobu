"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";

import type {
  Ticket,
  TicketParticipant,
} from "@/app/gen/nazobu/v1/ticket_pb";
import { ticketClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  PrimaryButton,
  Section,
  SectionTitle,
} from "@/app/_components";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "forbidden" }
  | { kind: "error"; message: string }
  | { kind: "ready"; ticket: Ticket; participants: TicketParticipant[] };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function TicketEditView({ ticketId }: { ticketId: string }) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    ticketClient
      .getTicket({ ticketId })
      .then((res) => {
        if (cancelled) return;
        if (!res.canEdit) {
          setLoad({ kind: "forbidden" });
          return;
        }
        if (!res.ticket) {
          setLoad({ kind: "not_found" });
          return;
        }
        setLoad({
          kind: "ready",
          ticket: res.ticket,
          participants: res.participants,
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, `/tickets/${ticketId}/edit`);
          return;
        }
        if (err instanceof ConnectError && err.code === Code.NotFound) {
          setLoad({ kind: "not_found" });
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [ticketId, router]);

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
            <p>指定されたチケットが見つかりませんでした。</p>
            <Link
              href="/tickets"
              className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
            >
              チケット一覧に戻る
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
            <p>このチケットを編集する権限がありません。</p>
            <Link
              href={`/tickets/${ticketId}`}
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

  return (
    <Form
      router={router}
      ticket={load.ticket}
      participants={load.participants}
    />
  );
}

function Form({
  router,
  ticket,
  participants,
}: {
  router: ReturnType<typeof useRouter>;
  ticket: Ticket;
  participants: TicketParticipant[];
}) {
  const currentPurchaserId =
    participants.find((p) => p.isPurchaser)?.userId ?? "";
  const [attendedOn, setAttendedOn] = useState(ticket.attendedOn);
  const [meetingTime, setMeetingTime] = useState(ticket.meetingTime);
  const [startTime, setStartTime] = useState(ticket.startTime);
  const [meetingPlace, setMeetingPlace] = useState(ticket.meetingPlace);
  const [pricePerPerson, setPricePerPerson] = useState(
    String(ticket.pricePerPerson),
  );
  const [purchasedByUserId, setPurchasedByUserId] =
    useState(currentPurchaserId);
  const [state, setState] = useState<SubmitState>({ kind: "idle" });

  const submitting = state.kind === "submitting";

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    const trimmedPlace = meetingPlace.trim();
    const priceNum = Number(pricePerPerson);
    if (
      attendedOn === "" ||
      trimmedPlace === "" ||
      pricePerPerson === "" ||
      purchasedByUserId === ""
    ) {
      setState({ kind: "error", message: "未入力の項目があります" });
      return;
    }
    if (!Number.isFinite(priceNum) || priceNum < 0 || !Number.isInteger(priceNum)) {
      setState({ kind: "error", message: "金額は 0 以上の整数で入力してください" });
      return;
    }

    setState({ kind: "submitting" });
    try {
      await ticketClient.updateTicket({
        ticketId: ticket.id,
        attendedOn,
        meetingTime,
        startTime,
        meetingPlace: trimmedPlace,
        pricePerPerson: priceNum,
        purchasedByUserId,
      });
      router.push(`/tickets/${ticket.id}`);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/tickets/${ticket.id}/edit`);
        return;
      }
      const message =
        err instanceof Error ? err.message : "チケットの更新に失敗しました";
      setState({ kind: "error", message });
    }
  };

  return (
    <>
      <AppHeader brand="謎部" user="" />
      <PageShell>
        <Section>
          <SectionTitle>チケットを編集</SectionTitle>

          <p className="mt-3 text-sm text-zinc-600">
            <span className="text-zinc-400">公演</span>{" "}
            <span className="font-semibold text-zinc-900">
              {ticket.eventTitle}
            </span>
          </p>

          <form onSubmit={onSubmit} className="mt-3 space-y-4">
            <Field label="参加日" htmlFor="ticket-attended-on">
              <input
                id="ticket-attended-on"
                type="date"
                required
                value={attendedOn}
                onChange={(e) => setAttendedOn(e.target.value)}
                disabled={submitting}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
              />
            </Field>

            <Field label="集合時刻（任意）" htmlFor="ticket-meeting-time">
              <input
                id="ticket-meeting-time"
                type="time"
                value={meetingTime}
                onChange={(e) => setMeetingTime(e.target.value)}
                disabled={submitting}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
              />
              <p className="mt-1 text-xs text-zinc-500">
                集合時刻が決まっている場合のみ入力してください。
              </p>
            </Field>

            <Field label="開始時刻（任意）" htmlFor="ticket-start-time">
              <input
                id="ticket-start-time"
                type="time"
                value={startTime}
                onChange={(e) => setStartTime(e.target.value)}
                disabled={submitting}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
              />
              <p className="mt-1 text-xs text-zinc-500">
                何時の回かが決まっている公演のみ入力してください。
              </p>
            </Field>

            <Field label="集合場所" htmlFor="ticket-meeting-place">
              <input
                id="ticket-meeting-place"
                type="text"
                required
                maxLength={255}
                value={meetingPlace}
                onChange={(e) => setMeetingPlace(e.target.value)}
                disabled={submitting}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                placeholder="例: 〇〇駅 改札前"
              />
            </Field>

            <Field label="一人あたり金額（円）" htmlFor="ticket-price">
              <input
                id="ticket-price"
                type="number"
                required
                min={0}
                step={1}
                inputMode="numeric"
                value={pricePerPerson}
                onChange={(e) => setPricePerPerson(e.target.value)}
                disabled={submitting}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                placeholder="例: 4000"
              />
            </Field>

            <Field label="立替者" htmlFor="ticket-purchaser">
              <select
                id="ticket-purchaser"
                required
                value={purchasedByUserId}
                onChange={(e) => setPurchasedByUserId(e.target.value)}
                disabled={submitting || participants.length === 0}
                className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
              >
                {participants.length === 0 && (
                  <option value="">参加者がいません</option>
                )}
                {participants.map((p) => (
                  <option key={p.userId} value={p.userId}>
                    {p.name}
                  </option>
                ))}
              </select>
              <p className="mt-1 text-xs text-zinc-500">
                立替者は参加者の中から選びます。
              </p>
            </Field>

            {state.kind === "error" && (
              <p className="text-sm text-amber-800">{state.message}</p>
            )}

            <div className="space-y-3 pt-2">
              <PrimaryButton type="submit" disabled={submitting}>
                {submitting ? "更新中…" : "更新する"}
              </PrimaryButton>
              <Link
                href={`/tickets/${ticket.id}`}
                className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
              >
                キャンセル
              </Link>
            </div>
          </form>
        </Section>
      </PageShell>
    </>
  );
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="block text-sm font-medium text-zinc-700">
        {label}
      </label>
      <div className="mt-1">{children}</div>
    </div>
  );
}
