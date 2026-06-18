"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import type {
  GetTicketResponse,
  Ticket,
  TicketParticipant,
} from "@/app/gen/nazobu/v1/ticket_pb";
import type { GetMeResponse, User } from "@/app/gen/nazobu/v1/user_pb";
import { ticketClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  Badge,
  Mono,
  PageShell,
  Section,
  SectionTitle,
} from "@/app/_components";
import {
  formatDateJa,
  formatTimeHM,
  formatYen,
  parseDateTime,
} from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "not_found" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      detail: GetTicketResponse;
      users: User[];
    };

export function TicketDetailView({ ticketId }: { ticketId: string }) {
  const router = useRouter();
  const [state, setState] = useState<LoadState>({ kind: "loading" });
  const [mutating, setMutating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    try {
      const [me, detail, usersRes] = await Promise.all([
        userClient.getMe({}),
        ticketClient.getTicket({ ticketId }),
        userClient.listUsers({}),
      ]);
      setState({ kind: "ready", me, detail, users: usersRes.users });
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, `/tickets/${ticketId}`);
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
  }, [router, ticketId]);

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
          redirectToLogin(router, `/tickets/${ticketId}`);
          return;
        }
        const message =
          err instanceof Error ? err.message : "更新に失敗しました";
        setError(message);
      } finally {
        setMutating(false);
      }
    },
    [mutating, reload, router, ticketId],
  );

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

  const { me, detail, users } = state;
  const ticket = detail.ticket;
  if (!ticket) {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            チケット情報の取得に失敗しました
          </p>
        </PageShell>
      </>
    );
  }

  const displayName = me.displayName;
  const isAdmin = me.role === "admin";
  const startAt = parseDateTime(ticket.startAt);
  const endAt = new Date(
    startAt.getTime() + ticket.eventExpectedDurationMinutes * 60 * 1000,
  );
  const meetingAt =
    ticket.meetingAt !== "" ? parseDateTime(ticket.meetingAt) : null;
  const hasMeeting = meetingAt !== null || ticket.meetingPlace !== "";
  const canEdit = detail.canEdit;

  return (
    <>
      <AppHeader brand="謎部" user={displayName} isAdmin={isAdmin} />
      <PageShell>
        <Section>
          <SectionTitle>チケット詳細</SectionTitle>

          <div className="mt-3 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
            <div className="flex items-baseline gap-3 px-4 pt-4">
              <Mono className="text-sm font-semibold text-emerald-700">
                {formatDateJa(startAt)}
              </Mono>
              <Mono className="ml-auto text-sm font-semibold tracking-tight">
                {formatYen(ticket.pricePerPerson)}
              </Mono>
            </div>
            <h3 className="px-4 pt-1 text-base leading-snug font-semibold">
              {ticket.eventTitle}
            </h3>
            {ticket.eventCatchphrase !== "" && (
              <p className="px-4 pt-0.5 text-sm text-zinc-700">
                {ticket.eventCatchphrase}
              </p>
            )}
            {ticket.eventUrl !== "" && (
              <a
                href={ticket.eventUrl}
                target="_blank"
                rel="noreferrer noopener"
                className="mt-1 block truncate px-4 text-xs text-emerald-700 underline decoration-zinc-300 underline-offset-4 hover:decoration-emerald-700"
              >
                {ticket.eventUrl}
              </a>
            )}
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 px-4 pt-3 pb-4 text-xs text-zinc-900">
              {hasMeeting && (
                <>
                  <dt>集合</dt>
                  <dd>
                    {meetingAt !== null && (
                      <Mono>{formatTimeHM(meetingAt)}</Mono>
                    )}
                    {meetingAt !== null && ticket.meetingPlace !== "" && " "}
                    {ticket.meetingPlace !== "" && ticket.meetingPlace}
                  </dd>
                </>
              )}
              <dt>開演</dt>
              <dd>
                <Mono>{formatTimeHM(startAt)}</Mono>
                <span className="ml-1 text-zinc-500">
                  （〜<Mono>{formatTimeHM(endAt)}</Mono>）
                </span>
              </dd>
              <dt>立替</dt>
              <dd>{ticket.purchaserName}</dd>
              <dt>定員</dt>
              <dd>
                <Mono>
                  {detail.participants.length}
                  <span className="text-zinc-500"> / </span>
                  {ticket.maxParticipants}
                </Mono>
                <span className="text-zinc-500"> 人</span>
              </dd>
            </dl>

            <div className="flex flex-wrap gap-2 border-t border-zinc-200 px-4 py-3">
              <a
                href={buildGoogleCalendarUrl(ticket)}
                target="_blank"
                rel="noreferrer noopener"
                className="inline-flex h-10 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50"
              >
                Google カレンダーに追加
              </a>
              {canEdit && (
                <Link
                  href={`/tickets/${ticket.id}/edit`}
                  className="inline-flex h-10 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50"
                >
                  チケット情報を編集
                </Link>
              )}
            </div>
          </div>
        </Section>

        <ParticipantsSection
          ticketId={ticket.id}
          participants={detail.participants}
          maxParticipants={ticket.maxParticipants}
          allUsers={users}
          myUserId={me.id}
          canEdit={canEdit}
          mutating={mutating}
          onAdd={(userIds) =>
            handleMutation(() =>
              ticketClient.addTicketParticipants({
                ticketId: ticket.id,
                userIds,
              }),
            )
          }
          onRemove={(userId) =>
            handleMutation(() =>
              ticketClient.removeTicketParticipant({
                ticketId: ticket.id,
                userId,
              }),
            )
          }
          onToggleSettlement={(userId, settled) =>
            handleMutation(() =>
              ticketClient.updateTicketParticipantSettlement({
                ticketId: ticket.id,
                userId,
                settled,
              }),
            )
          }
        />

        <GroupShuffleSection
          participants={detail.participants}
          myUserId={me.id}
        />

        {error !== null && (
          <Section>
            <p className="text-sm text-amber-800">エラー: {error}</p>
          </Section>
        )}

        <Section>
          <Link
            href="/tickets"
            className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
          >
            チケット一覧に戻る
          </Link>
        </Section>
      </PageShell>
    </>
  );
}

