import type { Metadata } from "next";

import { HomeView } from "./_view";

export const metadata: Metadata = {
  title: "マイページ | 謎部",
};

export default function HomePage() {
  return <HomeView />;
}
