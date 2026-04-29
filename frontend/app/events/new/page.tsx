import type { Metadata } from "next";

import { NewEventView } from "./_view";

export const metadata: Metadata = {
  title: "公演を登録 | 謎部",
};

export default function NewEventPage() {
  return <NewEventView />;
}
