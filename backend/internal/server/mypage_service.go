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
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jst)
	// 履歴セクションは前月をデフォルト表示にする。
	prevMonthStart := currentMonthStart.AddDate(0, -1, 0)

	unsettled, err := s.queryUnsettled(ctx, user.ID, now)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の取得に失敗: %w", err))
	}

	upcoming, err := s.queryUpcoming(ctx, user.ID, todayStart)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("今後の予定の取得に失敗: %w", err))
	}

	// 未精算は過去 / 今後の予定は当日以降と時間軸が分かれるので重複しない。
	// 1 回の IN クエリにまとめて参加者名を引き、N+1 を避ける。
	allTickets := make([]*nazobuv1.Ticket, 0, len(unsettled)+len(upcoming))
	allTickets = append(allTickets, unsettled...)
	allTickets = append(allTickets, upcoming...)
	if err := attachTicketParticipantNames(ctx, s.q, allTickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}

	monthly, err := s.queryMonthly(ctx, user.ID, prevMonthStart, clipHistoryEnd(currentMonthStart, todayStart))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("前月履歴の取得に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.GetMyPageResponse{
		Unsettled:    unsettled,
		Upcoming:     upcoming,
		Monthly:      monthly,
		MonthlyMonth: int32(prevMonthStart.Month()),
		MonthlyYear:  int32(prevMonthStart.Year()),
		CurrentMonth: int32(now.Month()),
		CurrentYear:  int32(now.Year()),
	}), nil
}

func (s *myPageService) ListMonthlyTickets(ctx context.Context, req *connect.Request[nazobuv1.ListMonthlyTicketsRequest]) (*connect.Response[nazobuv1.ListMonthlyTicketsResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	year := req.Msg.Year
	month := req.Msg.Month
	if month < 1 || month > 12 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("month は 1〜12 の範囲で指定してください"))
	}
	if year < 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("year は正の整数で指定してください"))
	}

	now := s.now().In(jst)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	monthStart := time.Date(int(year), time.Month(month), 1, 0, 0, 0, 0, jst)
	nextMonthStart := monthStart.AddDate(0, 1, 0)

	monthly, err := s.queryMonthly(ctx, user.ID, monthStart, clipHistoryEnd(nextMonthStart, todayStart))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("月別履歴の取得に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.ListMonthlyTicketsResponse{
		Monthly: monthly,
		Year:    year,
		Month:   month,
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

func (s *myPageService) queryUpcoming(ctx context.Context, userID string, todayStart time.Time) ([]*nazobuv1.Ticket, error) {
	rows, err := s.q.ListUpcomingTicketsByUserID(ctx, queries.ListUpcomingTicketsByUserIDParams{
		UserID:     userID,
		TodayStart: todayStart,
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
