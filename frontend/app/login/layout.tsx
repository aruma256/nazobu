import type { ReactNode } from "react";
import { Noto_Sans_JP } from "next/font/google";

const notoSansJP = Noto_Sans_JP({
  variable: "--font-noto-sans-jp",
  weight: ["400", "500", "700"],
  preload: false,
});

export default function LoginLayout({ children }: { children: ReactNode }) {
  return (
    <div
      className={`${notoSansJP.variable} flex min-h-screen flex-1 flex-col bg-zinc-50 text-zinc-900`}
      style={{
        fontFamily:
          "var(--font-noto-sans-jp), ui-sans-serif, system-ui, sans-serif",
      }}
    >
      {children}
    </div>
  );
}
