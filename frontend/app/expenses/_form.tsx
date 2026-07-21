"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";

import type { Ticket } from "@/app/gen/nazobu/v1/ticket_pb";
import type { User } from "@/app/gen/nazobu/v1/user_pb";

import { Badge, PrimaryButton } from "@/app/_components";
import {
  formatDateJa,
  formatYen,
  parseDateTime,
  toDateInputValue,
} from "@/app/_format";

// 送信ボタン等の状態。
type SubmitState =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "error"; message: string };

// 編集時の初期参加者（負担額・精算状態つき）。
export type ExpenseFormInitialParticipant = {
  userId: string;
  amount: number;
  settled: boolean;
};

export type ExpenseFormData = {
  ticketId: string;
  title: string;
  occurredOn: string;
  // 立替者の user id。create では常にログイン user、edit では選択値。
  paidByUserId: string;
  participants: { userId: string; amount: number }[];
};

// ExpenseForm は精算の登録 / 編集で共通のフォーム。
// mode="create" では立替者はログイン user（myUserId）に固定し、参加者候補から除外する。
// mode="edit" では立替者を全 user から選べる（選んだ user は参加者から自動的に外す）。
export function ExpenseForm({
  mode,
  users,
  tickets,
  myUserId,
  initialTitle = "",
  initialOccurredOn,
  initialTicketId = "",
  initialPaidByUserId,
  initialParticipants = [],
  submitLabel,
  submittingLabel,
  cancelHref,
  onSubmit,
}: {
  mode: "create" | "edit";
  users: User[];
  tickets: Ticket[];
  myUserId: string;
  initialTitle?: string;
  initialOccurredOn?: string;
  initialTicketId?: string;
  initialPaidByUserId?: string;
  initialParticipants?: ExpenseFormInitialParticipant[];
  submitLabel: string;
  submittingLabel: string;
  cancelHref: string;
  onSubmit: (data: ExpenseFormData) => Promise<void>;
}) {
  const [title, setTitle] = useState(initialTitle);
  const [occurredOn, setOccurredOn] = useState(
    initialOccurredOn ?? toDateInputValue(new Date()),
  );
  const [ticketId, setTicketId] = useState(initialTicketId);
  const [paidByUserId, setPaidByUserId] = useState(
    mode === "create" ? myUserId : (initialPaidByUserId ?? ""),
  );
  const [selectedIds, setSelectedIds] = useState<string[]>(
    initialParticipants.map((p) => p.userId),
  );
  const [amounts, setAmounts] = useState<Record<string, string>>(() => {
    const m: Record<string, string> = {};
    for (const p of initialParticipants) m[p.userId] = String(p.amount);
    return m;
  });
  const [splitTotal, setSplitTotal] = useState("");
  const [state, setState] = useState<SubmitState>({ kind: "idle" });
  const submitting = state.kind === "submitting";

  // 登録時に精算済みだった参加者（外すと記録が消える対象）。
  const settledInitially = useMemo(
    () =>
      new Set(
        initialParticipants.filter((p) => p.settled).map((p) => p.userId),
      ),
    [initialParticipants],
  );

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  // 立替者本人は参加者に含められないため候補から除外する。
  const candidates = useMemo(
    () => users.filter((u) => u.id !== paidByUserId),
    [users, paidByUserId],
  );
  // 送信・均等割りの「先頭から」の順序は候補（表示名昇順）の並びに揃える。
  const orderedSelected = useMemo(
    () => candidates.filter((u) => selectedSet.has(u.id)),
    [candidates, selectedSet],
  );

  // 入力済み負担額の合計（表示用。未入力・不正値は 0 とみなす）。
  const currentTotal = orderedSelected.reduce((sum, u) => {
    const n = Number((amounts[u.id] ?? "").trim());
    return sum + (Number.isFinite(n) && n > 0 ? n : 0);
  }, 0);

  // 外された精算済み参加者（警告表示用）。
  const removedSettled = useMemo(
    () =>
      users.filter(
        (u) => settledInitially.has(u.id) && !selectedSet.has(u.id),
      ),
    [users, settledInitially, selectedSet],
  );

  const toggleParticipant = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  };

  const changePaidBy = (id: string) => {
    setPaidByUserId(id);
    // 立替者に選んだ user が参加者に入っていたら外す（同一 user は両立できない）。
    setSelectedIds((prev) => prev.filter((x) => x !== id));
  };

  const setAmount = (id: string, value: string) => {
    setAmounts((prev) => ({ ...prev, [id]: value }));
  };

  // 合計金額を選択中の参加者へ均等割りする。端数は先頭の参加者から 1 円ずつ配る。
  const applyEvenSplit = () => {
    const n = orderedSelected.length;
    if (n === 0) {
      setState({ kind: "error", message: "先に参加者を選択してください" });
      return;
    }
    const total = Number(splitTotal.trim());
    if (
      splitTotal.trim() === "" ||
      !Number.isFinite(total) ||
      !Number.isInteger(total) ||
      total < 0
    ) {
      setState({
        kind: "error",
        message: "合計金額は 0 以上の整数で入力してください",
      });
      return;
    }
    const base = Math.floor(total / n);
    const remainder = total - base * n;
    const next = { ...amounts };
    orderedSelected.forEach((u, i) => {
      next[u.id] = String(base + (i < remainder ? 1 : 0));
    });
    setAmounts(next);
    setState({ kind: "idle" });
  };

  const onFormSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (submitting) return;

    const trimmedTitle = title.trim();
    if (trimmedTitle === "") {
      setState({ kind: "error", message: "タイトルを入力してください" });
      return;
    }
    if (occurredOn === "") {
      setState({ kind: "error", message: "発生日を入力してください" });
      return;
    }
    if (mode === "edit" && paidByUserId === "") {
      setState({ kind: "error", message: "立替者を選択してください" });
      return;
    }
    if (orderedSelected.length === 0) {
      setState({ kind: "error", message: "参加者を 1 人以上選択してください" });
      return;
    }
    const participants: { userId: string; amount: number }[] = [];
    for (const u of orderedSelected) {
      const raw = (amounts[u.id] ?? "").trim();
      const n = Number(raw);
      if (
        raw === "" ||
        !Number.isFinite(n) ||
        !Number.isInteger(n) ||
        n < 0
      ) {
        setState({
          kind: "error",
          message: `${u.displayName} の負担額は 0 以上の整数で入力してください`,
        });
        return;
      }
      participants.push({ userId: u.id, amount: n });
    }

    setState({ kind: "submitting" });
    try {
      await onSubmit({
        ticketId,
        title: trimmedTitle,
        occurredOn,
        paidByUserId,
        participants,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : "保存に失敗しました";
      setState({ kind: "error", message });
    }
  };

  return (
    <form onSubmit={onFormSubmit} className="mt-3 space-y-6">
      <fieldset className="space-y-4">
        <Field label="タイトル" htmlFor="expense-title">
          <input
            id="expense-title"
            type="text"
            required
            maxLength={255}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            disabled={submitting}
            className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
            placeholder="例: 打ち上げ @ 〇〇"
          />
        </Field>

        <Field label="発生日" htmlFor="expense-occurred-on">
          <input
            id="expense-occurred-on"
            type="date"
            required
            value={occurredOn}
            onChange={(e) => setOccurredOn(e.target.value)}
            disabled={submitting}
            className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
          />
        </Field>

        <Field label="紐付ける公演（任意）" htmlFor="expense-ticket">
          <select
            id="expense-ticket"
            value={ticketId}
            onChange={(e) => setTicketId(e.target.value)}
            disabled={submitting}
            className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
          >
            <option value="">紐付けない</option>
            {tickets.map((t) => (
              <option key={t.id} value={t.id}>
                {`${formatDateJa(parseDateTime(t.startAt))} ${t.eventTitle}`}
              </option>
            ))}
          </select>
          <p className="mt-1 text-xs text-zinc-500">
            公演後の飲み会など、特定の公演に紐付けたい場合に選択してください。
          </p>
        </Field>

        {mode === "edit" && (
          <Field label="立替者" htmlFor="expense-paid-by">
            <select
              id="expense-paid-by"
              required
              value={paidByUserId}
              onChange={(e) => changePaidBy(e.target.value)}
              disabled={submitting}
              className="block h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
            >
              {paidByUserId === "" && <option value="">選択してください</option>}
              {users.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.displayName}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-zinc-500">
              立替者は精算対象に含まれません。立替者に選んだ人は参加者から自動的に外れます。
            </p>
          </Field>
        )}
      </fieldset>

      <fieldset className="space-y-3 border-t border-zinc-200 pt-6">
        <legend className="text-xs font-semibold tracking-wider text-zinc-500 uppercase">
          参加者と負担額
        </legend>
        <p className="text-xs text-zinc-500">
          立替者（
          {mode === "create" ? "あなた" : "上で選んだ人"}
          ）は含めません。精算する人を選び、それぞれの負担額を入力してください。
        </p>

        <div className="rounded-lg border border-zinc-200 bg-white p-3">
          <label
            htmlFor="expense-split-total"
            className="block text-sm font-medium text-zinc-700"
          >
            合計金額から均等割り
          </label>
          <div className="mt-1 flex items-center gap-2">
            <input
              id="expense-split-total"
              type="number"
              min={0}
              step={1}
              inputMode="numeric"
              value={splitTotal}
              onChange={(e) => setSplitTotal(e.target.value)}
              disabled={submitting}
              className="block h-11 w-32 rounded-lg border border-zinc-300 bg-white px-3 text-base text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
              placeholder="例: 12000"
            />
            <span className="text-sm text-zinc-600">円</span>
            <button
              type="button"
              onClick={applyEvenSplit}
              disabled={submitting || orderedSelected.length === 0}
              className="ml-auto inline-flex h-10 items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50 disabled:opacity-50"
            >
              均等割り
            </button>
          </div>
          <p className="mt-1 text-xs text-zinc-500">
            選択中の参加者へ均等に振り分けます（端数は先頭の人から 1 円ずつ）。振り分け後に個別の金額を手直しできます。
          </p>
        </div>

        {candidates.length === 0 ? (
          <p className="text-xs text-zinc-500">
            選択できる参加者がいません。
          </p>
        ) : (
          <ul className="divide-y divide-zinc-200 overflow-hidden rounded-lg border border-zinc-200 bg-white">
            {candidates.map((u) => {
              const checked = selectedSet.has(u.id);
              const wasSettled = settledInitially.has(u.id);
              return (
                <li
                  key={u.id}
                  className="flex items-center gap-3 px-3 py-2"
                >
                  <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-base text-zinc-900">
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleParticipant(u.id)}
                      disabled={submitting}
                      className="size-4 rounded border-zinc-300 text-emerald-700 focus:ring-emerald-600"
                    />
                    <span className="truncate">{u.displayName}</span>
                    {mode === "edit" && wasSettled && (
                      <Badge tone="settled">精算済</Badge>
                    )}
                  </label>
                  {checked && (
                    <div className="flex items-center gap-1">
                      <input
                        type="number"
                        min={0}
                        step={1}
                        inputMode="numeric"
                        value={amounts[u.id] ?? ""}
                        onChange={(e) => setAmount(u.id, e.target.value)}
                        disabled={submitting}
                        aria-label={`${u.displayName} の負担額`}
                        className="block h-10 w-28 rounded-lg border border-zinc-300 bg-white px-3 text-right font-mono text-base tabular-nums text-zinc-900 placeholder-zinc-400 focus:border-emerald-700 focus:outline-none disabled:bg-zinc-100"
                        placeholder="0"
                      />
                      <span className="text-sm text-zinc-600">円</span>
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}

        <p className="text-right text-sm text-zinc-700">
          合計{" "}
          <span className="font-mono font-semibold tabular-nums">
            {formatYen(currentTotal)}
          </span>
        </p>

        {mode === "edit" && (
          <p className="text-xs text-zinc-500">
            精算済みの参加者を外すと、その精算記録も削除されます。
          </p>
        )}
        {removedSettled.length > 0 && (
          <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
            精算済みの{" "}
            {removedSettled.map((u) => u.displayName).join("・")}{" "}
            を外そうとしています。更新するとこの精算記録は削除されます。
          </p>
        )}
      </fieldset>

      {state.kind === "error" && (
        <p className="text-sm text-amber-800">{state.message}</p>
      )}

      <div className="space-y-3 pt-2">
        <PrimaryButton type="submit" disabled={submitting}>
          {submitting ? submittingLabel : submitLabel}
        </PrimaryButton>
        <Link
          href={cancelHref}
          className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-zinc-700 hover:bg-zinc-50"
        >
          キャンセル
        </Link>
      </div>
    </form>
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
      <label
        htmlFor={htmlFor}
        className="block text-sm font-medium text-zinc-700"
      >
        {label}
      </label>
      <div className="mt-1">{children}</div>
    </div>
  );
}
