package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

type eventService struct {
	db         *sql.DB
	q          *queries.Queries
	httpClient *http.Client
}

func newEventService(db *sql.DB) nazobuv1connect.EventServiceHandler {
	return &eventService{db: db, q: queries.New(db), httpClient: http.DefaultClient}
}

const (
	eventTitleMaxLen       = 255
	eventURLMaxLen         = 512
	eventCatchphraseMaxLen = 255
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
			Catchphrase:                r.Catchphrase,
			ImageUrl:                   nullStringToString(r.ImageUrl),
			DoorsOpenMinutesBefore:     nullInt32ToPtr(r.DoorsOpenMinutesBefore),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(r.EntryDeadlineMinutesBefore),
			ExpectedDurationMinutes:    r.ExpectedDurationMinutes,
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

func (s *eventService) GetEvent(ctx context.Context, req *connect.Request[nazobuv1.GetEventRequest]) (*connect.Response[nazobuv1.GetEventResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	eventID := strings.TrimSpace(req.Msg.GetEventId())
	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}

	row, err := s.q.GetEventByID(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された event は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の取得に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.GetEventResponse{
		Event: &nazobuv1.Event{
			Id:                         row.ID,
			Title:                      row.Title,
			Url:                        row.Url,
			Catchphrase:                row.Catchphrase,
			ImageUrl:                   nullStringToString(row.ImageUrl),
			DoorsOpenMinutesBefore:     nullInt32ToPtr(row.DoorsOpenMinutesBefore),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(row.EntryDeadlineMinutesBefore),
			ExpectedDurationMinutes:    row.ExpectedDurationMinutes,
			Tickets:                    []*nazobuv1.EventTicket{},
		},
		CanEdit: user.Role == auth.RoleAdmin,
	}), nil
}

func (s *eventService) CreateEvent(ctx context.Context, req *connect.Request[nazobuv1.CreateEventRequest]) (*connect.Response[nazobuv1.CreateEventResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("event の登録は admin のみ"))
	}

	msg := req.Msg
	prepared, err := prepareEventFields(
		ctx, s.httpClient,
		msg.GetTitle(), msg.GetUrl(), msg.GetCatchphrase(),
		msg.DoorsOpenMinutesBefore, msg.EntryDeadlineMinutesBefore,
		msg.GetExpectedDurationMinutes(),
	)
	if err != nil {
		return nil, err
	}

	eventID := id.New()
	if err := s.q.CreateEvent(ctx, queries.CreateEventParams{
		ID:                         eventID,
		Title:                      prepared.title,
		Url:                        prepared.url,
		Catchphrase:                prepared.catchphrase,
		ImageUrl:                   prepared.imageURL,
		DoorsOpenMinutesBefore:     prepared.doorsOpen,
		EntryDeadlineMinutesBefore: prepared.entryDeadline,
		ExpectedDurationMinutes:    prepared.expectedDuration,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の登録に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.CreateEventResponse{
		Event: &nazobuv1.Event{
			Id:                         eventID,
			Title:                      prepared.title,
			Url:                        prepared.url,
			Catchphrase:                prepared.catchphrase,
			ImageUrl:                   nullStringToString(prepared.imageURL),
			DoorsOpenMinutesBefore:     nullInt32ToPtr(prepared.doorsOpen),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(prepared.entryDeadline),
			ExpectedDurationMinutes:    prepared.expectedDuration,
			Tickets:                    []*nazobuv1.EventTicket{},
		},
	}), nil
}

func (s *eventService) UpdateEvent(ctx context.Context, req *connect.Request[nazobuv1.UpdateEventRequest]) (*connect.Response[nazobuv1.UpdateEventResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("event の編集は admin のみ"))
	}

	msg := req.Msg
	eventID := strings.TrimSpace(msg.GetEventId())
	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	prepared, err := prepareEventFields(
		ctx, s.httpClient,
		msg.GetTitle(), msg.GetUrl(), msg.GetCatchphrase(),
		msg.DoorsOpenMinutesBefore, msg.EntryDeadlineMinutesBefore,
		msg.GetExpectedDurationMinutes(),
	)
	if err != nil {
		return nil, err
	}

	if _, err := s.q.GetEventByID(ctx, eventID); errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された event は存在しない"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の取得に失敗: %w", err))
	}

	if err := s.q.UpdateEvent(ctx, queries.UpdateEventParams{
		ID:                         eventID,
		Title:                      prepared.title,
		Url:                        prepared.url,
		Catchphrase:                prepared.catchphrase,
		ImageUrl:                   prepared.imageURL,
		DoorsOpenMinutesBefore:     prepared.doorsOpen,
		EntryDeadlineMinutesBefore: prepared.entryDeadline,
		ExpectedDurationMinutes:    prepared.expectedDuration,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("event の更新に失敗: %w", err))
	}

	return connect.NewResponse(&nazobuv1.UpdateEventResponse{
		Event: &nazobuv1.Event{
			Id:                         eventID,
			Title:                      prepared.title,
			Url:                        prepared.url,
			Catchphrase:                prepared.catchphrase,
			ImageUrl:                   nullStringToString(prepared.imageURL),
			DoorsOpenMinutesBefore:     nullInt32ToPtr(prepared.doorsOpen),
			EntryDeadlineMinutesBefore: nullInt32ToPtr(prepared.entryDeadline),
			ExpectedDurationMinutes:    prepared.expectedDuration,
			Tickets:                    []*nazobuv1.EventTicket{},
		},
	}), nil
}

