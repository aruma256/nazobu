package server

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/oauth"
)

// newMCPHandler は Claude connector 向けの MCP（Streamable HTTP）ハンドラを組み立てる。
// 認証は前段の oauth.Middleware が済ませ、auth.User を context に注入してくる前提。
// ツール実装は既存の Connect RPC ハンドラを in-process 呼び出しして再利用する
// （lookupSessionUser が context の user を優先するため cookie なしで通る）。
func newMCPHandler(
	mypage nazobuv1connect.MyPageServiceHandler,
	tickets nazobuv1connect.TicketServiceHandler,
	users nazobuv1connect.UserServiceHandler,
) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "nazobu",
		Title:   "謎部",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_my_upcoming_tickets",
		Description: "ログインユーザー自身の、今後参加予定の謎解き公演チケット一覧を取得する。" +
			"開演日時・集合時刻・集合場所・同行者・一人あたりの参加費（円）を含む。日時は JST の RFC3339 形式。",
	}, listMyUpcomingTicketsTool(mypage))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "list_users",
		Description: "謎部の登録メンバー一覧（user_id と表示名）を取得する。" +
			"create_ticket_with_event の participant_user_ids を指定する前に、このツールで ID を確認する。",
	}, listUsersTool(users))

	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_ticket_with_event",
		Description: "新しい謎解き公演とそのチケットをまとめて 1 件登録する（web の新規登録と同じ動線）。" +
			"チケットの立替者（購入者）はログイン中のユーザー自身になる。admin ロールと write スコープが必要。" +
			"日時は JST の RFC3339 形式（例 2026-08-01T14:00:00+09:00）で指定する。",
	}, createTicketWithEventTool(tickets))

	// Stateless + JSONResponse: セッション管理を持たず、SSE ではなく素の JSON で応答する。
	// Cloudflare Tunnel + Next.js rewrites 越しでもバッファリングの影響を受けない構成に寄せる。
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}

// mcpTicket は MCP ツールが返すチケット情報。proto の Ticket から
// LLM が扱いやすいフィールドだけを抜き出した形。
type mcpTicket struct {
	TicketID         string   `json:"ticket_id" jsonschema:"チケット ID"`
	EventTitle       string   `json:"event_title" jsonschema:"公演タイトル"`
	EventURL         string   `json:"event_url" jsonschema:"公演の公式ページ URL"`
	StartAt          string   `json:"start_at" jsonschema:"開演日時（JST, RFC3339）"`
	MeetingAt        string   `json:"meeting_at,omitempty" jsonschema:"集合日時（JST, RFC3339）。未定なら空"`
	MeetingPlace     string   `json:"meeting_place,omitempty" jsonschema:"集合場所。未設定なら空"`
	PricePerPerson   int32    `json:"price_per_person" jsonschema:"一人あたりの参加費（税込・円）"`
	PurchaserName    string   `json:"purchaser_name" jsonschema:"チケットを立て替え購入したメンバーの表示名"`
	ParticipantNames []string `json:"participant_names" jsonschema:"参加メンバーの表示名一覧"`
}

// toMCPTicket は proto の Ticket を MCP ツール出力用の形に変換する。
func toMCPTicket(t *nazobuv1.Ticket) mcpTicket {
	return mcpTicket{
		TicketID:         t.GetId(),
		EventTitle:       t.GetEventTitle(),
		EventURL:         t.GetEventUrl(),
		StartAt:          t.GetStartAt(),
		MeetingAt:        t.GetMeetingAt(),
		MeetingPlace:     t.GetMeetingPlace(),
		PricePerPerson:   t.GetPricePerPerson(),
		PurchaserName:    t.GetPurchaserName(),
		ParticipantNames: t.GetParticipantNames(),
	}
}

type listMyUpcomingTicketsOutput struct {
	Tickets []mcpTicket `json:"tickets" jsonschema:"今後参加予定のチケット一覧（開演日時の昇順）"`
}

func listMyUpcomingTicketsTool(mypage nazobuv1connect.MyPageServiceHandler) mcp.ToolHandlerFor[struct{}, listMyUpcomingTicketsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listMyUpcomingTicketsOutput, error) {
		var out listMyUpcomingTicketsOutput
		res, err := mypage.ListMyUpcomingTickets(ctx, connect.NewRequest(&nazobuv1.ListMyUpcomingTicketsRequest{}))
		if err != nil {
			return nil, out, err
		}
		out.Tickets = make([]mcpTicket, 0, len(res.Msg.GetTickets()))
		for _, t := range res.Msg.GetTickets() {
			out.Tickets = append(out.Tickets, toMCPTicket(t))
		}
		return nil, out, nil
	}
}

type mcpUser struct {
	UserID      string `json:"user_id" jsonschema:"user の ID（participant_user_ids の指定に使う）"`
	DisplayName string `json:"display_name" jsonschema:"表示名"`
}

type listUsersOutput struct {
	Users []mcpUser `json:"users" jsonschema:"謎部の登録メンバー一覧"`
}

