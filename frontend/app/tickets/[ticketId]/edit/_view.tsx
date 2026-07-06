"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";

import type { Event as NazobuEvent } from "@/app/gen/nazobu/v1/event_pb";
import type {
  Ticket,
  TicketParticipant,
} from "@/app/gen/nazobu/v1/ticket_pb";
import { eventClient, ticketClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  PrimaryButton,
  Section,
  SectionTitle,
} from "@/app/_components";
import {
  joinJSTDateTime,
  parseDateTime,
  toDateInputValue,
  toTimeInputValue,
} from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "forbidden" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      ticket: Ticket;
      participants: TicketParticipant[];
      event: NazobuEvent;
      siblingTicketCount: number;
    };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function TicketEditView({ ticketId }: { ticketId: string }) {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const ticketRes = await ticketClient.getTicket({ ticketId });
        if (cancelled) return;
        if (!ticketRes.canEdit) {
          setLoad({ kind: "forbidden" });
          return;
        }
        if (!ticketRes.ticket) {
          setLoad({ kind: "not_found" });
          return;
        }
        const eventRes = await eventClient.getEvent({
          eventId: ticketRes.ticket.eventId,
        });
        if (cancelled) return;
        if (!eventRes.event) {
          setLoad({ kind: "not_found" });
          return;
        }
        const siblingTicketCount = eventRes.event.tickets.length;
        setLoad({
          kind: "ready",
          ticket: ticketRes.ticket,
          participants: ticketRes.participants,
          event: eventRes.event,
          siblingTicketCount,
        });
      } catch (err: unknown) {
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
      }
    })();
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
      event={load.event}
      siblingTicketCount={load.siblingTicketCount}
    />
  );
}

