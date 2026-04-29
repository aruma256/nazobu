package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

type eventService struct {
	db  *sql.DB
	now func() time.Time
}

func newEventService(db *sql.DB) nazobuv1connect.EventServiceHandler {
	return &eventService{db: db, now: time.Now}
}

const (
	eventTitleMaxLen = 255
	eventURLMaxLen   = 512
)

func (s *eventService) ListEvents(ctx context.Context, req *connect.Request[nazobuv1.ListEventsRequest]) (*connect.Response[nazobuv1.ListEventsResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	events, err := s.queryEvents(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event 一覧の取得に失敗: %w", err))
	}
	if len(events) == 0 {
		return connect.NewResponse(&nazobuv1.ListEventsResponse{Events: events}), nil
	}
	if err := s.attachTickets(ctx, events); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListEventsResponse{Events: events}), nil
}

func (s *eventService) CreateEvent(ctx context.Context, req *connect.Request[nazobuv1.CreateEventRequest]) (*connect.Response[nazobuv1.CreateEventResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	title := strings.TrimSpace(req.Msg.GetTitle())
	rawURL := strings.TrimSpace(req.Msg.GetUrl())

	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title は必須"))
	}
	if rawURL == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("url は必須"))
	}
	if len(title) > eventTitleMaxLen {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("title は %d 文字以内", eventTitleMaxLen))
	}
	if len(rawURL) > eventURLMaxLen {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("url は %d 文字以内", eventURLMaxLen))
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("url は http(s) スキーマの URL"))
	}

	id := ulid.Make().String()
	now := s.now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO events (id, title, url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, title, rawURL, now, now); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の登録に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.CreateEventResponse{
		Event: &nazobuv1.Event{
			Id:      id,
			Title:   title,
			Url:     rawURL,
			Tickets: []*nazobuv1.EventTicket{},
		},
	}), nil
}

func (s *eventService) queryEvents(ctx context.Context) ([]*nazobuv1.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, url
		FROM events
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*nazobuv1.Event{}
	for rows.Next() {
		var id, title, eventURL string
		if err := rows.Scan(&id, &title, &eventURL); err != nil {
			return nil, err
		}
		out = append(out, &nazobuv1.Event{
			Id:      id,
			Title:   title,
			Url:     eventURL,
			Tickets: []*nazobuv1.EventTicket{},
		})
	}
	return out, rows.Err()
}

// attachTickets は events に紐づく ticket と参加者を埋める。1 イベントずつ N+1 で叩かず、
// IN (...) でまとめて引いて in-memory で振り分ける。
func (s *eventService) attachTickets(ctx context.Context, events []*nazobuv1.Event) error {
	indexByEvent := make(map[string]*nazobuv1.Event, len(events))
	args := make([]any, 0, len(events))
	placeholders := make([]string, 0, len(events))
	for _, e := range events {
		indexByEvent[e.Id] = e
		args = append(args, e.Id)
		placeholders = append(placeholders, "?")
	}
	in := strings.Join(placeholders, ",")

	ticketRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.id, t.event_id, t.attended_on, t.price_per_person,
		       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS purchaser_name
		FROM tickets t
		JOIN users pu ON pu.id = t.purchased_by
		WHERE t.event_id IN (%s)
		ORDER BY t.attended_on DESC, t.id ASC
	`, in), args...)
	if err != nil {
		return err
	}
	defer ticketRows.Close()

	indexByTicket := map[string]*nazobuv1.EventTicket{}
	ticketIDs := []any{}
	for ticketRows.Next() {
		var (
			id, eventID, purchaser string
			price                  int32
			attendedOn             time.Time
		)
		if err := ticketRows.Scan(&id, &eventID, &attendedOn, &price, &purchaser); err != nil {
			return err
		}
		t := &nazobuv1.EventTicket{
			Id:               id,
			AttendedOn:       attendedOn.Format(dateLayout),
			PricePerPerson:   price,
			PurchaserName:    purchaser,
			ParticipantNames: []string{},
		}
		if ev, ok := indexByEvent[eventID]; ok {
			ev.Tickets = append(ev.Tickets, t)
		}
		indexByTicket[id] = t
		ticketIDs = append(ticketIDs, id)
	}
	if err := ticketRows.Err(); err != nil {
		return err
	}
	if len(ticketIDs) == 0 {
		return nil
	}

	tinPlaceholders := strings.Repeat("?,", len(ticketIDs)-1) + "?"
	partRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT tp.ticket_id, COALESCE(NULLIF(u.display_name, ''), u.username)
		FROM ticket_participants tp
		JOIN users u ON u.id = tp.user_id
		WHERE tp.ticket_id IN (%s)
		ORDER BY tp.ticket_id, tp.created_at ASC
	`, tinPlaceholders), ticketIDs...)
	if err != nil {
		return err
	}
	defer partRows.Close()

	for partRows.Next() {
		var ticketID, name string
		if err := partRows.Scan(&ticketID, &name); err != nil {
			return err
		}
		if t, ok := indexByTicket[ticketID]; ok {
			t.ParticipantNames = append(t.ParticipantNames, name)
		}
	}
	return partRows.Err()
}
