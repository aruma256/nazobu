import type { useRouter } from "next/navigation";

type Router = ReturnType<typeof useRouter>;

// session cookie が無い未ログイン状態は proxy.ts が /login に飛ばすが、
// cookie はあるが期限切れ等で backend が Unauthenticated を返すケースは
// クライアント側でこの関数に拾わせて /login?next=... へ誘導する。
export function redirectToLogin(router: Router, currentPath: string) {
  const next = encodeURIComponent(currentPath);
  router.replace(`/login?next=${next}`);
}
