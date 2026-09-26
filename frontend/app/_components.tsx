// 共通の再利用 UI 部品。スタイルはここに集約し、
// 個別ページからは意味のある単位（Card / Badge / Section など）で組み合わせる。

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Fragment, useEffect, useRef, useState } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";

import type { Ticket } from "@/app/gen/nazobu/v1/ticket_pb";
import {
  formatDateJa,
  formatTimeHM,
  formatYen,
  parseDateTime,
} from "@/app/_format";

export function PageShell({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-4 pb-[calc(6rem+env(safe-area-inset-bottom))] md:pb-16">{children}</main>
  );
}

const NAV_ITEMS = [
  { href: "/", label: "ダッシュボード", icon: "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z" },
  { href: "/events", label: "公演", icon: "M4 5h16v16H4z M8 3v4 M16 3v4 M4 11h16" },
  { href: "/tickets", label: "チケット", icon: "M3 5h18v5a2 2 0 0 0 0 4v5H3v-5a2 2 0 0 0 0-4z M15 5v3 M15 11v2 M15 16v3" },
] as const;

function isNavActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  return pathname === href || pathname.startsWith(`${href}/`);
}

function Navigation({ mobile = false }: { mobile?: boolean }) {
  const pathname = usePathname();
  return (
    <nav
      aria-label="メインナビゲーション"
      className={mobile
        ? "fixed inset-x-0 bottom-0 z-20 border-t border-zinc-200 bg-white pb-[env(safe-area-inset-bottom)] md:hidden"
        : "hidden md:block"}
    >
      <div className={mobile ? "mx-auto grid max-w-2xl grid-cols-3 px-2 py-1" : "flex gap-1"}>
        {NAV_ITEMS.map(({ href, label, icon }) => {
          const active = isNavActive(pathname, href);
          return (
            <Link
              key={href}
              href={href}
              aria-current={active ? "page" : undefined}
              className={`flex items-center justify-center rounded-lg whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 ${mobile ? "min-h-14 flex-col gap-1 text-xs" : "h-11 px-3 text-sm"} ${active ? "bg-emerald-50 font-semibold text-emerald-700" : "text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900"}`}
            >
              {mobile && (
                <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" className="size-5">
                  <path d={icon} />
                </svg>
              )}
              {label}
            </Link>
          );
        })}
      </div>
    </nav>
  );
}

function AccountMenu({ user, isAdmin }: { user: string; isAdmin: boolean }) {
  const detailsRef = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    function closeOutside(event: PointerEvent) {
      const details = detailsRef.current;
      if (details && event.target instanceof Node && !details.contains(event.target)) details.open = false;
    }
    function closeOnEscape(event: KeyboardEvent) {
      const details = detailsRef.current;
      if (event.key === "Escape" && details?.open) {
        details.open = false;
        details.querySelector("summary")?.focus();
      }
    }
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, []);
  return (
    <details ref={detailsRef} className="relative ml-auto shrink-0" onBlur={(event) => {
      // メニュー内の非フォーカス要素を押しても relatedTarget は null になる。
      // その blur 中に閉じるとブラウザがクラッシュするため、移動先が外部の Node のときだけ閉じる。
      // フォーカス先のない外側のタップは closeOutside で処理する。
      if (event.relatedTarget instanceof Node && !event.currentTarget.contains(event.relatedTarget)) {
        event.currentTarget.open = false;
      }
    }}>
      <summary aria-label="アカウント" className="flex size-11 cursor-pointer list-none items-center justify-center rounded-full border border-zinc-200 bg-zinc-50 text-zinc-600 hover:bg-zinc-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 [&::-webkit-details-marker]:hidden">
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" className="size-5">
          <circle cx="12" cy="8" r="4" />
          <path d="M4 21v-2a8 8 0 0 1 16 0v2" />
        </svg>
      </summary>
      <div className="absolute right-0 top-full mt-2 w-64 max-w-[calc(100vw-2rem)] rounded-xl border border-zinc-200 bg-white p-2 shadow-lg">
        <div className="border-b border-zinc-100 px-3 py-3">
          <p className="text-xs text-zinc-500">ログイン中{isAdmin ? "・管理者" : ""}</p>
          <p className="mt-1 text-sm font-semibold wrap-anywhere">{user}</p>
        </div>
        <form action="/auth/logout" method="post">
          <button type="submit" className="mt-1 flex h-11 w-full items-center rounded-lg px-3 text-sm text-zinc-700 hover:bg-zinc-100 focus-visible:outline-2 focus-visible:outline-emerald-700">ログアウト</button>
        </form>
      </div>
    </details>
  );
}

