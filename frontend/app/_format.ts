// 共通フォーマッタ。
// バックエンドの attended_on は JST のカレンダー日付 ("YYYY-MM-DD") なので、
// new Date(s) で UTC 解釈されて 1 日ずれることが無いよう、明示的に local Date に組み立てる。

const WEEKDAYS_JA = ["日", "月", "火", "水", "木", "金", "土"] as const;

export function parseAttendedOn(yyyymmdd: string): Date {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  return new Date(y, m - 1, d);
}

export function formatDateJa(date: Date): string {
  const m = date.getMonth() + 1;
  const d = date.getDate();
  const w = WEEKDAYS_JA[date.getDay()];
  return `${m}/${d} (${w})`;
}

export function formatMonoDate(date: Date): string {
  const m = (date.getMonth() + 1).toString().padStart(2, "0");
  const d = date.getDate().toString().padStart(2, "0");
  return `${m}/${d}`;
}

export function formatYen(amount: number): string {
  return `¥${amount.toLocaleString("ja-JP")}`;
}

export function daysFromToday(date: Date, today: Date): number {
  const oneDay = 24 * 60 * 60 * 1000;
  const a = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
  const b = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  return Math.round((b - a) / oneDay);
}
