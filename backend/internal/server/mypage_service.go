package server

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

type myPageService struct {
	db *sql.DB
	// now はテスト容易性のため差し替え可能にする。本番は time.Now。
	now func() time.Time
}

func newMyPageService(db *sql.DB) nazobuv1connect.MyPageServiceHandler {
	return &myPageService{db: db, now: time.Now}
}

// jst はサーバの想定タイムゾーン。attended_on の DATE 比較や月跨ぎの基準に使う。
var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

const dateLayout = "2006-01-02"

func (s *myPageService) GetMyPage(ctx context.Context, req *connect.Request[nazobuv1.GetMyPageRequest]) (*connect.Response[nazobuv1.GetMyPageResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	now := s.now().In(jst)
	today := now.Format(dateLayout)
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, jst).Format(dateLayout)
	firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, jst).Format(dateLayout)

	unsettled, err := s.queryUnsettled(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の取得に失敗: %w", err))
	}

	upcoming, err := s.queryUpcoming(ctx, user.ID, today)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("今後の予定の取得に失敗: %w", err))
	}
	if err := s.attachCompanions(ctx, user.ID, upcoming); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("同行者の取得に失敗: %w", err))
	}

	monthly, err := s.queryMonthly(ctx, user.ID, firstOfMonth, firstOfNextMonth)
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, e.title, t.price_per_person, t.attended_on,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS payee_name
		FROM ticket_participants tp
		JOIN tickets t ON t.id = tp.ticket_id
		JOIN events  e ON e.id = t.event_id
		JOIN users   pu ON pu.id = t.purchased_by
		WHERE tp.user_id = ?
		  AND tp.settled_at IS NULL
		  AND t.purchased_by <> tp.user_id
		ORDER BY t.attended_on ASC, t.id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*nazobuv1.UnsettledTicket{}
	for rows.Next() {
		var (
			id, title, payee string
			price            int32
			attendedOn       time.Time
		)
		if err := rows.Scan(&id, &title, &price, &attendedOn, &payee); err != nil {
			return nil, err
		}
		out = append(out, &nazobuv1.UnsettledTicket{
			TicketId:       id,
			EventTitle:     title,
			PricePerPerson: price,
			PayeeName:      payee,
			AttendedOn:     attendedOn.Format(dateLayout),
		})
	}
	return out, rows.Err()
}

func (s *myPageService) queryUpcoming(ctx context.Context, userID, today string) ([]*nazobuv1.UpcomingTicket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, e.title, e.url, t.attended_on
		FROM ticket_participants tp
		JOIN tickets t ON t.id = tp.ticket_id
		JOIN events  e ON e.id = t.event_id
		WHERE tp.user_id = ?
		  AND t.attended_on >= ?
		ORDER BY t.attended_on ASC, t.id ASC
	`, userID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*nazobuv1.UpcomingTicket{}
	for rows.Next() {
		var (
			id, title, url string
			attendedOn     time.Time
		)
		if err := rows.Scan(&id, &title, &url, &attendedOn); err != nil {
			return nil, err
		}
		out = append(out, &nazobuv1.UpcomingTicket{
			TicketId:   id,
			EventTitle: title,
			EventUrl:   url,
			AttendedOn: attendedOn.Format(dateLayout),
		})
	}
	return out, rows.Err()
}

// attachCompanions は upcoming の各 ticket に同行者（自分以外の参加者）名を埋める。
// ticket 数が 0 なら何もしない。1 回の IN クエリでまとめて取り、in-memory で振り分ける。
func (s *myPageService) attachCompanions(ctx context.Context, userID string, upcoming []*nazobuv1.UpcomingTicket) error {
	if len(upcoming) == 0 {
		return nil
	}
	indexByTicket := make(map[string]*nazobuv1.UpcomingTicket, len(upcoming))
	args := make([]any, 0, len(upcoming)+1)
	placeholders := make([]string, 0, len(upcoming))
	for _, t := range upcoming {
		indexByTicket[t.TicketId] = t
		args = append(args, t.TicketId)
		placeholders = append(placeholders, "?")
	}
	args = append(args, userID)

	query := fmt.Sprintf(`
		SELECT tp.ticket_id, COALESCE(NULLIF(u.display_name, ''), u.username)
		FROM ticket_participants tp
		JOIN users u ON u.id = tp.user_id
		WHERE tp.ticket_id IN (%s)
		  AND tp.user_id <> ?
		ORDER BY tp.ticket_id, tp.created_at ASC
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var ticketID, name string
		if err := rows.Scan(&ticketID, &name); err != nil {
			return err
		}
		if t, ok := indexByTicket[ticketID]; ok {
			t.CompanionNames = append(t.CompanionNames, name)
		}
	}
	return rows.Err()
}

func (s *myPageService) queryMonthly(ctx context.Context, userID, monthStart, nextMonthStart string) ([]*nazobuv1.MonthlyTicket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, e.title, t.attended_on,
		       (tp.settled_at IS NOT NULL OR t.purchased_by = tp.user_id) AS settled
		FROM ticket_participants tp
		JOIN tickets t ON t.id = tp.ticket_id
		JOIN events  e ON e.id = t.event_id
		WHERE tp.user_id = ?
		  AND t.attended_on >= ?
		  AND t.attended_on <  ?
		ORDER BY t.attended_on DESC, t.id ASC
	`, userID, monthStart, nextMonthStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*nazobuv1.MonthlyTicket{}
	for rows.Next() {
		var (
			id, title  string
			attendedOn time.Time
			settled    bool
		)
		if err := rows.Scan(&id, &title, &attendedOn, &settled); err != nil {
			return nil, err
		}
		out = append(out, &nazobuv1.MonthlyTicket{
			TicketId:   id,
			EventTitle: title,
			AttendedOn: attendedOn.Format(dateLayout),
			Settled:    settled,
		})
	}
	return out, rows.Err()
}
