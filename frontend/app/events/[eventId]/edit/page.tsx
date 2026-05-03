import type { Metadata } from "next";

import { EventEditView } from "./_view";

export const metadata: Metadata = {
  title: "公演を編集 | 謎部",
};

export default async function EventEditPage({
  params,
}: {
  params: Promise<{ eventId: string }>;
}) {
  const { eventId } = await params;
  return <EventEditView eventId={eventId} />;
}
