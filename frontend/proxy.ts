import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// backend の auth.SessionCookieName と一致させる。
const SESSION_COOKIE = "nazobu_session";

export function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;

  if (request.cookies.has(SESSION_COOKIE)) {
    return NextResponse.next();
  }

  const loginUrl = new URL("/login", request.url);
  loginUrl.searchParams.set("next", `${pathname}${search}`);
  return NextResponse.redirect(loginUrl);
}

// session cookie の有無だけ見るので RPC, 認証エンドポイント, Next 内部, 静的ファイルは除外する。
// /oauth・/mcp・/.well-known は MCP 連携（Claude connector）用で、backend 側が
// 認証（OAuth の Bearer / 認可画面のログインリダイレクト）を担う。
// 静的ファイルは拡張子付きパス（`.*\..*`）でまとめて除外する。ここには
// manifest.webmanifest / sw.js / icon-*.png / offline.html / favicon.ico が含まれる。
// manifest とそのアイコンはブラウザが Cookie なしで取りに来るため、除外しないと
// /login へリダイレクトされてしまい、PWA のインストール条件を満たせない。
export const config = {
  matcher: [
    "/((?!login|auth|oauth|mcp|\\.well-known|nazobu\\.v1|_next/static|_next/image|.*\\..*).*)",
  ],
};
