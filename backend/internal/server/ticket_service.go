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
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

type ticketService struct {
	db *sql.DB
	q  *queries.Queries
}

func newTicketService(db *sql.DB) nazobuv1connect.TicketServiceHandler {
	return &ticketService{db: db, q: queries.New(db)}
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

	rows, err := s.q.ListTickets(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket 一覧の取得に失敗: %w", err))
	}
	tickets := make([]*nazobuv1.Ticket, 0, len(rows))
	for _, r := range rows {
		tickets = append(tickets, &nazobuv1.Ticket{
			Id:               r.ID,
			EventId:          r.EventID,
			EventTitle:       r.EventTitle,
			AttendedOn:       r.AttendedOn.Format(dateLayout),
			PricePerPerson:   r.PricePerPerson,
			MeetingTime:      formatMeetingTime(r.MeetingTime),
			MeetingPlace:     r.MeetingPlace,
			PurchaserName:    r.PurchaserName,
			ParticipantNames: []string{},
		})
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
	attendedOnTime, err := time.ParseInLocation(dateLayout, attendedOn, jst)
	if err != nil {
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.CreateTicket(ctx, queries.CreateTicketParams{
		ID:             id,
		EventID:        eventID,
		AttendedOn:     attendedOnTime,
		PricePerPerson: price,
		PurchasedBy:    purchasedBy,
		MeetingTime:    meetingTime,
		MeetingPlace:   meetingPlace,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の登録に失敗: %w", err))
	}

	for _, uid := range participants {
		if err := qtx.CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{
			TicketID: id,
			UserID:   uid,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket_participants の登録に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}

	rows, err := s.q.ListTicketsByIDs(ctx, []string{id})
	if err != nil || len(rows) == 0 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("登録後の ticket 取得に失敗: %w", err))
	}
	r := rows[0]
	ticket := &nazobuv1.Ticket{
		Id:               r.ID,
		EventId:          r.EventID,
		EventTitle:       r.EventTitle,
		AttendedOn:       r.AttendedOn.Format(dateLayout),
		PricePerPerson:   r.PricePerPerson,
		MeetingTime:      formatMeetingTime(r.MeetingTime),
		MeetingPlace:     r.MeetingPlace,
		PurchaserName:    r.PurchaserName,
		ParticipantNames: []string{},
	}
	if err := s.attachParticipants(ctx, []*nazobuv1.Ticket{ticket}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.CreateTicketResponse{Ticket: ticket}), nil
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
	ticketIDs := make([]string, 0, len(tickets))
	for _, t := range tickets {
		indexByTicket[t.Id] = t
		ticketIDs = append(ticketIDs, t.Id)
	}

	rows, err := s.q.ListTicketParticipantNamesByTicketIDs(ctx, ticketIDs)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if t, ok := indexByTicket[r.TicketID]; ok {
			t.ParticipantNames = append(t.ParticipantNames, r.Name)
		}
	}
	return nil
}

func (s *ticketService) assertEventExists(ctx context.Context, eventID string) error {
	count, err := s.q.CountEventByID(ctx, eventID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("event の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("指定された event は存在しない"))
	}
	return nil
}

func (s *ticketService) assertUsersExist(ctx context.Context, ids []string) error {
	unique := dedupeStrings(ids)
	count, err := s.q.CountUsersByIDs(ctx, unique)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("user の存在確認に失敗: %w", err))
	}
	if int(count) != len(unique) {
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
