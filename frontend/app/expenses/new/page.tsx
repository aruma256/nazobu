import type { Metadata } from "next";

import { NewExpenseView } from "./_view";

export const metadata: Metadata = {
  title: "精算を登録 | 謎部",
};

export default async function NewExpensePage({
  searchParams,
}: {
  searchParams: Promise<{ [key: string]: string | string[] | undefined }>;
}) {
  const params = await searchParams;
  const raw = params.ticketId;
  const ticketId = typeof raw === "string" ? raw : "";
  return <NewExpenseView initialTicketId={ticketId} />;
}
