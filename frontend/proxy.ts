import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

// backend の auth.SessionCookieName と一致させる。
const SESSION_COOKIE = "nazobu_session";

// ログイン不要パス。matcher で除外しているのと別に、念のため関数内でも除外する。
const PUBLIC_PATHS = ["/login"];

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
    "/((?!login|auth|nazobu\\.v1|_next/static|_next/image|favicon\\.ico).*)",
  ],
};
