const WEEKDAYS_JA = ["日", "月", "火", "水", "木", "金", "土"] as const;

const JST_PARTS = new Intl.DateTimeFormat("en-US", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  weekday: "short",
  hourCycle: "h23",
});

type JSTParts = {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  weekday: number;
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

export function jstCurrentYearMonth(now: Date = new Date()): { year: number; month: number } {
  const p = jstParts(now);
  return { year: p.year, month: p.month };
}

export function toDateInputValue(date: Date): string {
  const p = jstParts(date);
  return `${p.year}-${String(p.month).padStart(2, "0")}-${String(p.day).padStart(2, "0")}`;
}

export function toTimeInputValue(date: Date): string {
  return formatTimeHM(date);
}

export function jstCompactDateTime(ms: number): string {
  const p = jstParts(new Date(ms));
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${p.year}${pad(p.month)}${pad(p.day)}T${pad(p.hour)}${pad(p.minute)}00`;
}

export function joinJSTDateTime(dateValue: string, timeValue: string): string | null {
  if (dateValue === "" || timeValue === "") return null;
  return `${dateValue}T${timeValue}:00+09:00`;
}
