import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// backend の auth.SessionCookieName と一致させる。
const SESSION_COOKIE = "nazobu_session";

// ログイン不要パス。matcher で除外しているのと別に、念のため関数内でも除外する。
// /oauth・/mcp・/.well-known は MCP 連携（Claude connector）用で、backend 側が
// 認証（OAuth の Bearer / 認可画面のログインリダイレクト）を担うためここでは素通しする。
const PUBLIC_PATHS = ["/login", "/oauth", "/mcp", "/.well-known"];

export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;

  if (PUBLIC_PATHS.some((p) => pathname === p || pathname.startsWith(`${p}/`))) {
    return NextResponse.next();
  }

  if (request.cookies.has(SESSION_COOKIE)) {
    return NextResponse.next();
  }

  const loginUrl = new URL("/login", request.url);
  loginUrl.searchParams.set("next", `${pathname}${search}`);
  return NextResponse.redirect(loginUrl);
}

// session cookie の有無だけ見るので RPC, 認証エンドポイント, Next 内部, 静的ファイルは除外する。
export const config = {
  matcher: [
    "/((?!login|auth|oauth|mcp|\\.well-known|nazobu\\.v1|_next/static|_next/image|favicon\\.ico).*)",
  ],
};
