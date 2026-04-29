import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { UserService } from "@/app/gen/nazobu/v1/user_pb";

// 同一 origin（next.config.ts の rewrites 経由で backend へ proxy される）。
// credentials: "include" にして session cookie を必ず付ける。
const transport = createConnectTransport({
  baseUrl: "/",
  fetch: (input, init) => fetch(input, { ...init, credentials: "same-origin" }),
});

export const userClient = createClient(UserService, transport);
