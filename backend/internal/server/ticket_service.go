package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

type ticketService struct {
	db  *sql.DB
	now func() time.Time
}

func newTicketService(db *sql.DB) nazobuv1connect.TicketServiceHandler {
	return &ticketService{db: db, now: time.Now}
}

const (
	meetingPlaceMaxLen = 255
	// 集合時刻 / 当日日付の入出力レイアウト。
	timeLayout = "15:04"
)

func (s *ticketService) ListTickets(ctx context.Context, req *connect.Request[nazobuv1.ListTicketsRequest]) (*connect.Response[nazobuv1.ListTicketsResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	tickets, err := s.queryTickets(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket 一覧の取得に失敗: %w", err))
	}
	if len(tickets) == 0 {
		return connect.NewResponse(&nazobuv1.ListTicketsResponse{Tickets: tickets}), nil
	}
	if err := s.attachParticipants(ctx, tickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListTicketsResponse{Tickets: tickets}), nil
}

func (s *ticketService) CreateTicket(ctx context.Context, req *connect.Request[nazobuv1.CreateTicketRequest]) (*connect.Response[nazobuv1.CreateTicketResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	// 立替者は常にログイン中の user。クライアントから受け取らない。
	purchasedBy := user.ID

	msg := req.Msg
	eventID := strings.TrimSpace(msg.GetEventId())
	attendedOn := strings.TrimSpace(msg.GetAttendedOn())
	meetingTime := strings.TrimSpace(msg.GetMeetingTime())
	meetingPlace := strings.TrimSpace(msg.GetMeetingPlace())
	price := msg.GetPricePerPerson()
	participants := dedupeStrings(msg.GetParticipantUserIds())

	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	if _, err := time.ParseInLocation(dateLayout, attendedOn, jst); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attended_on は YYYY-MM-DD"))
	}
	if _, err := time.ParseInLocation(timeLayout, meetingTime, jst); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("meeting_time は HH:MM"))
	}
	if meetingPlace == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("meeting_place は必須"))
	}
	if len(meetingPlace) > meetingPlaceMaxLen {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("meeting_place は %d 文字以内", meetingPlaceMaxLen))
	}
	if price < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("price_per_person は 0 以上"))
	}
	if len(participants) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participant_user_ids は 1 件以上"))
	}

	// 参照整合性は FK でも担保されるが、ユーザに分かりやすいメッセージを返すため事前確認する。
	// 立替者 (= session user) の存在は session 引きで担保済みなので、participants のみ確認する。
	if err := s.assertEventExists(ctx, eventID); err != nil {
		return nil, err
	}
	if err := s.assertUsersExist(ctx, participants); err != nil {
		return nil, err
	}

	id := ulid.Make().String()
	now := s.now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tickets (id, event_id, attended_on, price_per_person, purchased_by, meeting_time, meeting_place, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, eventID, attendedOn, price, purchasedBy, meetingTime, meetingPlace, now, now); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の登録に失敗: %w", err))
	}

	for _, uid := range participants {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ticket_participants (ticket_id, user_id, created_at)
			VALUES (?, ?, ?)
		`, id, uid, now); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket_participants の登録に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}

	tickets, err := s.queryTicketsByIDs(ctx, []string{id})
	if err != nil || len(tickets) == 0 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("登録後の ticket 取得に失敗: %w", err))
	}
	if err := s.attachParticipants(ctx, tickets); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.CreateTicketResponse{Ticket: tickets[0]}), nil
}

func (s *ticketService) queryTickets(ctx context.Context) ([]*nazobuv1.Ticket, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.event_id, e.title, t.attended_on, t.price_per_person,
		       t.meeting_time, t.meeting_place,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS purchaser_name
		FROM tickets t
		JOIN events e  ON e.id  = t.event_id
		JOIN users  pu ON pu.id = t.purchased_by
		ORDER BY t.attended_on DESC, t.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTicketRows(rows)
}

func (s *ticketService) queryTicketsByIDs(ctx context.Context, ids []string) ([]*nazobuv1.Ticket, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.event_id, e.title, t.attended_on, t.price_per_person,
		       t.meeting_time, t.meeting_place,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS purchaser_name
		FROM tickets t
		JOIN events e  ON e.id  = t.event_id
		JOIN users  pu ON pu.id = t.purchased_by
		WHERE t.id IN (%s)
		ORDER BY t.attended_on DESC, t.id ASC
	`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTicketRows(rows)
}

func scanTicketRows(rows *sql.Rows) ([]*nazobuv1.Ticket, error) {
	out := []*nazobuv1.Ticket{}
	for rows.Next() {
		var (
			id, eventID, title, place, purchaser string
			price                                int32
			attendedOn                           time.Time
			// MySQL の TIME は driver により string でも time.Duration でも来うるが
			// go-sql-driver/mysql は string で返す（"HH:MM:SS"）。
			meetingTimeRaw string
		)
		if err := rows.Scan(&id, &eventID, &title, &attendedOn, &price, &meetingTimeRaw, &place, &purchaser); err != nil {
			return nil, err
		}
		out = append(out, &nazobuv1.Ticket{
			Id:               id,
			EventId:          eventID,
			EventTitle:       title,
			AttendedOn:       attendedOn.Format(dateLayout),
			PricePerPerson:   price,
			MeetingTime:      formatMeetingTime(meetingTimeRaw),
			MeetingPlace:     place,
			PurchaserName:    purchaser,
			ParticipantNames: []string{},
		})
	}
	return out, rows.Err()
}

// formatMeetingTime は MySQL から戻る "HH:MM:SS" を "HH:MM" に丸める。
func formatMeetingTime(raw string) string {
	if len(raw) >= 5 {
		return raw[:5]
	}
	return raw
}

func (s *ticketService) attachParticipants(ctx context.Context, tickets []*nazobuv1.Ticket) error {
	indexByTicket := make(map[string]*nazobuv1.Ticket, len(tickets))
	args := make([]any, 0, len(tickets))
	placeholders := make([]string, 0, len(tickets))
	for _, t := range tickets {
		indexByTicket[t.Id] = t
		args = append(args, t.Id)
		placeholders = append(placeholders, "?")
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT tp.ticket_id, COALESCE(NULLIF(u.display_name, ''), u.username)
		FROM ticket_participants tp
		JOIN users u ON u.id = tp.user_id
		WHERE tp.ticket_id IN (%s)
		ORDER BY tp.ticket_id, tp.created_at ASC
	`, strings.Join(placeholders, ",")), args...)
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
			t.ParticipantNames = append(t.ParticipantNames, name)
		}
	}
	return rows.Err()
}

func (s *ticketService) assertEventExists(ctx context.Context, eventID string) error {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE id = ?)`, eventID).Scan(&exists); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("event の存在確認に失敗: %w", err))
	}
	if !exists {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("指定された event は存在しない"))
	}
	return nil
}

func (s *ticketService) assertUsersExist(ctx context.Context, ids []string) error {
	unique := dedupeStrings(ids)
	placeholders := strings.Repeat("?,", len(unique)-1) + "?"
	args := make([]any, 0, len(unique))
	for _, id := range unique {
		args = append(args, id)
	}
	var count int
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM users WHERE id IN (%s)`, placeholders), args...)
	if err := row.Scan(&count); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("user の存在確認に失敗: %w", err))
	}
	if count != len(unique) {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("存在しない user が含まれている"))
	}
	return nil
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
