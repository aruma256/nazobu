import type { Metadata } from "next";

import { NewTicketForEventView } from "./_view";

export const metadata: Metadata = {
  title: "チケットを登録 | 謎部",
};

export default async function NewTicketForEventPage({
  params,
}: {
  params: Promise<{ eventId: string }>;
}) {
  const { eventId } = await params;
  return <NewTicketForEventView eventId={eventId} />;
}
