import type { NextConfig } from "next";

const backendURL = process.env.BACKEND_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  // /api/* と /auth/* は backend に proxy する。
  // これにより frontend と同一 origin で扱えるので、Cookie まわりがシンプルになる。
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${backendURL}/api/:path*` },
      { source: "/auth/:path*", destination: `${backendURL}/auth/:path*` },
    ];
  },
};

export default nextConfig;
