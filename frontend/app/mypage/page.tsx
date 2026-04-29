import type { Metadata } from "next";

import { MyPageView } from "./_view";

export const metadata: Metadata = {
  title: "マイページ | 謎部",
};

export default function MyPage() {
  return <MyPageView />;
}
