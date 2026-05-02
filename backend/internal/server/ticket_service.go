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

	"github.com/aruma256/nazobu/backend/internal/auth"
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
			EventUrl:         r.EventUrl,
			AttendedOn:       r.AttendedOn.Format(dateLayout),
			PricePerPerson:   r.PricePerPerson,
			MaxParticipants:  nullInt32ToPtr(r.MaxParticipants),
			MeetingTime:      formatNullableMeetingTime(r.MeetingTime),
			MeetingPlace:     r.MeetingPlace,
			StartTime:        formatNullableMeetingTime(r.StartTime),
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

func (s *ticketService) GetTicket(ctx context.Context, req *connect.Request[nazobuv1.GetTicketRequest]) (*connect.Response[nazobuv1.GetTicketResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}

	row, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}

	parts, err := s.q.ListTicketParticipantsByTicketID(ctx, ticketID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	participants := make([]*nazobuv1.TicketParticipant, 0, len(parts))
	participantNames := make([]string, 0, len(parts))
	for _, p := range parts {
		isPurchaser := p.UserID == row.PurchasedBy
		participants = append(participants, &nazobuv1.TicketParticipant{
			UserId:      p.UserID,
			Name:        p.Name,
			Settled:     isPurchaser || p.SettledAt.Valid,
			IsPurchaser: isPurchaser,
		})
		participantNames = append(participantNames, p.Name)
	}

	ticket := &nazobuv1.Ticket{
		Id:               row.ID,
		EventId:          row.EventID,
		EventTitle:       row.EventTitle,
		EventUrl:         row.EventUrl,
		AttendedOn:       row.AttendedOn.Format(dateLayout),
		PricePerPerson:   row.PricePerPerson,
		MaxParticipants:  nullInt32ToPtr(row.MaxParticipants),
		MeetingTime:      formatNullableMeetingTime(row.MeetingTime),
		MeetingPlace:     row.MeetingPlace,
		StartTime:        formatNullableMeetingTime(row.StartTime),
		PurchaserName:    row.PurchaserName,
		ParticipantNames: participantNames,
	}

	canEdit := canEditTicket(user, row.PurchasedBy)

	return connect.NewResponse(&nazobuv1.GetTicketResponse{
		Ticket:       ticket,
		Participants: participants,
		CanEdit:      canEdit,
	}), nil
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
	meetingPlace := strings.TrimSpace(msg.GetMeetingPlace())
	price := msg.GetPricePerPerson()
	maxParticipants := msg.GetMaxParticipants()
	participants := dedupeStrings(msg.GetParticipantUserIds())

	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	attendedOnTime, err := time.ParseInLocation(dateLayout, attendedOn, jst)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attended_on は YYYY-MM-DD"))
	}
	meetingTime, err := parseNullableMeetingTime(msg.GetMeetingTime(), "meeting_time")
	if err != nil {
		return nil, err
	}
	startTime, err := parseNullableMeetingTime(msg.GetStartTime(), "start_time")
	if err != nil {
		return nil, err
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
	if maxParticipants < 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("max_participants は 1 以上"))
	}
	if len(participants) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participant_user_ids は 1 件以上"))
	}
	if int32(len(participants)) > maxParticipants {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participant_user_ids の件数が max_participants を超えている"))
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
		ID:              id,
		EventID:         eventID,
		AttendedOn:      attendedOnTime,
		PricePerPerson:  price,
		MaxParticipants: sql.NullInt32{Int32: maxParticipants, Valid: true},
		PurchasedBy:     purchasedBy,
		MeetingTime:     meetingTime,
		MeetingPlace:    meetingPlace,
		StartTime:       startTime,
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
		EventUrl:         r.EventUrl,
		AttendedOn:       r.AttendedOn.Format(dateLayout),
		PricePerPerson:   r.PricePerPerson,
		MaxParticipants:  nullInt32ToPtr(r.MaxParticipants),
		MeetingTime:      formatNullableMeetingTime(r.MeetingTime),
		MeetingPlace:     r.MeetingPlace,
		StartTime:        formatNullableMeetingTime(r.StartTime),
		PurchaserName:    r.PurchaserName,
		ParticipantNames: []string{},
	}
	if err := s.attachParticipants(ctx, []*nazobuv1.Ticket{ticket}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.CreateTicketResponse{Ticket: ticket}), nil
}