function Form({
  router,
  ticket,
  participants,
  event,
  siblingTicketCount,
}: {
  router: ReturnType<typeof useRouter>;
  ticket: Ticket;
  participants: TicketParticipant[];
  event: NazobuEvent;
  siblingTicketCount: number;
}) {
  // 公演（event）
  const [title, setTitle] = useState(event.title);
  const [url, setUrl] = useState(event.url);
  const [catchphrase, setCatchphrase] = useState(event.catchphrase);
  const [doorsOpen, setDoorsOpen] = useState(
    event.doorsOpenMinutesBefore !== undefined
      ? String(event.doorsOpenMinutesBefore)
      : "",
  );
  const [entryDeadline, setEntryDeadline] = useState(
    event.entryDeadlineMinutesBefore !== undefined
      ? String(event.entryDeadlineMinutesBefore)
      : "",
  );
  const [expectedDuration, setExpectedDuration] = useState(
    String(event.expectedDurationMinutes),
  );

  // チケット
  const startAtDate = parseDateTime(ticket.startAt);
  const meetingAtDate =
    ticket.meetingAt !== "" ? parseDateTime(ticket.meetingAt) : null;
  const currentPurchaserId =
    participants.find((p) => p.isPurchaser)?.userId ?? "";
  const [startDate, setStartDate] = useState(toDateInputValue(startAtDate));
  const [startTime, setStartTime] = useState(toTimeInputValue(startAtDate));
  const [meetingTime, setMeetingTime] = useState(
    meetingAtDate !== null ? toTimeInputValue(meetingAtDate) : "",
  );
  const [meetingPlace, setMeetingPlace] = useState(ticket.meetingPlace);
  const [pricePerPerson, setPricePerPerson] = useState(
    String(ticket.pricePerPerson),
  );
  const [maxParticipants, setMaxParticipants] = useState(
    String(ticket.maxParticipants),
  );
  const [unregisteredCount, setUnregisteredCount] = useState(
    ticket.unregisteredParticipantsCount > 0
      ? String(ticket.unregisteredParticipantsCount)
      : "",
  );
  const [purchasedByUserId, setPurchasedByUserId] =
    useState(currentPurchaserId);

  const [state, setState] = useState<SubmitState>({ kind: "idle" });

  const participantCount = participants.length;
  const submitting = state.kind === "submitting";
  const hasSiblings = siblingTicketCount > 1;

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    // 公演
    const trimmedTitle = title.trim();
    const trimmedUrl = url.trim();
    const trimmedCatchphrase = catchphrase.trim();
    if (trimmedTitle === "" || trimmedUrl === "") {
      setState({ kind: "error", message: "公演のタイトルと URL を入力してください" });
      return;
    }
    if (trimmedCatchphrase.length > 255) {
      setState({ kind: "error", message: "キャッチコピーは 255 文字以内で入力してください" });
      return;
    }
    const parsedDoorsOpen = parseOptionalNonNegativeInt(doorsOpen);
    if (parsedDoorsOpen === "invalid") {
      setState({ kind: "error", message: "開場は 0 以上の整数で入力してください" });
      return;
    }
    const parsedEntryDeadline = parseOptionalNonNegativeInt(entryDeadline);
    if (parsedEntryDeadline === "invalid") {
      setState({ kind: "error", message: "入場締切は 0 以上の整数で入力してください" });
      return;
    }
    const parsedExpectedDuration = parsePositiveInt(expectedDuration);
    if (parsedExpectedDuration === "invalid") {
      setState({ kind: "error", message: "想定所要時間は 1 以上の整数で入力してください" });
      return;
    }

    // チケット
    const trimmedPlace = meetingPlace.trim();
    const priceNum = Number(pricePerPerson);
    const maxNum = Number(maxParticipants);
    if (
      startDate === "" ||
      startTime === "" ||
      pricePerPerson === "" ||
      maxParticipants === "" ||
      purchasedByUserId === ""
    ) {
      setState({ kind: "error", message: "未入力の項目があります" });
      return;
    }
    if (!Number.isFinite(priceNum) || priceNum < 0 || !Number.isInteger(priceNum)) {
      setState({ kind: "error", message: "金額は 0 以上の整数で入力してください" });
      return;
    }
    if (!Number.isFinite(maxNum) || maxNum < 1 || !Number.isInteger(maxNum)) {
      setState({ kind: "error", message: "定員は 1 以上の整数で入力してください" });
      return;
    }
    const parsedUnregistered = parseOptionalNonNegativeInt(unregisteredCount);
    if (parsedUnregistered === "invalid") {
      setState({ kind: "error", message: "未登録の同行者は 0 以上の整数で入力してください" });
      return;
    }
    const unregisteredNum = parsedUnregistered ?? 0;
    if (maxNum < participantCount + unregisteredNum) {
      setState({
        kind: "error",
        message: `定員は参加者と未登録の同行者の合計人数（${participantCount + unregisteredNum} 人）以上で入力してください`,
      });
      return;
    }

    const startAt = joinJSTDateTime(startDate, startTime);
    const meetingAt = joinJSTDateTime(startDate, meetingTime) ?? "";
    if (startAt === null) {
      setState({ kind: "error", message: "未入力の項目があります" });
      return;
    }

    setState({ kind: "submitting" });
    try {
      await ticketClient.updateTicketWithEvent({
        ticketId: ticket.id,
        eventTitle: trimmedTitle,
        eventUrl: trimmedUrl,
        eventCatchphrase: trimmedCatchphrase,
        eventDoorsOpenMinutesBefore: parsedDoorsOpen,
        eventEntryDeadlineMinutesBefore: parsedEntryDeadline,
        eventExpectedDurationMinutes: parsedExpectedDuration,
        startAt,
        meetingAt,
        meetingPlace: trimmedPlace,
        pricePerPerson: priceNum,
        maxParticipants: maxNum,
        purchasedByUserId,
        unregisteredParticipantsCount: unregisteredNum,
      });
      router.push(`/tickets/${ticket.id}`);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/tickets/${ticket.id}/edit`);
        return;
      }
      const message =
        err instanceof Error ? err.message : "更新に失敗しました";
      setState({ kind: "error", message });
    }
  };

  return (
    <>
      <AppHeader brand="謎部" user="" />
      <PageShell>
        <Section>
          <SectionTitle>公演と参加チケットを編集</SectionTitle>

          {hasSiblings && (
            <p className="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
              この公演には他に {siblingTicketCount - 1} 件のチケットがあります。公演情報の変更は全チケットに反映されます。
            </p>
          )}

          <form onSubmit={onSubmit} className="mt-3 space-y-6">
            <fieldset className="space-y-4">
              <legend className="text-xs font-semibold tracking-wider text-zinc-500 uppercase">
                公演
              </legend>

              <Field label="タイトル" htmlFor="event-title">
                <input
                  id="event-title"
                  type="text"
                  required
                  maxLength={255}
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
              </Field>

              <Field label="URL" htmlFor="event-url">
                <input
                  id="event-url"
                  type="url"
                  required
                  maxLength={512}
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
                <p className="mt-1 text-xs text-zinc-500">
                  <code className="font-mono">realdgame.jp</code> /{" "}
                  <code className="font-mono">escape.id</code> の URL を入れると、自動でカード画像も取得します。
                </p>
              </Field>

              <Field label="キャッチコピー（任意）" htmlFor="event-catchphrase">
                <input
                  id="event-catchphrase"
                  type="text"
                  maxLength={255}
                  value={catchphrase}
                  onChange={(e) => setCatchphrase(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
                <p className="mt-1 text-xs text-zinc-500">
                  空欄のときは <code className="font-mono">escape.id</code> の og:description から自動取得します。
                </p>
              </Field>

              <Field label="開場（任意・開始の何分前か）" htmlFor="event-doors-open">
                <div className="flex items-center gap-2">
                  <input
                    id="event-doors-open"
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    value={doorsOpen}
                    onChange={(e) => setDoorsOpen(e.target.value)}
                    disabled={submitting}
                    className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  />
                  <span className="text-sm text-zinc-600">分前</span>
                </div>
              </Field>

              <Field label="入場締切（任意・開始の何分前か）" htmlFor="event-entry-deadline">
                <div className="flex items-center gap-2">
                  <input
                    id="event-entry-deadline"
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    value={entryDeadline}
                    onChange={(e) => setEntryDeadline(e.target.value)}
                    disabled={submitting}
                    className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  />
                  <span className="text-sm text-zinc-600">分前</span>
                </div>
                <p className="mt-1 text-xs text-zinc-500">
                  これを過ぎると参加できなくなる時刻。
                </p>
              </Field>

              <Field label="想定所要時間" htmlFor="event-expected-duration">
                <div className="flex items-center gap-2">
                  <input
                    id="event-expected-duration"
                    type="number"
                    required
                    min={1}
                    step={1}
                    inputMode="numeric"
                    value={expectedDuration}
                    onChange={(e) => setExpectedDuration(e.target.value)}
                    disabled={submitting}
                    className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  />
                  <span className="text-sm text-zinc-600">分</span>
                </div>
              </Field>
            </fieldset>

            <fieldset className="space-y-4 border-t border-zinc-200 pt-6">
              <legend className="text-xs font-semibold tracking-wider text-zinc-500 uppercase">
                参加チケット
              </legend>

              <Field label="参加日" htmlFor="ticket-start-date">
                <input
                  id="ticket-start-date"
                  type="date"
                  required
                  value={startDate}
                  onChange={(e) => setStartDate(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
              </Field>

              <Field label="開演時刻" htmlFor="ticket-start-time">
                <input
                  id="ticket-start-time"
                  type="time"
                  required
                  value={startTime}
                  onChange={(e) => setStartTime(e.target.value)}
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
                  集合時刻が決まっている場合のみ入力してください。参加日と同じ日として登録されます。
                </p>
              </Field>

              <Field label="集合場所（任意）" htmlFor="ticket-meeting-place">
                <input
                  id="ticket-meeting-place"
                  type="text"
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
                />
              </Field>

              <Field label="定員（このチケットで参加できる最大人数）" htmlFor="ticket-max-participants">
                <input
                  id="ticket-max-participants"
                  type="number"
                  required
                  min={Math.max(1, participantCount)}
                  step={1}
                  inputMode="numeric"
                  value={maxParticipants}
                  onChange={(e) => setMaxParticipants(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                />
                <p className="mt-1 text-xs text-zinc-500">
                  現在の参加者数: {participantCount} 人
                </p>
              </Field>

              <Field label="未登録の同行者（任意・人数）" htmlFor="ticket-unregistered-count">
                <div className="flex items-center gap-2">
                  <input
                    id="ticket-unregistered-count"
                    type="number"
                    min={0}
                    step={1}
                    inputMode="numeric"
                    value={unregisteredCount}
                    onChange={(e) => setUnregisteredCount(e.target.value)}
                    disabled={submitting}
                    className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                    placeholder="例: 1"
                  />
                  <span className="text-sm text-zinc-600">人</span>
                </div>
                <p className="mt-1 text-xs text-zinc-500">
                  謎部に未登録の人が一緒に参加する場合の人数。参加者と合わせて定員の枠を使います。
                </p>
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
            </fieldset>

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
  children: ReactNode;
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

function parseOptionalNonNegativeInt(raw: string): number | undefined | "invalid" {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n) || n < 0) return "invalid";
  return n;
}

function parsePositiveInt(raw: string): number | "invalid" {
  const trimmed = raw.trim();
  if (trimmed === "") return "invalid";
  const n = Number(trimmed);
  if (!Number.isFinite(n) || !Number.isInteger(n) || n < 1) return "invalid";
  return n;
}