func listUsersTool(users nazobuv1connect.UserServiceHandler) mcp.ToolHandlerFor[struct{}, listUsersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listUsersOutput, error) {
		var out listUsersOutput
		res, err := users.ListUsers(ctx, connect.NewRequest(&nazobuv1.ListUsersRequest{}))
		if err != nil {
			return nil, out, err
		}
		out.Users = make([]mcpUser, 0, len(res.Msg.GetUsers()))
		for _, u := range res.Msg.GetUsers() {
			out.Users = append(out.Users, mcpUser{
				UserID:      u.GetId(),
				DisplayName: u.GetDisplayName(),
			})
		}
		return nil, out, nil
	}
}

// createTicketWithEventInput は CreateTicketWithEventRequest の MCP 向けミラー。
// バリデーション（必須・上限・整合性）は RPC ハンドラ側に集約されているため、ここでは行わない。
type createTicketWithEventInput struct {
	EventTitle                      string   `json:"event_title" jsonschema:"公演タイトル（必須）"`
	EventURL                        string   `json:"event_url" jsonschema:"公演の公式ページ URL（必須、http(s)）"`
	EventCatchphrase                string   `json:"event_catchphrase,omitempty" jsonschema:"公演のキャッチコピー。省略すると公式ページの og:description から自動補完されることがある"`
	EventDoorsOpenMinutesBefore     *int32   `json:"event_doors_open_minutes_before,omitempty" jsonschema:"開場が開演の何分前か（0 以上）。不明なら省略"`
	EventEntryDeadlineMinutesBefore *int32   `json:"event_entry_deadline_minutes_before,omitempty" jsonschema:"入場締切が開演の何分前か（0 以上）。不明なら省略"`
	EventExpectedDurationMinutes    int32    `json:"event_expected_duration_minutes" jsonschema:"想定所要時間（分、1 以上、必須）"`
	StartAt                         string   `json:"start_at" jsonschema:"開演日時（JST, RFC3339、必須）"`
	MeetingAt                       string   `json:"meeting_at,omitempty" jsonschema:"集合日時（JST, RFC3339）。未定なら省略"`
	MeetingPlace                    string   `json:"meeting_place,omitempty" jsonschema:"集合場所。未定なら省略"`
	PricePerPerson                  int32    `json:"price_per_person" jsonschema:"一人あたりの参加費（税込・円、0 以上）"`
	MaxParticipants                 int32    `json:"max_participants" jsonschema:"このチケット 1 枚で参加できる最大人数（1 以上、必須）"`
	UnregisteredParticipantsCount   int32    `json:"unregistered_participants_count,omitempty" jsonschema:"謎部に未登録の同行者の人数（0 以上、省略時 0）。定員の枠を消費する"`
	ParticipantUserIDs              []string `json:"participant_user_ids" jsonschema:"参加メンバーの user_id（1 件以上）。list_users で確認した ID を使う。立替者（自分）も参加するなら含める"`
}

type createTicketWithEventOutput struct {
	EventID                       string    `json:"event_id" jsonschema:"登録された公演の ID"`
	Ticket                        mcpTicket `json:"ticket" jsonschema:"登録されたチケット"`
	MaxParticipants               int32     `json:"max_participants" jsonschema:"チケットの最大参加人数"`
	UnregisteredParticipantsCount int32     `json:"unregistered_participants_count" jsonschema:"未登録の同行者の人数"`
}

func createTicketWithEventTool(tickets nazobuv1connect.TicketServiceHandler) mcp.ToolHandlerFor[createTicketWithEventInput, createTicketWithEventOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createTicketWithEventInput) (*mcp.CallToolResult, createTicketWithEventOutput, error) {
		var out createTicketWithEventOutput
		// 書き込みツールはトークンの write スコープを要求する。
		// 既存の read のみのトークンには、コネクタの再接続（再認可）を促す。
		if !oauth.HasScope(ctx, oauth.ScopeWrite) {
			return nil, out, errors.New("このアクセストークンには write スコープが無い。Claude のコネクタを一度削除して接続し直し、書き込みを許可してほしい")
		}
		res, err := tickets.CreateTicketWithEvent(ctx, connect.NewRequest(&nazobuv1.CreateTicketWithEventRequest{
			EventTitle:                      in.EventTitle,
			EventUrl:                        in.EventURL,
			EventCatchphrase:                in.EventCatchphrase,
			EventDoorsOpenMinutesBefore:     in.EventDoorsOpenMinutesBefore,
			EventEntryDeadlineMinutesBefore: in.EventEntryDeadlineMinutesBefore,
			EventExpectedDurationMinutes:    in.EventExpectedDurationMinutes,
			PricePerPerson:                  in.PricePerPerson,
			StartAt:                         in.StartAt,
			MeetingAt:                       in.MeetingAt,
			MeetingPlace:                    in.MeetingPlace,
			ParticipantUserIds:              in.ParticipantUserIDs,
			MaxParticipants:                 in.MaxParticipants,
			UnregisteredParticipantsCount:   in.UnregisteredParticipantsCount,
		}))
		if err != nil {
			return nil, out, err
		}
		t := res.Msg.GetTicket()
		out.EventID = t.GetEventId()
		out.Ticket = toMCPTicket(t)
		out.MaxParticipants = t.GetMaxParticipants()
		out.UnregisteredParticipantsCount = t.GetUnregisteredParticipantsCount()
		return nil, out, nil
	}
}
