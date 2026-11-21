import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { EventService } from "@/app/gen/nazobu/v1/event_pb";
import { MyPageService } from "@/app/gen/nazobu/v1/mypage_pb";
import { TicketService } from "@/app/gen/nazobu/v1/ticket_pb";
import { UserService } from "@/app/gen/nazobu/v1/user_pb";

const transport = createConnectTransport({ baseUrl: "/" });

export const userClient = createClient(UserService, transport);
export const myPageClient = createClient(MyPageService, transport);
export const eventClient = createClient(EventService, transport);
export const ticketClient = createClient(TicketService, transport);
