import type { Metadata } from "next";

import { EventsView } from "./_view";

export const metadata: Metadata = {
  title: "公演一覧 | 謎部",
};

export default function EventsPage() {
  return <EventsView />;
}
