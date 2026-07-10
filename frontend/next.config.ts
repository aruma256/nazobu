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
      // Claude connector（remote MCP）連携。OAuth メタデータ / 認可 / トークン / MCP 本体を
      // backend に proxy して、外部からは frontend と同一 origin に見せる。
      { source: "/mcp", destination: `${backendURL}/mcp` },
      { source: "/oauth/:path*", destination: `${backendURL}/oauth/:path*` },
      {
        source: "/.well-known/oauth-authorization-server",
        destination: `${backendURL}/.well-known/oauth-authorization-server`,
      },
      {
        source: "/.well-known/oauth-protected-resource/:path*",
        destination: `${backendURL}/.well-known/oauth-protected-resource/:path*`,
      },
      {
        source: "/.well-known/oauth-protected-resource",
        destination: `${backendURL}/.well-known/oauth-protected-resource`,
      },
    ];
  },
};

export default nextConfig;
