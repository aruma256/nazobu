import type { Metadata } from "next";

import { TicketDetailView } from "./_view";

export const metadata: Metadata = {
  title: "チケット詳細 | 謎部",
};

export default async function TicketDetailPage({
  params,
}: {
  params: Promise<{ ticketId: string }>;
}) {
  const { ticketId } = await params;
  return <TicketDetailView ticketId={ticketId} />;
}
