// 共通フォーマッタ。
// バックエンドの start_at / meeting_at は JST オフセット付き RFC3339
// （例: "2026-04-15T19:00:00+09:00"）。Date オブジェクトに直して渡す。

const WEEKDAYS_JA = ["日", "月", "火", "水", "木", "金", "土"] as const;

// JST 固定の週・月・日付フォーマット。実行環境のローカル TZ に依存させない。
const JST_PARTS = new Intl.DateTimeFormat("en-US", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  weekday: "short",
  hour12: false,
});

type JSTParts = {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  weekday: number; // 0=Sun..6=Sat
};

const WEEKDAY_INDEX: Record<string, number> = {
  Sun: 0, Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6,
};

function jstParts(date: Date): JSTParts {
  const parts = JST_PARTS.formatToParts(date);
  const get = (type: string): string =>
    parts.find((p) => p.type === type)?.value ?? "";
  return {
    year: Number(get("year")),
    month: Number(get("month")),
    day: Number(get("day")),
    hour: Number(get("hour")),
    minute: Number(get("minute")),
    weekday: WEEKDAY_INDEX[get("weekday")] ?? 0,
  };
}

// RFC3339 文字列を Date に変換する。
// 受け取り側のローカル TZ に依らず、絶対時刻として解釈される。
export function parseDateTime(rfc3339: string): Date {
  return new Date(rfc3339);
}

export function formatDateJa(date: Date): string {
  const p = jstParts(date);
  return `${p.month}/${p.day} (${WEEKDAYS_JA[p.weekday]})`;
}

export function formatMonoDate(date: Date): string {
  const p = jstParts(date);
  return `${String(p.month).padStart(2, "0")}/${String(p.day).padStart(2, "0")}`;
}

export function formatTimeHM(date: Date): string {
  const p = jstParts(date);
  return `${String(p.hour).padStart(2, "0")}:${String(p.minute).padStart(2, "0")}`;
}

export function formatYen(amount: number): string {
  return `¥${amount.toLocaleString("ja-JP")}`;
}

// JST 基準のカレンダー日付差。同じ日 = 0 / 翌日 = 1。
export function daysFromToday(date: Date, today: Date): number {
  const a = jstParts(today);
  const b = jstParts(date);
  const aMs = Date.UTC(a.year, a.month - 1, a.day);
  const bMs = Date.UTC(b.year, b.month - 1, b.day);
  return Math.round((bMs - aMs) / (24 * 60 * 60 * 1000));
}

// <input type="date"> 用の "YYYY-MM-DD"（JST）。
export function toDateInputValue(date: Date): string {
  const p = jstParts(date);
  return `${p.year}-${String(p.month).padStart(2, "0")}-${String(p.day).padStart(2, "0")}`;
}

// <input type="time"> 用の "HH:MM"（JST）。
export function toTimeInputValue(date: Date): string {
  return formatTimeHM(date);
}

// <input type="date"> + <input type="time"> から JST RFC3339 を組み立てる。
// time が空文字なら null を返す（呼び出し側で「未設定」を判定しやすくするため）。
export function joinJSTDateTime(dateValue: string, timeValue: string): string | null {
  if (dateValue === "" || timeValue === "") return null;
  return `${dateValue}T${timeValue}:00+09:00`;
}
