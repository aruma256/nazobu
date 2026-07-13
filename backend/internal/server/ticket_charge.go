package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

const chargeTitleMaxLen = 255

func (s *ticketService) CreateTicketCharge(ctx context.Context, req *connect.Request[nazobuv1.CreateTicketChargeRequest]) (*connect.Response[nazobuv1.CreateTicketChargeResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	ticketID := strings.TrimSpace(msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	title, err := validateChargeTitle(msg.GetTitle())
	if err != nil {
		return nil, err
	}
	participants, err := validateChargeParticipants(msg.GetParticipants())
	if err != nil {
		return nil, err
	}

	if _, err := s.q.GetTicketByID(ctx, ticketID); errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}

	// 登録はチケットの参加者なら誰でもできる（幹事はチケット立替者と別人のことが多い）。
	// 立替者はログイン中の user で固定する。
	if user.Role != auth.RoleAdmin {
		count, err := s.q.CountTicketParticipant(ctx, queries.CountTicketParticipantParams{
			TicketID: ticketID,
			UserID:   user.ID,
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の存在確認に失敗: %w", err))
		}
		if count == 0 {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("追加精算の登録は admin もしくは ticket の参加者のみ"))
		}
	}

	if err := s.assertChargeParticipantsAreTicketParticipants(ctx, ticketID, participants); err != nil {
		return nil, err
	}

	chargeID := id.New()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.CreateTicketCharge(ctx, queries.CreateTicketChargeParams{
		ID:       chargeID,
		TicketID: ticketID,
		Title:    title,
		PaidBy:   user.ID,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の登録に失敗: %w", err))
	}
	for _, p := range participants {
		if err := qtx.CreateTicketChargeParticipant(ctx, queries.CreateTicketChargeParticipantParams{
			ChargeID: chargeID,
			UserID:   p.GetUserId(),
			Amount:   p.GetAmount(),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の対象者の登録に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.CreateTicketChargeResponse{}), nil
}

func (s *ticketService) UpdateTicketCharge(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketChargeRequest]) (*connect.Response[nazobuv1.UpdateTicketChargeResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	chargeID := strings.TrimSpace(msg.GetChargeId())
	if chargeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("charge_id は必須"))
	}
	title, err := validateChargeTitle(msg.GetTitle())
	if err != nil {
		return nil, err
	}
	participants, err := validateChargeParticipants(msg.GetParticipants())
	if err != nil {
		return nil, err
	}

	charge, err := s.q.GetTicketChargeByID(ctx, chargeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketCharge(user, charge.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("追加精算の編集は admin もしくは立替者のみ"))
	}

	if err := s.assertChargeParticipantsAreTicketParticipants(ctx, charge.TicketID, participants); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.UpdateTicketCharge(ctx, queries.UpdateTicketChargeParams{
		ID:    chargeID,
		Title: title,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の更新に失敗: %w", err))
	}

	// 対象者は全置換。既存対象者は金額のみ上書きして精算状態（settled_at）を維持し、
	// 指定から外れた対象者は削除、新しい対象者は未精算で追加する。
	existing, err := qtx.ListTicketChargeParticipantsByChargeID(ctx, chargeID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("既存対象者の取得に失敗: %w", err))
	}
	existingByID := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingByID[e.UserID] = struct{}{}
	}
	requested := make(map[string]struct{}, len(participants))
	for _, p := range participants {
		requested[p.GetUserId()] = struct{}{}
		if _, ok := existingByID[p.GetUserId()]; ok {
			if err := qtx.UpdateTicketChargeParticipantAmount(ctx, queries.UpdateTicketChargeParticipantAmountParams{
				Amount:   p.GetAmount(),
				ChargeID: chargeID,
				UserID:   p.GetUserId(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の金額更新に失敗: %w", err))
			}
		} else {
			if err := qtx.CreateTicketChargeParticipant(ctx, queries.CreateTicketChargeParticipantParams{
				ChargeID: chargeID,
				UserID:   p.GetUserId(),
				Amount:   p.GetAmount(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の追加に失敗: %w", err))
			}
		}
	}
	for _, e := range existing {
		if _, ok := requested[e.UserID]; !ok {
			if err := qtx.DeleteTicketChargeParticipant(ctx, queries.DeleteTicketChargeParticipantParams{
				ChargeID: chargeID,
				UserID:   e.UserID,
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の削除に失敗: %w", err))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketChargeResponse{}), nil
}

func (s *ticketService) DeleteTicketCharge(ctx context.Context, req *connect.Request[nazobuv1.DeleteTicketChargeRequest]) (*connect.Response[nazobuv1.DeleteTicketChargeResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	chargeID := strings.TrimSpace(req.Msg.GetChargeId())
	if chargeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("charge_id は必須"))
	}

	charge, err := s.q.GetTicketChargeByID(ctx, chargeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketCharge(user, charge.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("追加精算の削除は admin もしくは立替者のみ"))
	}

	if err := s.q.DeleteTicketCharge(ctx, chargeID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の削除に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.DeleteTicketChargeResponse{}), nil
}

func (s *ticketService) UpdateTicketChargeSettlement(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketChargeSettlementRequest]) (*connect.Response[nazobuv1.UpdateTicketChargeSettlementResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	chargeID := strings.TrimSpace(req.Msg.GetChargeId())
	userID := strings.TrimSpace(req.Msg.GetUserId())
	settled := req.Msg.GetSettled()
	if chargeID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("charge_id は必須"))
	}
	if userID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id は必須"))
	}

	charge, err := s.q.GetTicketChargeByID(ctx, chargeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketCharge(user, charge.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("精算状態の更新は admin もしくは立替者のみ"))
	}
	if userID == charge.PaidBy {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("立替者本人は精算操作の対象外"))
	}

	count, err := s.q.CountTicketChargeParticipant(ctx, queries.CountTicketChargeParticipantParams{
		ChargeID: chargeID,
		UserID:   userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された対象者は追加精算に紐づいていない"))
	}

	if settled {
		if err := s.q.MarkTicketChargeParticipantSettled(ctx, queries.MarkTicketChargeParticipantSettledParams{
			ChargeID: chargeID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("精算済みの登録に失敗: %w", err))
		}
	} else {
		if err := s.q.MarkTicketChargeParticipantUnsettled(ctx, queries.MarkTicketChargeParticipantUnsettledParams{
			ChargeID: chargeID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の登録に失敗: %w", err))
		}
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketChargeSettlementResponse{}), nil
}

// canEditTicketCharge は admin もしくは charge の立替者本人なら編集可とする。
// チケット本体（canEditTicket）とは別で、チケット立替者であっても他人の charge は編集できない。
func canEditTicketCharge(user *auth.User, paidBy string) bool {
	return user.Role == auth.RoleAdmin || user.ID == paidBy
}

// validateChargeTitle は費目名の必須・長さを検証し、trim 済みの値を返す。
func validateChargeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("title は必須"))
	}
	if len(title) > chargeTitleMaxLen {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("title は %d 文字以内", chargeTitleMaxLen))
	}
	return title, nil
}

// validateChargeParticipants は対象者リストの必須・重複・金額の下限を検証する。
// 金額は対象者ごとに異なってよい（不均等割りを許容する）。
func validateChargeParticipants(in []*nazobuv1.TicketChargeParticipantInput) ([]*nazobuv1.TicketChargeParticipantInput, error) {
	if len(in) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants は 1 件以上"))
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]*nazobuv1.TicketChargeParticipantInput, 0, len(in))
	for _, p := range in {
		userID := strings.TrimSpace(p.GetUserId())
		if userID == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants の user_id は必須"))
		}
		if _, ok := seen[userID]; ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants の user_id が重複している"))
		}
		seen[userID] = struct{}{}
		if p.GetAmount() < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants の amount は 0 以上"))
		}
		out = append(out, &nazobuv1.TicketChargeParticipantInput{UserId: userID, Amount: p.GetAmount()})
	}
	return out, nil
}

