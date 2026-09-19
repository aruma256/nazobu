"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useEffect, useState } from "react";

import type { Event as NazobuEvent, EventTicket } from "@/app/gen/nazobu/v1/event_pb";
import type { GetMeResponse } from "@/app/gen/nazobu/v1/user_pb";
import { eventClient, myPageClient, userClient } from "@/app/lib/rpc";

import {
  AppHeader,
  EventCover,
  Mono,
  PageShell,
  Section,
  SectionTitle,
  UnsettledBanner,
} from "@/app/_components";
import {
  formatDateJa,
  formatYen,
  parseDateTime,
} from "@/app/_format";
import { redirectToLogin } from "@/app/lib/auth";

type LoadState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | {
      kind: "ready";
      me: GetMeResponse;
      events: NazobuEvent[];
      unsettledCount: number;
      receivablesCount: number;
    };

export function EventsView() {
  const router = useRouter();
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [state, setState] = useState<LoadState>({ kind: "loading" });

  async function handleDelete(event: NazobuEvent) {
    if (deletingId || !window.confirm(
      `「${event.title}」を完全に削除しますか？元に戻せません。\nDiscord チャンネル・権限は残ります。`,
    )) return;
    setDeletingId(event.id);
    setDeleteError(null);
    try {
      await eventClient.deleteEvent({ eventId: event.id });
      setState((current) => current.kind === "ready"
        ? { ...current, events: current.events.filter((e) => e.id !== event.id) }
        : current);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, "/events");
        return;
      }
      setDeleteError(err instanceof Error ? err.message : "削除に失敗しました");
    } finally {
      setDeletingId(null);
    }
  }

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      userClient.getMe({}),
      eventClient.listEvents({}),
      myPageClient.listMyUnsettledTickets({}),
      myPageClient.listMyUnsettledReceivables({}),
    ])
      .then(([me, res, unsettled, receivables]) => {
        if (!cancelled)
          setState({
            kind: "ready",
            me,
            events: res.events,
            unsettledCount: unsettled.tickets.length,
            receivablesCount: receivables.tickets.length,
          });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
          redirectToLogin(router, "/events");
          return;
        }
        const message =
          err instanceof Error ? err.message : "データの取得に失敗しました";
        setState({ kind: "error", message });
      });
    return () => {
      cancelled = true;
    };
  }, [router]);

  if (state.kind === "loading") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-zinc-500">読み込み中…</p>
        </PageShell>
      </>
    );
  }

  if (state.kind === "error") {
    return (
      <>
        <AppHeader brand="謎部" user="" />
        <PageShell>
          <p className="pt-8 text-sm text-amber-800">
            読み込みに失敗しました: {state.message}
          </p>
        </PageShell>
      </>
    );
  }

  const { me, events, unsettledCount, receivablesCount } = state;
  const displayName = me.displayName;

  return (
    <>
      <AppHeader brand="謎部" user={displayName} isAdmin={me.role === "admin"} />
      <PageShell>
        <UnsettledBanner
          unsettledCount={unsettledCount}
          receivablesCount={receivablesCount}
        />
        {me.role === "admin" && (
          <Section>
            <Link
              href="/tickets/new"
              className="inline-flex h-11 w-full items-center justify-center rounded-lg bg-emerald-700 px-4 text-sm font-semibold text-white transition-colors hover:bg-emerald-800 active:bg-emerald-900"
            >
              公演と参加チケットを登録
            </Link>
          </Section>
        )}

        <Section>
          <SectionTitle count={events.length}>公演一覧</SectionTitle>
          {deleteError && (
            <p role="alert" className="mt-3 text-sm text-red-700">
              {deleteError}
            </p>
          )}
          {events.length === 0 ? (
            <p className="mt-3 text-sm text-zinc-500">
              まだ公演が登録されていません。
            </p>
          ) : (
            <ul className="mt-3 space-y-4">
              {events.map((e) => (
                <EventCard
                  key={e.id}
                  event={e}
                  myName={displayName}
                  isAdmin={me.role === "admin"}
                  deleting={deletingId !== null}
                  onDelete={() => handleDelete(e)}
                />
              ))}
            </ul>
          )}
        </Section>
      </PageShell>
    </>
  );
}

