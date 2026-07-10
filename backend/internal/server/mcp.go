package server

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

// newMCPHandler は Claude connector 向けの MCP（Streamable HTTP）ハンドラを組み立てる。
// 認証は前段の oauth.Middleware が済ませ、auth.User を context に注入してくる前提。
// ツール実装は既存の Connect RPC ハンドラを in-process 呼び出しして再利用する
// （lookupSessionUser が context の user を優先するため cookie なしで通る）。
func newMCPHandler(mypage nazobuv1connect.MyPageServiceHandler) http.Handler {
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
			out.Tickets = append(out.Tickets, mcpTicket{
				TicketID:         t.GetId(),
				EventTitle:       t.GetEventTitle(),
				EventURL:         t.GetEventUrl(),
				StartAt:          t.GetStartAt(),
				MeetingAt:        t.GetMeetingAt(),
				MeetingPlace:     t.GetMeetingPlace(),
				PricePerPerson:   t.GetPricePerPerson(),
				PurchaserName:    t.GetPurchaserName(),
				ParticipantNames: t.GetParticipantNames(),
			})
		}
		return nil, out, nil
	}
}
