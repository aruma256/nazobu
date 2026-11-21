import type { NextConfig } from "next";

const backendURL = process.env.BACKEND_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  async headers() {
    return [
      {
        source: "/sw.js",
        headers: [
          {
            key: "Cache-Control",
            value: "no-cache, no-store, must-revalidate",
          },
        ],
      },
    ];
  },

  async rewrites() {
    return [
      {
        source: "/nazobu.v1.:service/:method",
        destination: `${backendURL}/nazobu.v1.:service/:method`,
      },
      { source: "/auth/:path*", destination: `${backendURL}/auth/:path*` },
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
