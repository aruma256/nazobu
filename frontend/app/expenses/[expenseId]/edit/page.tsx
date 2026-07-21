import type { Metadata } from "next";

import { ExpenseEditView } from "./_view";

export const metadata: Metadata = {
  title: "精算を編集 | 謎部",
};

export default async function ExpenseEditPage({
  params,
}: {
  params: Promise<{ expenseId: string }>;
}) {
  const { expenseId } = await params;
  return <ExpenseEditView expenseId={expenseId} />;
}
