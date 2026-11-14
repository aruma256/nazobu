import type { Metadata, Viewport } from "next";
import { Geist_Mono, Noto_Sans_JP } from "next/font/google";
import "./globals.css";
import { ServiceWorkerRegistrar } from "./_service-worker";

// 本文フォント。全ページ共通なのでルートで 1 度だけ読み込む。
const notoSansJP = Noto_Sans_JP({
  variable: "--font-noto-sans-jp",
  weight: ["400", "500", "700"],
  preload: false,
});

// 数字・日付・金額用（Mono コンポーネント / font-mono）。
const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "謎部",
  // PWA インストール時のホーム画面表示名
  appleWebApp: {
    title: "謎部",
    capable: true,
  },
};

export const viewport: Viewport = {
  // AppHeader（白 + backdrop-blur）に合わせてステータスバーも白にする
  themeColor: "#ffffff",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="ja"
      className={`${notoSansJP.variable} ${geistMono.variable} h-full antialiased`}
    >
      <body className="flex min-h-screen flex-col bg-zinc-50 text-zinc-900">
        {children}
        <ServiceWorkerRegistrar />
      </body>
    </html>
  );
}