func (s *ticketService) UpdateTicket(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketRequest]) (*connect.Response[nazobuv1.UpdateTicketResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	ticketID := strings.TrimSpace(msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}

	existing, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	if !canEditTicket(user, existing.PurchasedBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("ticket の編集は admin もしくは立替者のみ"))
	}

	attendedOn := strings.TrimSpace(msg.GetAttendedOn())
	meetingPlace := strings.TrimSpace(msg.GetMeetingPlace())
	purchasedBy := strings.TrimSpace(msg.GetPurchasedByUserId())
	price := msg.GetPricePerPerson()
	maxParticipants := msg.GetMaxParticipants()

	attendedOnTime, err := time.ParseInLocation(dateLayout, attendedOn, jst)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attended_on は YYYY-MM-DD"))
	}
	meetingTime, err := parseNullableMeetingTime(msg.GetMeetingTime(), "meeting_time")
	if err != nil {
		return nil, err
	}
	startTime, err := parseNullableMeetingTime(msg.GetStartTime(), "start_time")
	if err != nil {
		return nil, err
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
	if maxParticipants < 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("max_participants は 1 以上"))
	}
	if purchasedBy == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("purchased_by_user_id は必須"))
	}
	// 立替者を変更する場合、新しい立替者は ticket の参加者の中から選ぶ。
	if purchasedBy != existing.PurchasedBy {
		count, err := s.q.CountTicketParticipant(ctx, queries.CountTicketParticipantParams{
			TicketID: ticketID,
			UserID:   purchasedBy,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の存在確認に失敗: %w", err))
		}
		if count == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("立替者は ticket の参加者の中から選ぶ"))
		}
	}
	// max_participants は現在の参加者数を下回ってはいけない。
	participantCount, err := s.q.CountTicketParticipantsByTicketID(ctx, ticketID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者数の取得に失敗: %w", err))
	}
	if int64(maxParticipants) < participantCount {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("max_participants は現在の参加者数以上"))
	}

	if err := s.q.UpdateTicket(ctx, queries.UpdateTicketParams{
		ID:              ticketID,
		AttendedOn:      attendedOnTime,
		PricePerPerson:  price,
		MaxParticipants: sql.NullInt32{Int32: maxParticipants, Valid: true},
		MeetingTime:     meetingTime,
		MeetingPlace:    meetingPlace,
		StartTime:       startTime,
		PurchasedBy:     purchasedBy,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の更新に失敗: %w", err))
	}

	rows, err := s.q.ListTicketsByIDs(ctx, []string{ticketID})
	if err != nil || len(rows) == 0 {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("更新後の ticket 取得に失敗: %w", err))
	}
	r := rows[0]
	ticket := &nazobuv1.Ticket{
		Id:               r.ID,
		EventId:          r.EventID,
		EventTitle:       r.EventTitle,
		EventUrl:         r.EventUrl,
		AttendedOn:       r.AttendedOn.Format(dateLayout),
		PricePerPerson:   r.PricePerPerson,
		MaxParticipants:  nullInt32ToPtr(r.MaxParticipants),
		MeetingTime:      formatNullableMeetingTime(r.MeetingTime),
		MeetingPlace:     r.MeetingPlace,
		StartTime:        formatNullableMeetingTime(r.StartTime),
		PurchaserName:    r.PurchaserName,
		ParticipantNames: []string{},
	}
	if err := s.attachParticipants(ctx, []*nazobuv1.Ticket{ticket}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketResponse{Ticket: ticket}), nil
}

