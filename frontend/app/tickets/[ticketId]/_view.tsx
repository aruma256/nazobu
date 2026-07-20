"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";

import type {
  GetTicketResponse,
  Ticket,
  TicketExpense,
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
  const doorsOpenAt =
    ticket.eventDoorsOpenMinutesBefore !== undefined
      ? new Date(
          startAt.getTime() - ticket.eventDoorsOpenMinutesBefore * 60 * 1000,
        )
      : null;
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
              {doorsOpenAt !== null && (
                <>
                  <dt>開場</dt>
                  <dd>
                    <Mono>{formatTimeHM(doorsOpenAt)}</Mono>
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
                  {detail.participants.length +
                    ticket.unregisteredParticipantsCount}
                  <span className="text-zinc-500"> / </span>
                  {ticket.maxParticipants}
                </Mono>
                <span className="text-zinc-500"> 人</span>
                {ticket.unregisteredParticipantsCount > 0 && (
                  <span className="ml-1 text-zinc-500">
                    （未登録{" "}
                    <Mono>{ticket.unregisteredParticipantsCount}</Mono>{" "}
                    人を含む）
                  </span>
                )}
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
          unregisteredCount={ticket.unregisteredParticipantsCount}
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

        <ExpensesSection
          expenses={detail.expenses}
          ticketParticipants={detail.participants}
          myUserId={me.id}
          canAddExpense={detail.canAddExpense}
          mutating={mutating}
          onCreate={(title, participants) =>
            handleMutation(() =>
              ticketClient.createTicketExpense({
                ticketId: ticket.id,
                title,
                participants,
              }),
            )
          }
          onUpdate={(expenseId, title, participants) =>
            handleMutation(() =>
              ticketClient.updateTicketExpense({
                expenseId,
                title,
                participants,
              }),
            )
          }
          onDelete={(expenseId) =>
            handleMutation(() => ticketClient.deleteTicketExpense({ expenseId }))
          }
          onToggleSettlement={(expenseId, userId, settled) =>
            handleMutation(() =>
              ticketClient.updateTicketExpenseSettlement({
                expenseId,
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
  const [sizeText, setSizeText] = useState("4");
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
  unregisteredCount,
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
  unregisteredCount: number;
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

  // 未登録の同行者も定員の枠を消費する。
  const occupiedCount = participants.length + unregisteredCount;

  return (
    <Section>
      <SectionTitle count={occupiedCount}>参加者</SectionTitle>

      {occupiedCount === 0 ? (
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
          {unregisteredCount > 0 && (
            <li className="flex items-center gap-3 px-4 py-3">
              <span className="text-sm text-zinc-500">
                未登録の同行者 <Mono>{unregisteredCount}</Mono> 人
              </span>
              <Badge tone="muted">精算対象外</Badge>
            </li>
          )}
        </ul>
      )}

      {canEdit && (
        <div className="mt-4 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
          <div className="flex items-baseline gap-3 px-4 pt-4 pb-2">
            <span className="text-sm font-medium text-zinc-700">参加者を追加</span>
            <span className="text-xs text-zinc-500">
              残り {Math.max(0, maxParticipants - occupiedCount)} 人
            </span>
          </div>
          {candidates.length === 0 ? (
            <p className="px-4 pb-4 text-xs text-zinc-500">
              追加できるユーザーがいません。
            </p>
          ) : occupiedCount >= maxParticipants ? (
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
                {occupiedCount + selectedToAdd.length > maxParticipants && (
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
                    occupiedCount + selectedToAdd.length > maxParticipants
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

// 追加精算（打ち上げ飲み会など）の登録・更新で使う対象者 1 人分の入力。
type ExpenseParticipantDraft = { userId: string; amount: number };

// チケットにぶら下がる追加精算のセクション。
// 費目ごとに対象者と負担額（人によって変えられる）・精算状況を表示し、
// 立替者（と admin）は編集・削除・精算トグルができる。
function ExpensesSection({
  expenses,
  ticketParticipants,
  myUserId,
  canAddExpense,
  mutating,
  onCreate,
  onUpdate,
  onDelete,
  onToggleSettlement,
}: {
  expenses: TicketExpense[];
  ticketParticipants: TicketParticipant[];
  myUserId: string;
  canAddExpense: boolean;
  mutating: boolean;
  onCreate: (title: string, participants: ExpenseParticipantDraft[]) => Promise<void>;
  onUpdate: (
    expenseId: string,
    title: string,
    participants: ExpenseParticipantDraft[],
  ) => Promise<void>;
  onDelete: (expenseId: string) => Promise<void>;
  onToggleSettlement: (
    expenseId: string,
    userId: string,
    settled: boolean,
  ) => Promise<void>;
}) {
  const [adding, setAdding] = useState(false);
  const [editingExpenseId, setEditingExpenseId] = useState<string | null>(null);

  if (expenses.length === 0 && !canAddExpense) return null;

  return (
    <Section>
      <SectionTitle count={expenses.length > 0 ? expenses.length : undefined}>
        追加精算
      </SectionTitle>
      <p className="mt-1 text-xs text-zinc-500">
        打ち上げ飲み会など、公演のあとに発生した費用の精算。負担額は人ごとに変えられます。
      </p>

      {expenses.map((expense) =>
        editingExpenseId === expense.id ? (
          <div
            key={expense.id}
            className="mt-3 overflow-hidden rounded-2xl border border-zinc-200 bg-white"
          >
            <ExpenseForm
              heading={`「${expense.title}」を編集`}
              ticketParticipants={ticketParticipants}
              initial={expense}
              mutating={mutating}
              submitLabel="保存"
              onSubmit={async (title, participants) => {
                await onUpdate(expense.id, title, participants);
                setEditingExpenseId(null);
              }}
              onCancel={() => setEditingExpenseId(null)}
            />
          </div>
        ) : (
          <ExpenseCard
            key={expense.id}
            expense={expense}
            myUserId={myUserId}
            mutating={mutating}
            onEdit={() => {
              setAdding(false);
              setEditingExpenseId(expense.id);
            }}
            onDelete={() => onDelete(expense.id)}
            onToggleSettlement={(userId, settled) =>
              onToggleSettlement(expense.id, userId, settled)
            }
          />
        ),
      )}

      {canAddExpense &&
        (adding ? (
          <div className="mt-4 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
            <ExpenseForm
              heading="追加精算を登録"
              ticketParticipants={ticketParticipants}
              mutating={mutating}
              submitLabel="登録（立替者は自分になります）"
              onSubmit={async (title, participants) => {
                await onCreate(title, participants);
                setAdding(false);
              }}
              onCancel={() => setAdding(false)}
            />
          </div>
        ) : (
          <button
            type="button"
            onClick={() => {
              setEditingExpenseId(null);
              setAdding(true);
            }}
            disabled={mutating}
            className="mt-4 inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50 disabled:opacity-50"
          >
            追加精算を登録
          </button>
        ))}
    </Section>
  );
}

// 追加精算 1 件分の表示カード。
function ExpenseCard({
  expense,
  myUserId,
  mutating,
  onEdit,
  onDelete,
  onToggleSettlement,
}: {
  expense: TicketExpense;
  myUserId: string;
  mutating: boolean;
  onEdit: () => void;
  onDelete: () => Promise<void>;
  onToggleSettlement: (userId: string, settled: boolean) => Promise<void>;
}) {
  const total = expense.participants.reduce((sum, p) => sum + p.amount, 0);
  return (
    <div className="mt-3 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      <div className="flex items-baseline gap-3 px-4 pt-4">
        <h3 className="text-sm leading-snug font-semibold">{expense.title}</h3>
        <Mono className="ml-auto text-sm font-semibold tracking-tight">
          {formatYen(total)}
        </Mono>
      </div>
      <p className="px-4 pt-0.5 pb-3 text-xs text-zinc-500">
        立替 {expense.payerName}
      </p>
      <ul className="divide-y divide-zinc-100 border-t border-zinc-200">
        {expense.participants.map((p) => (
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
            <Mono className="text-sm text-zinc-900">{formatYen(p.amount)}</Mono>
            {p.isPayer ? (
              <Badge tone="muted">立替</Badge>
            ) : p.settled ? (
              <Badge tone="settled">精算済</Badge>
            ) : (
              <Badge tone="unsettled">未精算</Badge>
            )}
            {expense.canEdit && !p.isPayer && (
              <button
                type="button"
                onClick={() => onToggleSettlement(p.userId, !p.settled)}
                disabled={mutating}
                className="ml-auto inline-flex h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
              >
                {p.settled ? "未精算に戻す" : "精算済にする"}
              </button>
            )}
          </li>
        ))}
      </ul>
      {expense.canEdit && (
        <div className="flex flex-wrap gap-2 border-t border-zinc-200 px-4 py-3">
          <button
            type="button"
            onClick={onEdit}
            disabled={mutating}
            className="inline-flex h-9 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-emerald-700 hover:bg-zinc-50 disabled:opacity-50"
          >
            編集
          </button>
          <button
            type="button"
            onClick={onDelete}
            disabled={mutating}
            className="inline-flex h-9 items-center justify-center rounded-lg border border-amber-200 bg-white px-3 text-xs font-semibold text-amber-800 hover:bg-amber-50 disabled:opacity-50"
          >
            削除
          </button>
        </div>
      )}
    </div>
  );
}

// 追加精算の登録・編集フォーム。対象者はチケット参加者から選び、
// 「一人あたり」で均等額を一括入力したうえで、人ごとに金額を調整できる。
function ExpenseForm({
  heading,
  ticketParticipants,
  initial,
  mutating,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  heading: string;
  ticketParticipants: TicketParticipant[];
  initial?: TicketExpense;
  mutating: boolean;
  submitLabel: string;
  onSubmit: (title: string, participants: ExpenseParticipantDraft[]) => Promise<void>;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState(initial?.title ?? "");
  const [amounts, setAmounts] = useState<Record<string, string>>(() => {
    const init: Record<string, string> = {};
    for (const p of initial?.participants ?? []) {
      init[p.userId] = String(p.amount);
    }
    return init;
  });
  // 新規登録では「全員参加」が最頻ケースなのでチケット参加者全員をプリチェックする。
  const [checked, setChecked] = useState<Set<string>>(() =>
    initial
      ? new Set(initial.participants.map((p) => p.userId))
      : new Set(ticketParticipants.map((p) => p.userId)),
  );
  const [perPersonText, setPerPersonText] = useState("");

  // 編集時、対象者がチケット参加者から外れていても行を残す（金額・精算記録を保持するため）。
  const rows = useMemo(() => {
    const seen = new Set<string>();
    const list: { userId: string; name: string }[] = [];
    for (const p of ticketParticipants) {
      seen.add(p.userId);
      list.push({ userId: p.userId, name: p.name });
    }
    for (const p of initial?.participants ?? []) {
      if (!seen.has(p.userId)) list.push({ userId: p.userId, name: p.name });
    }
    return list;
  }, [ticketParticipants, initial]);

  const toggleChecked = (userId: string) => {
    setChecked((prev) => {
      const next = new Set(prev);
      if (next.has(userId)) {
        next.delete(userId);
      } else {
        next.add(userId);
      }
      return next;
    });
  };

  const applyPerPerson = () => {
    const perPerson = Number.parseInt(perPersonText, 10);
    if (Number.isNaN(perPerson) || perPerson < 0) return;
    setAmounts((prev) => {
      const next = { ...prev };
      for (const userId of checked) {
        next[userId] = String(perPerson);
      }
      return next;
    });
  };

  const drafts: ExpenseParticipantDraft[] = [];
  let amountInvalid = false;
  for (const userId of checked) {
    const amount = Number.parseInt(amounts[userId] ?? "", 10);
    if (Number.isNaN(amount) || amount < 0) {
      amountInvalid = true;
      continue;
    }
    drafts.push({ userId, amount });
  }
  const canSubmit =
    !mutating && title.trim() !== "" && checked.size > 0 && !amountInvalid;

  return (
    <div className="px-4 py-4">
      <p className="text-sm font-medium text-zinc-700">{heading}</p>

      <label className="mt-3 block text-xs text-zinc-700">
        費目名
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="例: 打ち上げ飲み会"
          maxLength={255}
          className="mt-1 block h-11 w-full rounded-lg border border-zinc-200 px-3 text-base text-zinc-900 focus:border-emerald-600 focus:ring-1 focus:ring-emerald-600 focus:outline-none"
        />
      </label>

      <div className="mt-3 flex flex-wrap items-end gap-2">
        <label className="block text-xs text-zinc-700">
          一人あたり（円）
          <input
            type="number"
            min={0}
            inputMode="numeric"
            value={perPersonText}
            onChange={(e) => setPerPersonText(e.target.value)}
            className="mt-1 block h-11 w-28 rounded-lg border border-zinc-200 px-3 text-right font-mono text-base tabular-nums focus:border-emerald-600 focus:ring-1 focus:ring-emerald-600 focus:outline-none"
          />
        </label>
        <button
          type="button"
          onClick={applyPerPerson}
          disabled={mutating || checked.size === 0}
          className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-3 text-xs font-semibold text-emerald-700 hover:bg-zinc-50 disabled:opacity-50"
        >
          選択中の {checked.size} 人に適用
        </button>
      </div>

      <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-xl border border-zinc-200">
        {rows.map((row) => {
          const isChecked = checked.has(row.userId);
          return (
            <li
              key={row.userId}
              className="flex items-center gap-3 px-3 py-2"
            >
              <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-sm text-zinc-900">
                <input
                  type="checkbox"
                  checked={isChecked}
                  onChange={() => toggleChecked(row.userId)}
                  disabled={mutating}
                  className="size-4 rounded border-zinc-300 text-emerald-700 focus:ring-emerald-600"
                />
                <span className="truncate">{row.name}</span>
              </label>
              {isChecked && (
                <input
                  type="number"
                  min={0}
                  inputMode="numeric"
                  value={amounts[row.userId] ?? ""}
                  onChange={(e) =>
                    setAmounts((prev) => ({
                      ...prev,
                      [row.userId]: e.target.value,
                    }))
                  }
                  disabled={mutating}
                  aria-label={`${row.name} の負担額`}
                  placeholder="0"
                  className="h-11 w-24 rounded-lg border border-zinc-200 px-2 text-right font-mono text-base tabular-nums focus:border-emerald-600 focus:ring-1 focus:ring-emerald-600 focus:outline-none"
                />
              )}
            </li>
          );
        })}
      </ul>

      {checked.size > 0 && (
        <p className="mt-2 text-xs text-zinc-500">
          合計{" "}
          <Mono>
            {formatYen(drafts.reduce((sum, d) => sum + d.amount, 0))}
          </Mono>
          （{checked.size} 人）
          {amountInvalid && (
            <span className="ml-2 text-amber-800">
              金額が未入力の対象者がいます。
            </span>
          )}
        </p>
      )}

      <div className="mt-4 flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => onSubmit(title.trim(), drafts)}
          disabled={!canSubmit}
          className="inline-flex h-11 items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900 disabled:opacity-50"
        >
          {submitLabel}
        </button>
        <button
          type="button"
          onClick={onCancel}
          disabled={mutating}
          className="inline-flex h-11 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50 disabled:opacity-50"
        >
          キャンセル
        </button>
      </div>
    </div>
  );
}
