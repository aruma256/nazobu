import type { Metadata } from "next";

import { TicketsView } from "./_view";

export const metadata: Metadata = {
  title: "チケット一覧 | 謎部",
};

export default function TicketsPage() {
  return <TicketsView />;
}
