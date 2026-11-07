"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { redirectToLogin } from "@/app/lib/auth";
import { eventClient } from "@/app/lib/rpc";

// 公演のネタバレチャンネルに本人だけ参加する。参加後は Discord へのリンクに切り替える。
export function SpoilerChannelJoin({
  eventId,
  eventTitle,
  loginRedirectPath,
}: {
  eventId: string;
  eventTitle: string;
  loginRedirectPath: string;
}) {
  const router = useRouter();
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);
  const [channelUrl, setChannelUrl] = useState("");

  async function joinChannel() {
    if (joining || !window.confirm(`「${eventTitle}」は参加済みですか？ネタバレを含むチャンネルへの参加を了承しますか？`)) return;
    setJoining(true);
    setJoinError(null);
    try {
      const res = await eventClient.joinEventSpoilerChannel({ eventId });
      setChannelUrl(res.discordChannelUrl);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, loginRedirectPath);
        return;
      }
      setJoinError(err instanceof Error ? err.message : "参加に失敗しました");
    } finally {
      setJoining(false);
    }
  }

  return (
    <>
      {channelUrl ? (
        <div role="status">
          <p className="text-sm text-emerald-800">閲覧権限を付与しました。</p>
          <a href={channelUrl} target="_blank" rel="noreferrer noopener" className="inline-flex h-11 items-center text-base font-semibold text-emerald-700 underline">
            Discord で開く
          </a>
        </div>
      ) : (
        <button type="button" onClick={joinChannel} disabled={joining} className="inline-flex min-h-11 w-full items-center justify-center rounded-lg bg-emerald-700 px-4 py-2 text-base font-semibold text-white hover:bg-emerald-800 disabled:opacity-50">
          {joining ? "参加処理中…" : "ネタバレチャンネルに参加"}
        </button>
      )}
      {joinError && <p role="alert" className="text-sm text-red-700">{joinError}</p>}
    </>
  );
}
