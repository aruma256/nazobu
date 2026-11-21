import type { Metadata, Viewport } from "next";
import { Geist_Mono, Noto_Sans_JP } from "next/font/google";
import "./globals.css";
import { ServiceWorkerRegistrar } from "./_service-worker";

const notoSansJP = Noto_Sans_JP({
  variable: "--font-noto-sans-jp",
  weight: ["400", "500", "700"],
  preload: false,
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "謎部",
  appleWebApp: {
    title: "謎部",
    capable: true,
  },
};

export const viewport: Viewport = {
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