// Google カレンダーの「予定を作成」プリフィル URL を組み立てる。
// 開始 = meeting_at（あれば）/ start_at、終了 = start_at + event.expected_duration_minutes。
// 立替・参加者は意図的に説明欄に含めない。
function buildGoogleCalendarUrl(ticket: Ticket): string {
  const startAt = parseDateTime(ticket.startAt);
  const startMs =
    ticket.meetingAt !== ""
      ? parseDateTime(ticket.meetingAt).getTime()
      : startAt.getTime();
  const endMs =
    startAt.getTime() + ticket.eventExpectedDurationMinutes * 60 * 1000;

  const detailLines = [
    `開演 ${formatTimeHM(startAt)}`,
    `一人 ${formatYen(ticket.pricePerPerson)}`,
  ];
  if (ticket.eventUrl !== "") {
    detailLines.push("", `公演 ${ticket.eventUrl}`);
  }
  if (typeof window !== "undefined") {
    detailLines.push(`謎部 ${window.location.href}`);
  }

  const params = new URLSearchParams({
    action: "TEMPLATE",
    text: ticket.eventTitle,
    dates: `${jstCompactDateTime(startMs)}/${jstCompactDateTime(endMs)}`,
    ctz: "Asia/Tokyo",
    location: ticket.meetingPlace,
    details: detailLines.join("\n"),
  });
  return `https://calendar.google.com/calendar/render?${params.toString()}`;
}

// JST の壁時計を "YYYYMMDDTHHMMSS" に整形する。Google カレンダーの dates 引数用。
// sv-SE ロケールは ISO 8601 形式 ("YYYY-MM-DD HH:MM:SS") を返すため、区切りを除くだけで済む。
function jstCompactDateTime(ms: number): string {
  const s = new Date(ms).toLocaleString("sv-SE", {
    timeZone: "Asia/Tokyo",
    hour12: false,
  });
  return s.replace(/[-:]/g, "").replace(" ", "T");
}

