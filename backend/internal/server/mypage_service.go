package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"connectrpc.com/connect"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

type myPageService struct {
	db *sql.DB
	q  *queries.Queries
	// now はテスト容易性のため差し替え可能にする。本番は time.Now。
	now func() time.Time
}

func newMyPageService(db *sql.DB) nazobuv1connect.MyPageServiceHandler {
	return &myPageService{db: db, q: queries.New(db), now: time.Now}
}

// jst はサーバの想定タイムゾーン。当日 0:00 や月初の境界算出に使う。
var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

func (s *myPageService) GetMyPage(ctx context.Context, req *connect.Request[nazobuv1.GetMyPageRequest]) (*connect.Response[nazobuv1.GetMyPageResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	now := s.now().In(jst)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jst)
	nextMonthStart := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, jst)

	unsettled, err := s.queryUnsettled(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の取得に失敗: %w", err))
	}

	upcoming, err := s.queryUpcoming(ctx, user.ID, todayStart)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("今後の予定の取得に失敗: %w", err))
	}
	if err := s.attachCompanions(ctx, user.ID, upcoming); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("同行者の取得に失敗: %w", err))
	}

	monthly, err := s.queryMonthly(ctx, user.ID, monthStart, nextMonthStart)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("当月履歴の取得に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.GetMyPageResponse{
		Unsettled:    unsettled,
		Upcoming:     upcoming,
		Monthly:      monthly,
		MonthlyMonth: int32(now.Month()),
	}), nil
}

func (s *myPageService) queryUnsettled(ctx context.Context, userID string) ([]*nazobuv1.UnsettledTicket, error) {
	rows, err := s.q.ListUnsettledTicketsByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.UnsettledTicket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.UnsettledTicket{
			TicketId:       r.ID,
			EventTitle:     r.EventTitle,
			PricePerPerson: r.PricePerPerson,
			PayeeName:      r.PayeeName,
			StartAt:        formatJSTDateTime(r.StartAt),
		})
	}
	return out, nil
}

func (s *myPageService) queryUpcoming(ctx context.Context, userID string, todayStart time.Time) ([]*nazobuv1.UpcomingTicket, error) {
	rows, err := s.q.ListUpcomingTicketsByUserID(ctx, queries.ListUpcomingTicketsByUserIDParams{
		UserID:     userID,
		TodayStart: todayStart,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.UpcomingTicket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.UpcomingTicket{
			TicketId:   r.ID,
			EventTitle: r.EventTitle,
			EventUrl:   r.EventUrl,
			StartAt:    formatJSTDateTime(r.StartAt),
		})
	}
	return out, nil
}

// attachCompanions は upcoming の各 ticket に同行者（自分以外の参加者）名を埋める。
// ticket 数が 0 なら何もしない。1 回の IN クエリでまとめて取り、in-memory で振り分ける。
func (s *myPageService) attachCompanions(ctx context.Context, userID string, upcoming []*nazobuv1.UpcomingTicket) error {
	if len(upcoming) == 0 {
		return nil
	}
	indexByTicket := make(map[string]*nazobuv1.UpcomingTicket, len(upcoming))
	ticketIDs := make([]string, 0, len(upcoming))
	for _, t := range upcoming {
		indexByTicket[t.TicketId] = t
		ticketIDs = append(ticketIDs, t.TicketId)
	}

	rows, err := s.q.ListCompanionNamesByTicketIDs(ctx, queries.ListCompanionNamesByTicketIDsParams{
		TicketIds:     ticketIDs,
		ExcludeUserID: userID,
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if t, ok := indexByTicket[r.TicketID]; ok {
			t.CompanionNames = append(t.CompanionNames, r.Name)
		}
	}
	return nil
}

func (s *myPageService) queryMonthly(ctx context.Context, userID string, monthStart, nextMonthStart time.Time) ([]*nazobuv1.MonthlyTicket, error) {
	rows, err := s.q.ListMyMonthlyTicketsByUserID(ctx, queries.ListMyMonthlyTicketsByUserIDParams{
		UserID:         userID,
		MonthStart:     monthStart,
		NextMonthStart: nextMonthStart,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.MonthlyTicket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.MonthlyTicket{
			TicketId:   r.ID,
			EventTitle: r.EventTitle,
			StartAt:    formatJSTDateTime(r.StartAt),
			Settled:    r.Settled != 0,
		})
	}
	return out, nil
}
