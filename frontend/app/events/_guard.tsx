"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

import { AppHeader, PageShell } from "@/app/_components";
import { canOrganize, redirectToLogin } from "@/app/lib/auth";
import { userClient } from "@/app/lib/rpc";

type GuardState = "checking" | "denied" | "ok";

// /events 配下は公演運営権限（admin / organizer）限定。member は / へ、未ログインは /login へ飛ばす。
// children は権限ありと確定した後にだけ render する。
export function OrganizerGuard({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [state, setState] = useState<GuardState>("checking");

  useEffect(() => {
    let cancelled = false;
    userClient
      .getMe({})
      .then((me) => {
        if (cancelled) return;
        if (!canOrganize(me.role)) {
          setState("denied");
          router.replace("/");
          return;
        }
        setState("ok");
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, pathname);
          return;
        }
        setState("denied");
      });
    return () => {
      cancelled = true;
    };
  }, [router, pathname]);

  if (state === "ok") return <>{children}</>;

  return (
    <>
      <AppHeader brand="謎部" user="" />
      <PageShell>
        <p className="pt-8 text-sm text-zinc-500">
          {state === "checking"
            ? "読み込み中…"
            : "このページは公演を管理できるユーザーのみ利用できます。"}
        </p>
      </PageShell>
    </>
  );
}