func (s *ticketService) AddTicketParticipants(ctx context.Context, req *connect.Request[nazobuv1.AddTicketParticipantsRequest]) (*connect.Response[nazobuv1.AddTicketParticipantsResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	ticketID := strings.TrimSpace(msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	userIDs := dedupeStrings(msg.GetUserIds())
	if len(userIDs) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_ids は 1 件以上"))
	}

	existing, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	if !canEditTicket(user, existing.PurchasedBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("参加者の追加は admin もしくは立替者のみ"))
	}

	if err := s.assertUsersExist(ctx, userIDs); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	// max_participants を超えないよう、現在数を持って 1 件ずつ加算する。
	// max_participants が NULL（既存レコード）のときは制限なしで扱う。
	currentCount, err := qtx.CountTicketParticipantsByTicketID(ctx, ticketID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者数の取得に失敗: %w", err))
	}
	for _, uid := range userIDs {
		count, err := qtx.CountTicketParticipant(ctx, queries.CountTicketParticipantParams{
			TicketID: ticketID,
			UserID:   uid,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の重複確認に失敗: %w", err))
		}
		if count > 0 {
			// 既に参加済み。冪等に扱う。
			continue
		}
		if existing.MaxParticipants.Valid && currentCount >= int64(existing.MaxParticipants.Int32) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("max_participants を超えるため追加できない"))
		}
		if err := qtx.CreateTicketParticipant(ctx, queries.CreateTicketParticipantParams{
			TicketID: ticketID,
			UserID:   uid,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の追加に失敗: %w", err))
		}
		currentCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.AddTicketParticipantsResponse{}), nil
}

func (s *ticketService) RemoveTicketParticipant(ctx context.Context, req *connect.Request[nazobuv1.RemoveTicketParticipantRequest]) (*connect.Response[nazobuv1.RemoveTicketParticipantResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	userID := strings.TrimSpace(req.Msg.GetUserId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	if userID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id は必須"))
	}

	existing, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	if !canEditTicket(user, existing.PurchasedBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("参加者の削除は admin もしくは立替者のみ"))
	}
	if userID == existing.PurchasedBy {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("立替者本人は参加者から削除できない"))
	}

	if err := s.q.DeleteTicketParticipant(ctx, queries.DeleteTicketParticipantParams{
		TicketID: ticketID,
		UserID:   userID,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の削除に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.RemoveTicketParticipantResponse{}), nil
}

func (s *ticketService) UpdateTicketParticipantSettlement(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketParticipantSettlementRequest]) (*connect.Response[nazobuv1.UpdateTicketParticipantSettlementResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	userID := strings.TrimSpace(req.Msg.GetUserId())
	settled := req.Msg.GetSettled()
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	if userID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id は必須"))
	}

	existing, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	if !canEditTicket(user, existing.PurchasedBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("精算状態の更新は admin もしくは立替者のみ"))
	}
	if userID == existing.PurchasedBy {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("立替者本人は精算操作の対象外"))
	}

	count, err := s.q.CountTicketParticipant(ctx, queries.CountTicketParticipantParams{
		TicketID: ticketID,
		UserID:   userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された参加者は ticket に紐づいていない"))
	}

	if settled {
		if err := s.q.MarkTicketParticipantSettled(ctx, queries.MarkTicketParticipantSettledParams{
			TicketID: ticketID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("精算済みの登録に失敗: %w", err))
		}
	} else {
		if err := s.q.MarkTicketParticipantUnsettled(ctx, queries.MarkTicketParticipantUnsettledParams{
			TicketID: ticketID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の登録に失敗: %w", err))
		}
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketParticipantSettlementResponse{}), nil
}

// canEditTicket は admin もしくは立替者本人なら編集可とする。
func canEditTicket(user *auth.User, purchasedBy string) bool {
	return user.Role == auth.RoleAdmin || user.ID == purchasedBy
}

// formatNullableMeetingTime は NULL 可な TIME カラム（"HH:MM:SS"）を "HH:MM"（NULL なら空文字）にする。
func formatNullableMeetingTime(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	if len(v.String) >= 5 {
		return v.String[:5]
	}
	return v.String
}

// parseNullableMeetingTime は "HH:MM"（空文字なら NULL）として受け取る。
// field は invalid 時のエラーメッセージで参照する proto フィールド名。
func parseNullableMeetingTime(raw, field string) (sql.NullString, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return sql.NullString{}, nil
	}
	if _, err := time.ParseInLocation(timeLayout, s, jst); err != nil {
		return sql.NullString{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s は HH:MM", field))
	}
	return sql.NullString{String: s, Valid: true}, nil
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
