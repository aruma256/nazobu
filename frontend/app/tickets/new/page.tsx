import type { Metadata } from "next";

import { NewTicketView } from "./_view";

export const metadata: Metadata = {
  title: "公演と参加チケットを登録 | 謎部",
};

export default function NewTicketPage() {
  return <NewTicketView />;
}
