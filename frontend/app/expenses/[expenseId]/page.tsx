import type { Metadata } from "next";

import { ExpenseDetailView } from "./_view";

export const metadata: Metadata = {
  title: "精算の詳細 | 謎部",
};

export default async function ExpenseDetailPage({
  params,
}: {
  params: Promise<{ expenseId: string }>;
}) {
  const { expenseId } = await params;
  return <ExpenseDetailView expenseId={expenseId} />;
}