export function AppHeader({ brand, user, isAdmin = false }: {
  brand: string;
  user: string;
  isAdmin?: boolean;
}) {
  const pathname = usePathname();
  return (
    <>
      <header className="sticky top-0 z-30 border-b border-zinc-200 bg-white/95 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-2xl items-center gap-4 px-4">
          <Link href="/" aria-label={`${brand} ダッシュボード`} className="flex h-11 shrink-0 items-center gap-2 whitespace-nowrap rounded-md focus-visible:outline-2 focus-visible:outline-emerald-700">
            <span aria-hidden="true" className="size-2 rounded-full bg-emerald-600" />
            <span className="text-base font-semibold tracking-tight">{brand}</span>
          </Link>
          <Navigation />
          {user !== "" && <AccountMenu key={pathname} user={user} isAdmin={isAdmin} />}
        </div>
      </header>
      <Navigation mobile />
    </>
  );
}

export function Section({ children }: { children: ReactNode }) {
  return <section className="pt-8 first:pt-6">{children}</section>;
}

export function SectionTitle({
  children,
  count,
}: {
  children: ReactNode;
  count?: number;
}) {
  return (
    <div className="flex items-baseline justify-between">
      <h2 className="text-sm font-semibold tracking-wider text-zinc-700 uppercase">
        {children}
      </h2>
      {typeof count === "number" && (
        <span className="font-mono text-xs tabular-nums text-zinc-500">
          {count} 件
        </span>
      )}
    </div>
  );
}

export function ListCard({ children }: { children: ReactNode }) {
  return (
    <ul className="mt-3 divide-y divide-zinc-200 overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      {children}
    </ul>
  );
}

export function AlertCard({
  title,
  children,
}: {
  title: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="overflow-hidden rounded-2xl border border-amber-300 bg-amber-50">
      <div className="flex items-center gap-2 px-4 pt-4">
        <WarnIcon />
        <h2 className="text-sm font-semibold text-amber-900">{title}</h2>
      </div>
      <div className="space-y-3 p-4">{children}</div>
    </div>
  );
}

export function AlertItem({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-xl border border-amber-200 bg-white p-4">
      {children}
    </div>
  );
}

export function PrimaryButton({
  children,
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={`inline-flex h-11 w-full items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900 ${className}`}
      {...props}
    >
      {children}
    </button>
  );
}

const BADGE_TONES = {
  settled: "bg-zinc-100 text-zinc-600",
  unsettled: "bg-amber-50 text-amber-800",
  muted: "bg-zinc-100 text-zinc-600",
} as const;

export type BadgeTone = keyof typeof BADGE_TONES;