// preparedEventFields は event の入力検証 + OG タグ取得 + catchphrase 補完まで済ませた値を保持する。
// CreateEvent / UpdateEvent / CreateTicketWithEvent / UpdateTicketWithEvent から共通で使う。
type preparedEventFields struct {
	title            string
	url              string
	catchphrase      string
	imageURL         sql.NullString
	doorsOpen        sql.NullInt32
	entryDeadline    sql.NullInt32
	expectedDuration int32
}

// prepareEventFields は event 部の入力を検証し、OG タグ取得と catchphrase 補完まで一括で行う。
func prepareEventFields(
	ctx context.Context,
	client *http.Client,
	titleIn, rawURLIn, catchphraseIn string,
	doorsOpenIn, entryDeadlineIn *int32,
	expectedDurationIn int32,
) (preparedEventFields, error) {
	title := strings.TrimSpace(titleIn)
	rawURL := strings.TrimSpace(rawURLIn)
	catchphrase := strings.TrimSpace(catchphraseIn)

	doorsOpen, err := validateMinutesBefore(doorsOpenIn, "doors_open_minutes_before")
	if err != nil {
		return preparedEventFields{}, err
	}
	entryDeadline, err := validateMinutesBefore(entryDeadlineIn, "entry_deadline_minutes_before")
	if err != nil {
		return preparedEventFields{}, err
	}
	expectedDuration, err := validateExpectedDurationMinutes(expectedDurationIn)
	if err != nil {
		return preparedEventFields{}, err
	}
	if title == "" {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, errors.New("title は必須"))
	}
	if rawURL == "" {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, errors.New("url は必須"))
	}
	if len(title) > eventTitleMaxLen {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("title は %d 文字以内", eventTitleMaxLen))
	}
	if len(rawURL) > eventURLMaxLen {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("url は %d 文字以内", eventURLMaxLen))
	}
	if len(catchphrase) > eventCatchphraseMaxLen {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("catchphrase は %d 文字以内", eventCatchphraseMaxLen))
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return preparedEventFields{}, connect.NewError(connect.CodeInvalidArgument, errors.New("url は http(s) スキーマの URL"))
	}

	og := fetchOGTags(ctx, client, rawURL)
	imageURL := stringToNullString(og.Image)
	catchphrase = applyOGDescriptionFallback(catchphrase, parsed.Host, og.Description)

	return preparedEventFields{
		title:            title,
		url:              rawURL,
		catchphrase:      catchphrase,
		imageURL:         imageURL,
		doorsOpen:        doorsOpen,
		entryDeadline:    entryDeadline,
		expectedDuration: expectedDuration,
	}, nil
}

// applyOGDescriptionFallback は web 入力のキャッチコピーが空のとき、og:description を採用するか判定して返す。
// 採用条件は shouldUseOGDescriptionAsCatchphrase に委ねる。長さオーバー時は採用しない（手入力で補ってもらう）。
func applyOGDescriptionFallback(catchphrase, host, ogDescription string) string {
	if catchphrase != "" {
		return catchphrase
	}
	if !shouldUseOGDescriptionAsCatchphrase(host, ogDescription) {
		return catchphrase
	}
	if len(ogDescription) > eventCatchphraseMaxLen {
		return catchphrase
	}
	return ogDescription
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

// validateExpectedDurationMinutes は想定所要時間（分）を検証する。1 以上が必須。
func validateExpectedDurationMinutes(v int32) (int32, error) {
	if v < 1 {
		return 0, connect.NewError(connect.CodeInvalidArgument, errors.New("expected_duration_minutes は 1 以上"))
	}
	return v, nil
}

func nullInt32ToPtr(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	x := v.Int32
	return &x
}

func nullStringToString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

// 空文字は NULL にする（DB 側でも意味的に未取得 = NULL を貫く）。
func stringToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
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
