// マイページ表示確認用のダミーデータ。バックエンド実装が入ったら置き換える。

export type SampleUser = {
  displayName: string;
  username: string;
};

export type SampleSettlement = {
  id: string;
  performanceTitle: string;
  amount: number;
  payee: string;
  attendedOn: Date;
  dueOn: Date;
};

export type SampleUpcoming = {
  id: string;
  organizer: string;
  title: string;
  venue: string;
  startsAt: Date;
  durationMinutes: number;
  members: string[];
};

export type SampleArchive = {
  id: string;
  title: string;
  organizer: string;
  attendedOn: Date;
  settled: boolean;
};

export const sampleUser: SampleUser = {
  displayName: "あるま",
  username: "aruma256",
};

export const sampleSettlements: SampleSettlement[] = [
  {
    id: "stl_01HQ0AB3K9",
    performanceTitle: "深淵の図書館",
    amount: 4800,
    payee: "ヒロ",
    attendedOn: new Date("2026-04-25T14:00:00+09:00"),
    dueOn: new Date("2026-05-05T23:59:00+09:00"),
  },
];

export const sampleUpcoming: SampleUpcoming[] = [
  {
    id: "evt_01HQ1A0001",
    organizer: "SCRAP",
    title: "タイムトラベル探偵団",
    venue: "新宿アジト",
    startsAt: new Date("2026-05-03T14:00:00+09:00"),
    durationMinutes: 90,
    members: ["ヒロ", "ミナ", "タケ"],
  },
  {
    id: "evt_01HQ1A0002",
    organizer: "よだかのレコード",
    title: "螺旋の研究室",
    venue: "渋谷",
    startsAt: new Date("2026-05-09T19:00:00+09:00"),
    durationMinutes: 100,
    members: ["ミナ", "りん"],
  },
  {
    id: "evt_01HQ1A0003",
    organizer: "NAZO×NAZO劇団",
    title: "真夜中のサーカス",
    venue: "池袋",
    startsAt: new Date("2026-05-15T19:30:00+09:00"),
    durationMinutes: 110,
    members: ["タケ", "ハル", "ヒロ"],
  },
  {
    id: "evt_01HQ1A0004",
    organizer: "SCRAP",
    title: "失われた古城のなぞ",
    venue: "新宿",
    startsAt: new Date("2026-05-23T13:00:00+09:00"),
    durationMinutes: 120,
    members: ["りん", "ミナ", "ハル"],
  },
  {
    id: "evt_01HQ1A0005",
    organizer: "タンブルウィード",
    title: "孤島の灯台",
    venue: "高田馬場",
    startsAt: new Date("2026-06-06T14:00:00+09:00"),
    durationMinutes: 90,
    members: ["ヒロ", "タケ"],
  },
];

export const sampleArchive: SampleArchive[] = [
  {
    id: "evt_01HP9X0001",
    title: "夜行列車の謎",
    organizer: "SCRAP",
    attendedOn: new Date("2026-04-25T14:00:00+09:00"),
    settled: false,
  },
  {
    id: "evt_01HP9X0002",
    title: "廃墟と少女",
    organizer: "よだかのレコード",
    attendedOn: new Date("2026-04-18T19:00:00+09:00"),
    settled: true,
  },
  {
    id: "evt_01HP9X0003",
    title: "水曜日の魔法陣",
    organizer: "NAZO×NAZO劇団",
    attendedOn: new Date("2026-04-12T13:00:00+09:00"),
    settled: true,
  },
  {
    id: "evt_01HP9X0004",
    title: "銀河鉄道の終着駅",
    organizer: "タンブルウィード",
    attendedOn: new Date("2026-04-05T15:00:00+09:00"),
    settled: true,
  },
];

const WEEKDAYS_JA = ["日", "月", "火", "水", "木", "金", "土"] as const;

export function formatDateJa(date: Date): string {
  const m = date.getMonth() + 1;
  const d = date.getDate();
  const w = WEEKDAYS_JA[date.getDay()];
  return `${m}/${d} (${w})`;
}

export function formatTimeJa(date: Date): string {
  const h = date.getHours().toString().padStart(2, "0");
  const m = date.getMinutes().toString().padStart(2, "0");
  return `${h}:${m}`;
}

export function formatYen(amount: number): string {
  return `¥${amount.toLocaleString("ja-JP")}`;
}

export function formatMonoDate(date: Date): string {
  const m = (date.getMonth() + 1).toString().padStart(2, "0");
  const d = date.getDate().toString().padStart(2, "0");
  return `${m}/${d}`;
}

export function daysFromToday(date: Date, today: Date): number {
  const oneDay = 24 * 60 * 60 * 1000;
  const a = new Date(today.getFullYear(), today.getMonth(), today.getDate()).getTime();
  const b = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  return Math.round((b - a) / oneDay);
}
