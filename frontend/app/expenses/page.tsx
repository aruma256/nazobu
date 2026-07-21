import type { Metadata } from "next";

import { ExpensesView } from "./_view";

export const metadata: Metadata = {
  title: "精算一覧 | 謎部",
};

export default function ExpensesPage() {
  return <ExpensesView />;
}