// 参加者をシャッフルして 1 グループあたり size 人ずつ先頭から詰める。
// 例: 6 人で size=4 なら A:4 人 / B:2 人、size=3 なら 3/3。永続化はしない。
function GroupShuffleSection({
  participants,
  myUserId,
}: {
  participants: TicketParticipant[];
  myUserId: string;
}) {
  const total = participants.length;
  const [sizeText, setSizeText] = useState("2");
  const [groups, setGroups] = useState<TicketParticipant[][] | null>(null);

  // 参加者の顔ぶれが変わったら古い分け方は破棄する（古い参加者が残らないように）。
  const idsKey = useMemo(
    () =>
      participants
        .map((p) => p.userId)
        .sort()
        .join(","),
    [participants],
  );
  useEffect(() => {
    setGroups(null);
  }, [idsKey]);

  if (total < 2) return null;

  const size = Math.min(Math.max(1, Number.parseInt(sizeText, 10) || 1), total);
  const groupCount = Math.ceil(total / size);

  const shuffle = () => {
    const shuffled = [...participants];
    // Fisher–Yates でその場シャッフル
    for (let i = shuffled.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
    }
    const result: TicketParticipant[][] = [];
    for (let i = 0; i < shuffled.length; i += size) {
      result.push(shuffled.slice(i, i + size));
    }
    setGroups(result);
  };

  return (
    <Section>
      <SectionTitle>グループ分け</SectionTitle>
      <div className="mt-3 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-4">
          <label className="flex items-center gap-2 text-sm text-zinc-700">
            1 グループ
            <input
              type="number"
              min={1}
              max={total}
              inputMode="numeric"
              value={sizeText}
              onChange={(e) => setSizeText(e.target.value)}
              className="h-10 w-16 rounded-lg border border-zinc-200 px-2 text-center font-mono tabular-nums focus:border-emerald-600 focus:ring-1 focus:ring-emerald-600 focus:outline-none"
            />
            人
          </label>
          <span className="text-xs text-zinc-500">
            {total} 人 → <Mono>{groupCount}</Mono> グループ
          </span>
          <button
            type="button"
            onClick={shuffle}
            className="ml-auto inline-flex h-10 items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900"
          >
            {groups === null ? "シャッフル" : "シャッフルし直す"}
          </button>
        </div>

        {groups !== null && (
          <div className="grid grid-cols-1 gap-3 border-t border-zinc-200 p-4 sm:grid-cols-2">
            {groups.map((group, i) => (
              <div
                key={i}
                className="overflow-hidden rounded-xl border border-zinc-200"
              >
                <div className="flex items-baseline justify-between bg-zinc-50 px-3 py-2">
                  <span className="text-sm font-semibold text-zinc-900">
                    グループ {String.fromCharCode(65 + i)}
                  </span>
                  <span className="text-xs text-zinc-500">
                    <Mono>{group.length}</Mono> 人
                  </span>
                </div>
                <ul className="divide-y divide-zinc-100">
                  {group.map((p) => (
                    <li
                      key={p.userId}
                      className={
                        p.userId === myUserId
                          ? "px-3 py-2 text-sm font-semibold text-emerald-700"
                          : "px-3 py-2 text-sm text-zinc-900"
                      }
                    >
                      {p.name}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </div>
    </Section>
  );
}

function ParticipantsSection({
  participants,
  maxParticipants,
  allUsers,
  myUserId,
  canEdit,
  mutating,
  onAdd,
  onRemove,
  onToggleSettlement,
}: {
  ticketId: string;
  participants: TicketParticipant[];
  maxParticipants: number;
  allUsers: User[];
  myUserId: string;
  canEdit: boolean;
  mutating: boolean;
  onAdd: (userIds: string[]) => Promise<void>;
  onRemove: (userId: string) => Promise<void>;
  onToggleSettlement: (userId: string, settled: boolean) => Promise<void>;
}) {
  const participantIds = useMemo(
    () => new Set(participants.map((p) => p.userId)),
    [participants],
  );
  const sortedParticipants = useMemo(
    () =>
      [...participants].sort((a, b) => {
        if (a.userId === myUserId) return -1;
        if (b.userId === myUserId) return 1;
        return a.name.localeCompare(b.name, "ja");
      }),
    [participants, myUserId],
  );
  const candidates = useMemo(
    () => allUsers.filter((u) => !participantIds.has(u.id)),
    [allUsers, participantIds],
  );
  const [selectedToAdd, setSelectedToAdd] = useState<string[]>([]);

  const toggleSelectedToAdd = (id: string) => {
    setSelectedToAdd((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const handleAdd = async () => {
    if (selectedToAdd.length === 0) return;
    const ids = selectedToAdd;
    setSelectedToAdd([]);
    await onAdd(ids);
  };

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
              {p.isPurchaser ? (
                <Badge tone="muted">立替</Badge>
              ) : p.settled ? (
                <Badge tone="settled">精算済</Badge>
              ) : (
                <Badge tone="unsettled">未精算</Badge>
              )}
              {canEdit && !p.isPurchaser && (
                <div className="ml-auto flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => onToggleSettlement(p.userId, !p.settled)}
                    disabled={mutating}
                    className="inline-flex h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
                  >
                    {p.settled ? "未精算に戻す" : "精算済にする"}
                  </button>
                  <button
                    type="button"
                    onClick={() => onRemove(p.userId)}
                    disabled={mutating}
                    className="inline-flex h-9 items-center justify-center rounded-lg border border-amber-200 bg-white px-3 text-xs font-semibold text-amber-800 hover:bg-amber-50 disabled:opacity-50"
                    aria-label={`${p.name} を削除`}
                  >
                    削除
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {canEdit && (
        <div className="mt-4 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
          <div className="flex items-baseline gap-3 px-4 pt-4 pb-2">
            <span className="text-sm font-medium text-zinc-700">参加者を追加</span>
            <span className="text-xs text-zinc-500">
              残り {Math.max(0, maxParticipants - participants.length)} 人
            </span>
          </div>
          {candidates.length === 0 ? (
            <p className="px-4 pb-4 text-xs text-zinc-500">
              追加できるユーザーがいません。
            </p>
          ) : participants.length >= maxParticipants ? (
            <p className="px-4 pb-4 text-xs text-amber-800">
              定員に達しているため追加できません。
            </p>
          ) : (
            <>
              <ul className="divide-y divide-zinc-200 border-t border-zinc-200">
                {candidates.map((u) => {
                  const checked = selectedToAdd.includes(u.id);
                  const label = u.displayName;
                  return (
                    <li key={u.id}>
                      <label className="flex h-11 cursor-pointer items-center gap-3 px-4 text-sm text-zinc-900 hover:bg-zinc-50">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleSelectedToAdd(u.id)}
                          disabled={mutating}
                          className="size-4 rounded border-zinc-300 text-emerald-700 focus:ring-emerald-600"
                        />
                        <span>{label}</span>
                      </label>
                    </li>
                  );
                })}
              </ul>
              <div className="px-4 py-3">
                {participants.length + selectedToAdd.length >
                  maxParticipants && (
                  <p className="mb-2 text-xs text-amber-800">
                    選択中の人数が定員を超えています。
                  </p>
                )}
                <button
                  type="button"
                  onClick={handleAdd}
                  disabled={
                    mutating ||
                    selectedToAdd.length === 0 ||
                    participants.length + selectedToAdd.length >
                      maxParticipants
                  }
                  className="inline-flex h-10 items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900 disabled:opacity-50"
                >
                  選択した {selectedToAdd.length} 人を追加
                </button>
              </div>
            </>
          )}
        </div>
      )}
    </Section>
  );
}
