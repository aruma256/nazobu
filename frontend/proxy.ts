import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

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

// manifest とそのアイコンはブラウザが Cookie なしで取りに来るため、除外しないと
// /login へリダイレクトされてしまい、PWA のインストール条件を満たせない。
export const config = {
  matcher: [
    "/((?!login|auth|oauth|mcp|\\.well-known|nazobu\\.v1|_next/static|_next/image|.*\\..*).*)",
  ],
};