function EventCard({
  event,
  myName,
  isAdmin,
  deleting,
  onDelete,
}: {
  event: NazobuEvent;
  myName: string;
  isAdmin: boolean;
  deleting: boolean;
  onDelete: () => void;
}) {
  const router = useRouter();
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);
  const [channelUrl, setChannelUrl] = useState("");

  async function joinChannel() {
    if (joining || !window.confirm(`「${event.title}」は参加済みですか？ネタバレを含むチャンネルへの参加を了承しますか？`)) return;
    setJoining(true);
    setJoinError(null);
    try {
      const res = await eventClient.joinEventSpoilerChannel({ eventId: event.id });
      setChannelUrl(res.discordChannelUrl);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        redirectToLogin(router, "/events");
        return;
      }
      setJoinError(err instanceof Error ? err.message : "参加に失敗しました");
    } finally {
      setJoining(false);
    }
  }

  const hasOffsets =
    event.doorsOpenMinutesBefore !== undefined ||
    event.entryDeadlineMinutesBefore !== undefined;
  return (
    <li className="overflow-hidden rounded-2xl border border-zinc-200 bg-white">
      {event.imageUrl !== "" && <EventCover src={event.imageUrl} alt={event.title} />}
      <div className="px-4 pt-4">
        <h3 className="text-base leading-snug font-semibold">{event.title}</h3>
        {event.catchphrase !== "" && (
          <p className="mt-1 text-sm text-zinc-700">{event.catchphrase}</p>
        )}
        {event.url !== "" && (
          <a
            href={event.url}
            target="_blank"
            rel="noreferrer noopener"
            className="mt-1 block truncate text-xs text-emerald-700 underline decoration-zinc-300 underline-offset-4 hover:decoration-emerald-700"
          >
            {event.url}
          </a>
        )}
        {hasOffsets && (
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs text-zinc-900">
            {event.doorsOpenMinutesBefore !== undefined && (
              <>
                <dt>開場</dt>
                <dd>開始 {event.doorsOpenMinutesBefore} 分前</dd>
              </>
            )}
            {event.entryDeadlineMinutesBefore !== undefined && (
              <>
                <dt>締切</dt>
                <dd>開始 {event.entryDeadlineMinutesBefore} 分前</dd>
              </>
            )}
          </dl>
        )}
      </div>

      {event.tickets.length === 0 ? (
        <p className="px-4 pt-3 text-sm text-zinc-500">チケットはまだありません。</p>
      ) : (
        <ul className="mt-3 divide-y divide-zinc-200 border-t border-zinc-200">
          {event.tickets.map((t) => (
            <TicketRow key={t.id} ticket={t} myName={myName} />
          ))}
        </ul>
      )}

      <div className="space-y-3 px-4 pt-4 pb-4">
        {event.hasSpoilerChannel && (channelUrl ? (
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
        ))}
        {joinError && <p role="alert" className="text-sm text-red-700">{joinError}</p>}
        {isAdmin && (
          <>
            <Link
              href={`/events/${event.id}/tickets/new`}
              className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-zinc-200 bg-white px-4 text-sm font-semibold text-emerald-700 hover:bg-zinc-50"
            >
              この公演にチケットを追加
            </Link>
            <button
              type="button"
              onClick={onDelete}
              disabled={deleting || event.tickets.length > 0}
              className="inline-flex h-11 w-full items-center justify-center rounded-lg border border-red-200 px-4 text-sm font-semibold text-red-700 hover:bg-red-50 disabled:opacity-50"
            >
              公演を削除
            </button>
            {event.tickets.length > 0 && (
              <p className="text-sm text-zinc-500">公演を削除するには、先にチケットを削除してください。</p>
            )}
          </>
        )}
      </div>
    </li>
  );
}

function TicketRow({ ticket, myName }: { ticket: EventTicket; myName: string }) {
  const date = parseDateTime(ticket.startAt);
  return (
    <li className="transition-colors hover:bg-zinc-50">
      <Link href={`/tickets/${ticket.id}`} className="block px-4 py-3">
        <div className="flex items-baseline gap-3">
          <Mono className="text-sm font-semibold text-emerald-700">
            {formatDateJa(date)}
          </Mono>
          <Mono className="ml-auto text-sm font-semibold tracking-tight">
            {formatYen(ticket.pricePerPerson)}
          </Mono>
        </div>
        <p className="mt-1 text-xs text-zinc-900">
          立替 {ticket.purchaserName}
        </p>
        {(ticket.participantNames.length > 0 ||
          ticket.unregisteredParticipantsCount > 0) && (
          <p className="mt-1 text-xs text-zinc-900">
            参加{" "}
            {[...ticket.participantNames]
              .sort((a, b) => {
                if (a === myName) return -1;
                if (b === myName) return 1;
                return a.localeCompare(b, "ja");
              })
              .map((name, i) => (
                <Fragment key={i}>
                  {i > 0 && "・"}
                  {name === myName ? (
                    <span className="font-semibold text-emerald-700">{name}</span>
                  ) : (
                    name
                  )}
                </Fragment>
              ))}
            {ticket.unregisteredParticipantsCount > 0 && (
              <span className="text-zinc-500">
                {ticket.participantNames.length > 0 && "・"}
                未登録 {ticket.unregisteredParticipantsCount} 人
              </span>
            )}
          </p>
        )}
      </Link>
    </li>
  );
}
