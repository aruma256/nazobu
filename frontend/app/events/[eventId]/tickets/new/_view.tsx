"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent } from "react";

import type { Event as NazobuEvent } from "@/app/gen/nazobu/v1/event_pb";
import type { User } from "@/app/gen/nazobu/v1/user_pb";
import { eventClient, ticketClient, userClient } from "@/app/lib/rpc";

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
  | { kind: "error"; message: string }
  | { kind: "ready"; event: NazobuEvent; users: User[] };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function NewTicketForEventView({ eventId }: { eventId: string }) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    Promise.all([eventClient.listEvents({}), userClient.listUsers({})])
      .then(([ev, us]) => {
        if (cancelled) return;
        const found = ev.events.find((e) => e.id === eventId);
        if (!found) {
          setLoad({ kind: "not_found" });
          return;
        }
        setLoad({ kind: "ready", event: found, users: us.users });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, `/events/${eventId}/tickets/new`);
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [eventId, router]);

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
            <p>指定された公演が見つかりませんでした。</p>
            <Link
              href="/events"
              className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
            >
              公演一覧に戻る
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

  return <Form router={router} event={load.event} users={load.users} />;
}

function Form({
  router,
  event,
  users,
}: {
  router: ReturnType<typeof useRouter>;
  event: NazobuEvent;
  users: User[];
}) {
  const [attendedOn, setAttendedOn] = useState("");
  const [meetingTime, setMeetingTime] = useState("");
  const [startTime, setStartTime] = useState("");
  const [meetingPlace, setMeetingPlace] = useState("");
  const [pricePerPerson, setPricePerPerson] = useState("");
  const [participantIds, setParticipantIds] = useState<string[]>([]);
  const [state, setState] = useState<SubmitState>({ kind: "idle" });

  const submitting = state.kind === "submitting";

  const toggleParticipant = (id: string) => {
    setParticipantIds((prev) =>
      prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id],
    );
  };

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    const trimmedPlace = meetingPlace.trim();
    const priceNum = Number(pricePerPerson);
    if (
      attendedOn === "" ||
      trimmedPlace === "" ||
      pricePerPerson === ""
    ) {
      setState({ kind: "error", message: "未入力の項目があります" });
      return;
    }
    if (!Number.isFinite(priceNum) || priceNum < 0 || !Number.isInteger(priceNum)) {
      setState({ kind: "error", message: "金額は 0 以上の整数で入力してください" });
      return;
    }
    if (participantIds.length === 0) {
      setState({ kind: "error", message: "参加者を 1 人以上選択してください" });
      return;
    }

    setState({ kind: "submitting" });
    try {
      await ticketClient.createTicket({
        eventId: event.id,
        attendedOn,
        meetingTime,
        startTime,
        meetingPlace: trimmedPlace,
        pricePerPerson: priceNum,
        participantUserIds: participantIds,
      });
      router.push("/tickets");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/events/${event.id}/tickets/new`);
        return;
      }
      const message =
        err instanceof Error ? err.message : "チケットの登録に失敗しました";
      setState({ kind: "error", message });
    }
  };

  return (
    <>
      <AppHeader brand="謎部" user="" />
      <PageShell>
        <Section>
          <SectionTitle>チケットを登録</SectionTitle>

          <p className="mt-3 text-sm text-zinc-600">
            <span className="text-zinc-400">公演</span>{" "}
            <span className="font-semibold text-zinc-900">{event.title}</span>
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
                placeholder="例: 3500"
              />
            </Field>

            <fieldset className="space-y-2">
              <legend className="block text-sm font-medium text-zinc-700">
                参加者
              </legend>
              <p className="text-xs text-zinc-500">
                立替者（あなた）を含めても構いません。1 人以上選択してください。
              </p>
              <ul className="divide-y divide-zinc-200 overflow-hidden rounded-lg border border-zinc-200 bg-white">
                {users.map((u) => {
                  const checked = participantIds.includes(u.id);
                  const label = u.displayName;
                  return (
                    <li key={u.id}>
                      <label className="flex h-11 cursor-pointer items-center gap-3 px-3 text-base text-zinc-900 hover:bg-zinc-50">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleParticipant(u.id)}
                          disabled={submitting}
                          className="size-4 rounded border-zinc-300 text-emerald-700 focus:ring-emerald-600"
                        />
                        <span>{label}</span>
                      </label>
                    </li>
                  );
                })}
              </ul>
            </fieldset>

            {state.kind === "error" && (
              <p className="text-sm text-amber-800">{state.message}</p>
            )}

            <div className="space-y-3 pt-2">
              <PrimaryButton type="submit" disabled={submitting}>
                {submitting ? "登録中…" : "登録する"}
              </PrimaryButton>
              <Link
                href="/events"
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
