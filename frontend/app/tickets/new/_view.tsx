"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";

import type { User } from "@/app/gen/nazobu/v1/user_pb";
import { ticketClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  PageShell,
  PrimaryButton,
  Section,
  SectionTitle,
} from "@/app/_components";
import { joinJSTDateTime } from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; users: User[] };

type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

export function NewTicketView() {
  const router = useRouter();
  const [load, setLoad] = useState<LoadState>({ kind: "loading" });

  useEffect(() => {
    let cancelled = false;
    userClient
      .listUsers({})
      .then((res) => {
        if (cancelled) return;
        setLoad({ kind: "ready", users: res.users });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/tickets/new");
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setLoad({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

  if (load.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" isAdmin />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }
  if (load.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" isAdmin />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {load.message}
          </p>
        </PageShell>
      </>
    );
  }

  return <Form router={router} users={load.users} />;
}

function Form({
  router,
  users,
}: {
  router: ReturnType<typeof useRouter>;
  users: User[];
}) {
  // 公演（event）
  const [title, setTitle] = useState("");
  const [url, setUrl] = useState("");
  const [catchphrase, setCatchphrase] = useState("");
  const [doorsOpen, setDoorsOpen] = useState("");
  const [entryDeadline, setEntryDeadline] = useState("");
  const [expectedDuration, setExpectedDuration] = useState("120");

  // チケット
  const [startDate, setStartDate] = useState("");
  const [startTime, setStartTime] = useState("");
  const [meetingTime, setMeetingTime] = useState("");
  const [meetingPlace, setMeetingPlace] = useState("");
  const [pricePerPerson, setPricePerPerson] = useState("");
  const [maxParticipants, setMaxParticipants] = useState("");
  const [participantIds, setParticipantIds] = useState<string[]>([]);

  const [submit, setSubmit] = useState<SubmitState>({ kind: "idle" });
  const submitting = submit.kind === "submitting";

  const toggleParticipant = (id: string) => {
    setParticipantIds((prev) =>
      prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id],
    );
  };

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    // 公演
    const trimmedTitle = title.trim();
    const trimmedUrl = url.trim();
    const trimmedCatchphrase = catchphrase.trim();
    if (trimmedTitle === "" || trimmedUrl === "") {
      setSubmit({ kind: "error", message: "公演のタイトルと URL を入力してください" });
      return;
    }
    if (trimmedCatchphrase.length > 255) {
      setSubmit({ kind: "error", message: "キャッチコピーは 255 文字以内で入力してください" });
      return;
    }
    const parsedDoorsOpen = parseOptionalNonNegativeInt(doorsOpen);
    if (parsedDoorsOpen === "invalid") {
      setSubmit({ kind: "error", message: "開場は 0 以上の整数で入力してください" });
      return;
    }
    const parsedEntryDeadline = parseOptionalNonNegativeInt(entryDeadline);
    if (parsedEntryDeadline === "invalid") {
      setSubmit({ kind: "error", message: "入場締切は 0 以上の整数で入力してください" });
      return;
    }
    const parsedExpectedDuration = parsePositiveInt(expectedDuration);
    if (parsedExpectedDuration === "invalid") {
      setSubmit({ kind: "error", message: "想定所要時間は 1 以上の整数で入力してください" });
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
      maxParticipants === ""
    ) {
      setSubmit({ kind: "error", message: "チケットに未入力の項目があります" });
      return;
    }
    if (!Number.isFinite(priceNum) || priceNum < 0 || !Number.isInteger(priceNum)) {
      setSubmit({ kind: "error", message: "金額は 0 以上の整数で入力してください" });
      return;
    }
    if (!Number.isFinite(maxNum) || maxNum < 1 || !Number.isInteger(maxNum)) {
      setSubmit({ kind: "error", message: "定員は 1 以上の整数で入力してください" });
      return;
    }
    if (participantIds.length === 0) {
      setSubmit({ kind: "error", message: "参加者を 1 人以上選択してください" });
      return;
    }
    if (participantIds.length > maxNum) {
      setSubmit({ kind: "error", message: "選択した参加者の人数が定員を超えています" });
      return;
    }

    const startAt = joinJSTDateTime(startDate, startTime);
    const meetingAt = joinJSTDateTime(startDate, meetingTime) ?? "";
    if (startAt === null) {
      setSubmit({ kind: "error", message: "チケットに未入力の項目があります" });
      return;
    }

    setSubmit({ kind: "submitting" });
    try {
      const res = await ticketClient.createTicketWithEvent({
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
        participantUserIds: participantIds,
      });
      const newTicketId = res.ticket?.id ?? "";
      router.push(newTicketId !== "" ? `/tickets/${newTicketId}` : "/tickets");
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, "/tickets/new");
        return;
      }
      const message =
        err instanceof Error ? err.message : "登録に失敗しました";
      setSubmit({ kind: "error", message });
    }
  };

  return (
    <>
      <AppHeader brand="謎部" user="" isAdmin />
      <PageShell>
        <Section>
          <SectionTitle>公演と参加チケットを登録</SectionTitle>
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
                  placeholder="例: 〇〇からの脱出"
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
                  placeholder="https://..."
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
                  placeholder="例: 限られた時間で謎を解け！"
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
                    placeholder="例: 15"
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
                    placeholder="例: 2"
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
                  placeholder="例: 4000"
                />
              </Field>

              <Field label="定員（このチケットで参加できる最大人数）" htmlFor="ticket-max-participants">
                <input
                  id="ticket-max-participants"
                  type="number"
                  required
                  min={1}
                  step={1}
                  inputMode="numeric"
                  value={maxParticipants}
                  onChange={(e) => setMaxParticipants(e.target.value)}
                  disabled={submitting}
                  className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                  placeholder="例: 4"
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
                          <span>{u.displayName}</span>
                        </label>
                      </li>
                    );
                  })}
                </ul>
              </fieldset>
            </fieldset>

            {submit.kind === "error" && (
              <p className="text-sm text-amber-800">{submit.message}</p>
            )}

            <div className="space-y-3 pt-2">
              <PrimaryButton type="submit" disabled={submitting}>
                {submitting ? "登録中…" : "登録する"}
              </PrimaryButton>
              <Link
                href="/tickets"
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
