package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

type eventService struct {
	db *sql.DB
	q  *queries.Queries
}

func newEventService(db *sql.DB) nazobuv1connect.EventServiceHandler {
	return &eventService{db: db, q: queries.New(db)}
}

const (
	eventTitleMaxLen = 255
	eventURLMaxLen   = 512
)

func (s *eventService) ListEvents(ctx context.Context, req *connect.Request[nazobuv1.ListEventsRequest]) (*connect.Response[nazobuv1.ListEventsResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	rows, err := s.q.ListEvents(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event 一覧の取得に失敗: %w", err))
	}
	events := make([]*nazobuv1.Event, 0, len(rows))
	for _, r := range rows {
		events = append(events, &nazobuv1.Event{
			Id:                         r.ID,
			Title:                      r.Title,
			Url:                        r.Url,
			DoorsOpenMinutesBefore:     nullInt32ToPtr(r.DoorsOpenMinutesBefore),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(r.EntryDeadlineMinutesBefore),
			Tickets:                    []*nazobuv1.EventTicket{},
		})
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
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("event の登録は admin のみ"))
	}

	title := strings.TrimSpace(req.Msg.GetTitle())
	rawURL := strings.TrimSpace(req.Msg.GetUrl())
	doorsOpen, err := validateMinutesBefore(req.Msg.DoorsOpenMinutesBefore, "doors_open_minutes_before")
	if err != nil {
		return nil, err
	}
	entryDeadline, err := validateMinutesBefore(req.Msg.EntryDeadlineMinutesBefore, "entry_deadline_minutes_before")
	if err != nil {
		return nil, err
	}

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
	if err := s.q.CreateEvent(ctx, queries.CreateEventParams{
		ID:                         id,
		Title:                      title,
		Url:                        rawURL,
		DoorsOpenMinutesBefore:     doorsOpen,
		EntryDeadlineMinutesBefore: entryDeadline,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の登録に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.CreateEventResponse{
		Event: &nazobuv1.Event{
			Id:                         id,
			Title:                      title,
			Url:                        rawURL,
			DoorsOpenMinutesBefore:     nullInt32ToPtr(doorsOpen),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(entryDeadline),
			Tickets:                    []*nazobuv1.EventTicket{},
		},
	}), nil
}

// validateMinutesBefore は任意の「開演時刻の何分前か」を受け取る。0 以上のみ許容し、未指定なら NULL。
func validateMinutesBefore(v *int32, fieldName string) (sql.NullInt32, error) {
	if v == nil {
		return sql.NullInt32{}, nil
	}
	if *v < 0 {
		return sql.NullInt32{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s は 0 以上", fieldName))
	}
	return sql.NullInt32{Int32: *v, Valid: true}, nil
}

func nullInt32ToPtr(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	x := v.Int32
	return &x
}

// attachTickets は events に紐づく ticket と参加者を埋める。1 イベントずつ N+1 で叩かず、
// IN (...) でまとめて引いて in-memory で振り分ける。
func (s *eventService) attachTickets(ctx context.Context, events []*nazobuv1.Event) error {
	indexByEvent := make(map[string]*nazobuv1.Event, len(events))
	eventIDs := make([]string, 0, len(events))
	for _, e := range events {
		indexByEvent[e.Id] = e
		eventIDs = append(eventIDs, e.Id)
	}

	ticketRows, err := s.q.ListEventTicketsByEventIDs(ctx, eventIDs)
	if err != nil {
		return err
	}

	indexByTicket := map[string]*nazobuv1.EventTicket{}
	ticketIDs := make([]string, 0, len(ticketRows))
	for _, r := range ticketRows {
		t := &nazobuv1.EventTicket{
			Id:               r.ID,
			StartAt:          formatJSTDateTime(r.StartAt),
			PricePerPerson:   r.PricePerPerson,
			PurchaserName:    r.PurchaserName,
			ParticipantNames: []string{},
		}
		if ev, ok := indexByEvent[r.EventID]; ok {
			ev.Tickets = append(ev.Tickets, t)
		}
		indexByTicket[r.ID] = t
		ticketIDs = append(ticketIDs, r.ID)
	}
	if len(ticketIDs) == 0 {
		return nil
	}

	partRows, err := s.q.ListTicketParticipantNamesByTicketIDs(ctx, ticketIDs)
	if err != nil {
		return err
	}
	for _, r := range partRows {
		if t, ok := indexByTicket[r.TicketID]; ok {
			t.ParticipantNames = append(t.ParticipantNames, r.Name)
		}
	}
	return nil
}
