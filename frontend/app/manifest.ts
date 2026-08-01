import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "謎部",
    short_name: "謎部",
    description: "謎解き仲間のための参加・精算管理",
    lang: "ja",
    start_url: "/",
    display: "standalone",
    // zinc-50（アプリ背景）と白（ヘッダー）に合わせる
    background_color: "#fafafa",
    theme_color: "#ffffff",
    icons: [
      { src: "/icon-192.png", sizes: "192x192", type: "image/png" },
      { src: "/icon-512.png", sizes: "512x512", type: "image/png" },
      // maskable はセーフゾーン込みで同一デザインを流用
      {
        src: "/icon-192.png",
        sizes: "192x192",
        type: "image/png",
        purpose: "maskable",
      },
      {
        src: "/icon-512.png",
        sizes: "512x512",
        type: "image/png",
        purpose: "maskable",
      },
    ],
  };
}
