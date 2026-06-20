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

func (s *myPageService) ListMyUnsettledTickets(ctx context.Context, req *connect.Request[nazobuv1.ListMyUnsettledTicketsRequest]) (*connect.Response[nazobuv1.ListMyUnsettledTicketsResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	now := s.now().In(jst)
	tickets, err := s.queryUnsettled(ctx, user.ID, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の取得に失敗: %w", err))
	}
	if err := attachTicketParticipantNames(ctx, s.q, tickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListMyUnsettledTicketsResponse{Tickets: tickets}), nil
}

func (s *myPageService) ListMyUnsettledReceivables(ctx context.Context, req *connect.Request[nazobuv1.ListMyUnsettledReceivablesRequest]) (*connect.Response[nazobuv1.ListMyUnsettledReceivablesResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	now := s.now().In(jst)
	tickets, err := s.queryUnsettledReceivables(ctx, user.ID, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未回収の取得に失敗: %w", err))
	}
	if err := attachTicketParticipantNames(ctx, s.q, tickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListMyUnsettledReceivablesResponse{Tickets: tickets}), nil
}

func (s *myPageService) ListMyUpcomingTickets(ctx context.Context, req *connect.Request[nazobuv1.ListMyUpcomingTicketsRequest]) (*connect.Response[nazobuv1.ListMyUpcomingTicketsResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	now := s.now().In(jst)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	tickets, err := s.queryUpcoming(ctx, user.ID, todayStart, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("今後の予定の取得に失敗: %w", err))
	}
	if err := attachTicketParticipantNames(ctx, s.q, tickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListMyUpcomingTicketsResponse{Tickets: tickets}), nil
}

func (s *myPageService) ListMonthlyTickets(ctx context.Context, req *connect.Request[nazobuv1.ListMonthlyTicketsRequest]) (*connect.Response[nazobuv1.ListMonthlyTicketsResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	now := s.now().In(jst)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)

	year := req.Msg.Year
	month := req.Msg.Month
	var monthStart time.Time
	switch {
	case year == 0 && month == 0:
		// 既定（マイページ初期表示）として前月を返す。
		currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jst)
		monthStart = currentMonthStart.AddDate(0, -1, 0)
	case month < 1 || month > 12:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("month は 1〜12 の範囲で指定してください"))
	case year < 1:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("year は正の整数で指定してください"))
	default:
		monthStart = time.Date(int(year), time.Month(month), 1, 0, 0, 0, 0, jst)
	}
	nextMonthStart := monthStart.AddDate(0, 1, 0)

	monthly, err := s.queryMonthly(ctx, user.ID, monthStart, clipHistoryEnd(nextMonthStart, todayStart))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("月別履歴の取得に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.ListMonthlyTicketsResponse{
		Monthly: monthly,
		Year:    int32(monthStart.Year()),
		Month:   int32(monthStart.Month()),
	}), nil
}

// clipHistoryEnd は履歴の上限を「当日 0:00」で切る。
// 履歴に未来分（今後の予定と重なる範囲）を入れないため。
// monthStart >= todayStart の月は upper <= monthStart になり、結果は空になる。
func clipHistoryEnd(nextMonthStart, todayStart time.Time) time.Time {
	if todayStart.Before(nextMonthStart) {
		return todayStart
	}
	return nextMonthStart
}

func (s *myPageService) queryUnsettled(ctx context.Context, userID string, now time.Time) ([]*nazobuv1.Ticket, error) {
	rows, err := s.q.ListUnsettledTicketsByUserID(ctx, queries.ListUnsettledTicketsByUserIDParams{
		UserID: userID,
		Now:    now,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.Ticket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.Ticket{
			Id:                           r.ID,
			EventId:                      r.EventID,
			EventTitle:                   r.EventTitle,
			EventUrl:                     r.EventUrl,
			EventCatchphrase:             r.EventCatchphrase,
			EventImageUrl:                nullStringToString(r.EventImageUrl),
			EventExpectedDurationMinutes: r.EventExpectedDurationMinutes,
			EventDoorsOpenMinutesBefore:  nullInt32ToPtr(r.EventDoorsOpenMinutesBefore),
			StartAt:                      formatJSTDateTime(r.StartAt),
			MeetingAt:                    formatNullableJSTDateTime(r.MeetingAt),
			PricePerPerson:               r.PricePerPerson,
			MaxParticipants:              r.MaxParticipants,
			MeetingPlace:                 r.MeetingPlace,
			PurchaserName:                r.PurchaserName,
			ParticipantNames:             []string{},
		})
	}
	return out, nil
}

func (s *myPageService) queryUnsettledReceivables(ctx context.Context, userID string, now time.Time) ([]*nazobuv1.Ticket, error) {
	rows, err := s.q.ListUnsettledReceivablesByUserID(ctx, queries.ListUnsettledReceivablesByUserIDParams{
		UserID: userID,
		Now:    now,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.Ticket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.Ticket{
			Id:                           r.ID,
			EventId:                      r.EventID,
			EventTitle:                   r.EventTitle,
			EventUrl:                     r.EventUrl,
			EventCatchphrase:             r.EventCatchphrase,
			EventImageUrl:                nullStringToString(r.EventImageUrl),
			EventExpectedDurationMinutes: r.EventExpectedDurationMinutes,
			EventDoorsOpenMinutesBefore:  nullInt32ToPtr(r.EventDoorsOpenMinutesBefore),
			StartAt:                      formatJSTDateTime(r.StartAt),
			MeetingAt:                    formatNullableJSTDateTime(r.MeetingAt),
			PricePerPerson:               r.PricePerPerson,
			MaxParticipants:              r.MaxParticipants,
			MeetingPlace:                 r.MeetingPlace,
			PurchaserName:                r.PurchaserName,
			ParticipantNames:             []string{},
		})
	}
	return out, nil
}

func (s *myPageService) queryUpcoming(ctx context.Context, userID string, todayStart, now time.Time) ([]*nazobuv1.Ticket, error) {
	rows, err := s.q.ListUpcomingTicketsByUserID(ctx, queries.ListUpcomingTicketsByUserIDParams{
		UserID:     userID,
		TodayStart: todayStart,
		Now:        now,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*nazobuv1.Ticket, 0, len(rows))
	for _, r := range rows {
		out = append(out, &nazobuv1.Ticket{
			Id:                           r.ID,
			EventId:                      r.EventID,
			EventTitle:                   r.EventTitle,
			EventUrl:                     r.EventUrl,
			EventCatchphrase:             r.EventCatchphrase,
			EventImageUrl:                nullStringToString(r.EventImageUrl),
			EventExpectedDurationMinutes: r.EventExpectedDurationMinutes,
			EventDoorsOpenMinutesBefore:  nullInt32ToPtr(r.EventDoorsOpenMinutesBefore),
			StartAt:                      formatJSTDateTime(r.StartAt),
			MeetingAt:                    formatNullableJSTDateTime(r.MeetingAt),
			PricePerPerson:               r.PricePerPerson,
			MaxParticipants:              r.MaxParticipants,
			MeetingPlace:                 r.MeetingPlace,
			PurchaserName:                r.PurchaserName,
			ParticipantNames:             []string{},
		})
	}
	return out, nil
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
