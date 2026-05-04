// 共通の再利用 UI 部品。スタイルはここに集約し、
// 個別ページからは意味のある単位（Card / Badge / Section など）で組み合わせる。

"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";

export function PageShell({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-4 pb-16">{children}</main>
  );
}

type NavItem = { href: string; label: string; adminOnly?: boolean };

const NAV_ITEMS: readonly NavItem[] = [
  { href: "/", label: "マイページ" },
  { href: "/events", label: "公演", adminOnly: true },
  { href: "/tickets", label: "全てのチケット" },
] as const;

function isNavActive(pathname: string, href: string): boolean {
  if (href === "/") return pathname === "/";
  return pathname === href || pathname.startsWith(`${href}/`);
}

export function AppHeader({
  brand,
  user,
  isAdmin = false,
}: {
  brand: string;
  user: string;
  isAdmin?: boolean;
}) {
  const pathname = usePathname();
  const navItems = NAV_ITEMS.filter((item) => !item.adminOnly || isAdmin);
  return (
    <header className="sticky top-0 z-10 border-b border-zinc-200 bg-white/85 backdrop-blur-md">
      <div className="mx-auto flex max-w-2xl items-center gap-3 px-4 py-3">
        <Link href="/" className="flex items-center gap-2">
          <span
            aria-hidden
            className="inline-block size-2 rounded-full bg-emerald-600"
          />
          <span className="text-base font-semibold tracking-tight">{brand}</span>
        </Link>
        <nav className="flex items-center gap-1 text-sm">
          {navItems.map(({ href, label }) => {
            const active = isNavActive(pathname, href);
            return (
              <Link
                key={href}
                href={href}
                aria-current={active ? "page" : undefined}
                className={
                  active
                    ? "rounded-md bg-zinc-100 px-2 py-1 font-semibold text-zinc-900"
                    : "rounded-md px-2 py-1 text-zinc-600 hover:text-zinc-900"
                }
              >
                {label}
              </Link>
            );
          })}
        </nav>
        <div className="ml-auto flex items-center gap-3">
          {user !== "" && (
            <span className="max-w-[8rem] truncate text-sm text-zinc-500">
              {user}
            </span>
          )}
          {user !== "" && (
            <form action="/auth/logout" method="post">
              <button
                type="submit"
                className="text-xs text-zinc-500 underline decoration-zinc-300 underline-offset-4 hover:text-zinc-700 hover:decoration-zinc-500"
              >
                ログアウト
              </button>
            </form>
          )}
        </div>
      </div>
    </header>
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

// EventCover は公演 URL から取得した OG 画像を表示するカード上部用カバー。
// 読み込み失敗時は要素ごと隠す（壊れた画像アイコンが残らないように）。
export function EventCover({ src, alt }: { src: string; alt: string }) {
  const [hidden, setHidden] = useState(false);
  if (hidden) return null;
  return (
    <div className="aspect-[1.91/1] w-full overflow-hidden bg-zinc-100">
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
