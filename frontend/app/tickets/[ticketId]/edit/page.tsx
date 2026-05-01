import type { Metadata } from "next";

import { TicketEditView } from "./_view";

export const metadata: Metadata = {
  title: "チケットを編集 | 謎部",
};

export default async function TicketEditPage({
  params,
}: {
  params: Promise<{ ticketId: string }>;
}) {
  const { ticketId } = await params;
  return <TicketEditView ticketId={ticketId} />;
}