// assertChargeParticipantsAreTicketParticipants は charge の対象者が全員
// ticket の参加者であることを確認する（対象者はチケット参加者から選ぶ仕様）。
func (s *ticketService) assertChargeParticipantsAreTicketParticipants(ctx context.Context, ticketID string, participants []*nazobuv1.TicketChargeParticipantInput) error {
	for _, p := range participants {
		count, err := s.q.CountTicketParticipant(ctx, queries.CountTicketParticipantParams{
			TicketID: ticketID,
			UserID:   p.GetUserId(),
		})
		if err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の存在確認に失敗: %w", err))
		}
		if count == 0 {
			return connect.NewError(connect.CodeFailedPrecondition, errors.New("追加精算の対象者は ticket の参加者から選ぶ"))
		}
	}
	return nil
}

// loadTicketCharges は ticket に紐づく追加精算を表示用の形で組み立てる。GetTicket 用。
func loadTicketCharges(ctx context.Context, q *queries.Queries, user *auth.User, ticketID string) ([]*nazobuv1.TicketCharge, error) {
	rows, err := q.ListTicketChargesByTicketID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	charges := make([]*nazobuv1.TicketCharge, 0, len(rows))
	if len(rows) == 0 {
		return charges, nil
	}

	indexByCharge := make(map[string]*nazobuv1.TicketCharge, len(rows))
	chargeIDs := make([]string, 0, len(rows))
	paidByByCharge := make(map[string]string, len(rows))
	for _, r := range rows {
		c := &nazobuv1.TicketCharge{
			Id:           r.ID,
			Title:        r.Title,
			PaidByUserId: r.PaidBy,
			PayerName:    r.PayerName,
			Participants: []*nazobuv1.TicketChargeParticipant{},
			CanEdit:      canEditTicketCharge(user, r.PaidBy),
		}
		charges = append(charges, c)
		indexByCharge[r.ID] = c
		chargeIDs = append(chargeIDs, r.ID)
		paidByByCharge[r.ID] = r.PaidBy
	}

	parts, err := q.ListTicketChargeParticipantsByChargeIDs(ctx, chargeIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range parts {
		c, ok := indexByCharge[p.ChargeID]
		if !ok {
			continue
		}
		isPayer := p.UserID == paidByByCharge[p.ChargeID]
		c.Participants = append(c.Participants, &nazobuv1.TicketChargeParticipant{
			UserId:  p.UserID,
			Name:    p.Name,
			Amount:  p.Amount,
			Settled: isPayer || p.SettledAt.Valid,
			IsPayer: isPayer,
		})
	}
	return charges, nil
}
