import type { useRouter } from "next/navigation";

type Router = ReturnType<typeof useRouter>;

export function redirectToLogin(router: Router, currentPath: string) {
  const next = encodeURIComponent(currentPath);
  router.replace(`/login?next=${next}`);
}
