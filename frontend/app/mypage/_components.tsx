// マイページの再利用 UI 部品。スタイルはここに集約し、
// 個別ページからは意味のある単位（Card / Badge / Section など）で組み合わせる。

import type { ButtonHTMLAttributes, ReactNode } from "react";

export function PageShell({ children }: { children: ReactNode }) {
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-4 pb-16">{children}</main>
  );
}

export function AppHeader({
  brand,
  user,
}: {
  brand: string;
  user: string;
}) {
  return (
    <header className="sticky top-0 z-10 border-b border-zinc-200 bg-white/85 backdrop-blur-md">
      <div className="mx-auto flex max-w-2xl items-center justify-between px-4 py-3">
        <div className="flex items-center gap-2">
          <span
            aria-hidden
            className="inline-block size-2 rounded-full bg-emerald-600"
          />
          <span className="text-base font-semibold tracking-tight">{brand}</span>
        </div>
        <span className="text-sm text-zinc-500">{user}</span>
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
