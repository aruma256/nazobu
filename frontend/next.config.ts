import type { NextConfig } from "next";

const backendURL = process.env.BACKEND_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  // Connect RPC (/nazobu.v1.*) と /auth/* は backend に proxy する。
  // これにより frontend と同一 origin で扱えるので、Cookie まわりがシンプルになる。
  async rewrites() {
    return [
      {
        source: "/nazobu.v1.:service/:method",
        destination: `${backendURL}/nazobu.v1.:service/:method`,
      },
      { source: "/auth/:path*", destination: `${backendURL}/auth/:path*` },
    ];
  },
};

export default nextConfig;