export function Badge({
  tone = "muted",
  children,
}: {
  tone?: BadgeTone;
  children: ReactNode;
}) {
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${BADGE_TONES[tone]}`}
    >
      {children}
    </span>
  );
}

// 数字や日付など、桁ズレが嫌なテキスト用。
export function Mono({
  className = "",
  children,
}: {
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={`font-mono tabular-nums ${className}`}>{children}</span>
  );
}

// EventCover は公演 URL から取得した OG 画像を表示する。
// variant="top" はカード上部の横幅いっぱいのカバー、
// variant="side" は flex 行内で左側に並べる縦長サムネイル（高さは行に追従）。
// 読み込み失敗時は要素ごと隠す（壊れた画像アイコンが残らないように）。
export function EventCover({
  src,
  alt,
  variant = "top",
}: {
  src: string;
  alt: string;
  variant?: "top" | "side";
}) {
  const [hidden, setHidden] = useState(false);
  if (hidden) return null;
  const wrapperClass =
    variant === "side"
      ? "w-24 flex-none self-stretch overflow-hidden rounded-lg bg-zinc-100 sm:w-32"
      : "aspect-[1.91/1] w-full overflow-hidden bg-zinc-100";
  return (
    <div className={wrapperClass}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={src}
        alt={alt}
        loading="lazy"
        decoding="async"
        referrerPolicy="no-referrer"
        onError={() => setHidden(true)}
        className="h-full w-full object-cover"
      />
    </div>
  );
}

// TicketCard は /tickets と mypage（未精算 / 今後の予定）で使う共通カード。
// tone="alert" は amber 系のトーンで未精算カードに使う。
export type TicketCardTone = "default" | "alert";

export function TicketCard({
  ticket,
  myName,
  tone = "default",
}: {
  ticket: Ticket;
  myName: string;
  tone?: TicketCardTone;
}) {
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
  // 未登録の同行者も定員の枠を消費するため、占有数に含める。
  const occupiedCount =
    ticket.participantNames.length + ticket.unregisteredParticipantsCount;
  const wrapperClass =
    tone === "alert"
      ? "overflow-hidden rounded-2xl border border-amber-300 bg-amber-50 transition-colors hover:bg-amber-100"
      : "overflow-hidden rounded-2xl border border-zinc-200 bg-white transition-colors hover:bg-zinc-50";
  const dateClass =
    tone === "alert"
      ? "text-sm font-semibold text-amber-800"
      : "text-sm font-semibold text-emerald-700";
  return (
    <li className={wrapperClass}>
      <Link
        href={`/tickets/${ticket.id}`}
        className="flex items-stretch gap-3 p-3"
      >
        {ticket.eventImageUrl !== "" && (
          <EventCover
            src={ticket.eventImageUrl}
            alt={ticket.eventTitle}
            variant="side"
          />
        )}
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-3">
            <Mono className={dateClass}>{formatDateJa(startAt)}</Mono>
            <Mono className="ml-auto text-sm font-semibold tracking-tight">
              {formatYen(ticket.pricePerPerson)}
            </Mono>
          </div>
          <h3 className="pt-1 text-base leading-snug font-semibold">
            {ticket.eventTitle}
          </h3>
          {ticket.eventCatchphrase !== "" && (
            <p className="pt-0.5 text-xs text-zinc-700">
              {ticket.eventCatchphrase}
            </p>
          )}
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 pt-3 text-xs text-zinc-900">
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
            <dt>定員</dt>
            <dd>
              <Mono>
                {occupiedCount}/{ticket.maxParticipants}
              </Mono>
              {occupiedCount < ticket.maxParticipants && (
                <span className="ml-2 text-amber-800">
                  （残り
                  <Mono className="font-semibold">
                    {ticket.maxParticipants - occupiedCount}
                  </Mono>
                  ）
                </span>
              )}
            </dd>
            {occupiedCount > 0 && (
              <>
                <dt>参加</dt>
                <dd>
                  {[...ticket.participantNames]
                    .sort((a, b) => {
                      if (a === myName) return -1;
                      if (b === myName) return 1;
                      return a.localeCompare(b, "ja");
                    })
                    .map((name, i) => (
                      <Fragment key={i}>
                        {i > 0 && "・"}
                        {name === myName ? (
                          <span className="font-semibold text-emerald-700">
                            {name}
                          </span>
                        ) : (
                          name
                        )}
                      </Fragment>
                    ))}
                  {ticket.unregisteredParticipantsCount > 0 && (
                    <span className="text-zinc-500">
                      {ticket.participantNames.length > 0 && "・"}
                      未登録{" "}
                      <Mono>{ticket.unregisteredParticipantsCount}</Mono> 人
                    </span>
                  )}
                </dd>
              </>
            )}
          </dl>
        </div>
      </Link>
    </li>
  );
}

// 未精算 / 未回収のチケットがある場合に上部に出す 1 行リンクバナー。
// /tickets, /events 等、ダッシュボード以外のページからユーザーをダッシュボードへ誘導する。
export function UnsettledBanner({
  unsettledCount,
  receivablesCount,
}: {
  unsettledCount: number;
  receivablesCount: number;
}) {
  if (unsettledCount === 0 && receivablesCount === 0) return null;
  const parts: string[] = [];
  if (unsettledCount > 0) parts.push(`未精算 ${unsettledCount} 件`);
  if (receivablesCount > 0) parts.push(`未回収 ${receivablesCount} 件`);
  return (
    <Link
      href="/"
      className="mt-4 flex h-11 items-center gap-2 rounded-lg border border-amber-300 bg-amber-50 px-4 text-sm font-semibold text-amber-900 hover:bg-amber-100"
    >
      <WarnIcon />
      <span className="flex-1 truncate">{parts.join("・")}があります</span>
      <span aria-hidden className="text-amber-700">›</span>
    </Link>
  );
}

function WarnIcon() {
  return (
    <svg
      aria-hidden
      viewBox="0 0 20 20"
      className="size-4 text-amber-700"
      fill="currentColor"
    >
      <path
        fillRule="evenodd"
        d="M9.401 2.927a.75.75 0 0 1 1.198 0l7.25 10.5a.75.75 0 0 1-.6 1.198H2.751a.75.75 0 0 1-.6-1.198l7.25-10.5ZM10 7a.75.75 0 0 1 .75.75v3a.75.75 0 0 1-1.5 0v-3A.75.75 0 0 1 10 7Zm0 7a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z"
        clipRule="evenodd"
      />
    </svg>
  );
}
