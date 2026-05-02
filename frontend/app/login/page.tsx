import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "ログイン | 謎部",
};

// next は内部パス（`/` 始まりかつ `//` で始まらない）のみ許可。
// 不正値は捨てて `/` 扱いにすることで open redirect の踏み台にしない。
function sanitizeNext(raw: string | string[] | undefined): string {
  if (typeof raw !== "string") return "/";
  if (!raw.startsWith("/")) return "/";
  if (raw.startsWith("//")) return "/";
  return raw;
}

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ [key: string]: string | string[] | undefined }>;
}) {
  const params = await searchParams;
  const next = sanitizeNext(params.next);
  const loginHref = `/auth/discord/login?next=${encodeURIComponent(next)}`;

  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col items-center justify-center gap-6 px-4 py-12">
      <div className="flex flex-col items-center gap-2">
        <span
          aria-hidden
          className="inline-block size-3 rounded-full bg-emerald-600"
        />
        <h1 className="text-2xl font-semibold tracking-tight">謎部</h1>
      </div>

      <a
        href={loginHref}
        className="inline-flex h-11 items-center justify-center rounded-lg bg-emerald-700 px-6 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900"
      >
        Discord でログイン
      </a>
    </main>
  );
}
