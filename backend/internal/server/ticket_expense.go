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

const expenseTitleMaxLen = 255

func (s *ticketService) CreateTicketExpense(ctx context.Context, req *connect.Request[nazobuv1.CreateTicketExpenseRequest]) (*connect.Response[nazobuv1.CreateTicketExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	ticketID := strings.TrimSpace(msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	title, err := validateExpenseTitle(msg.GetTitle())
	if err != nil {
		return nil, err
	}
	participants, err := validateExpenseParticipants(msg.GetParticipants())
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

	if err := s.assertExpenseParticipantsAreTicketParticipants(ctx, ticketID, participants); err != nil {
		return nil, err
	}

	expenseID := id.New()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.CreateTicketExpense(ctx, queries.CreateTicketExpenseParams{
		ID:       expenseID,
		TicketID: ticketID,
		Title:    title,
		PaidBy:   user.ID,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の登録に失敗: %w", err))
	}
	for _, p := range participants {
		if err := qtx.CreateTicketExpenseParticipant(ctx, queries.CreateTicketExpenseParticipantParams{
			ExpenseID: expenseID,
			UserID:   p.GetUserId(),
			Amount:   p.GetAmount(),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の対象者の登録に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.CreateTicketExpenseResponse{}), nil
}

func (s *ticketService) UpdateTicketExpense(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketExpenseRequest]) (*connect.Response[nazobuv1.UpdateTicketExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	expenseID := strings.TrimSpace(msg.GetExpenseId())
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}
	title, err := validateExpenseTitle(msg.GetTitle())
	if err != nil {
		return nil, err
	}
	participants, err := validateExpenseParticipants(msg.GetParticipants())
	if err != nil {
		return nil, err
	}

	expense, err := s.q.GetTicketExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketExpense(user, expense.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("追加精算の編集は admin もしくは立替者のみ"))
	}

	if err := s.assertExpenseParticipantsAreTicketParticipants(ctx, expense.TicketID, participants); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.UpdateTicketExpense(ctx, queries.UpdateTicketExpenseParams{
		ID:    expenseID,
		Title: title,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の更新に失敗: %w", err))
	}

	// 対象者は全置換。既存対象者は金額のみ上書きして精算状態（settled_at）を維持し、
	// 指定から外れた対象者は削除、新しい対象者は未精算で追加する。
	existing, err := qtx.ListTicketExpenseParticipantsByExpenseID(ctx, expenseID)
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
			if err := qtx.UpdateTicketExpenseParticipantAmount(ctx, queries.UpdateTicketExpenseParticipantAmountParams{
				Amount:   p.GetAmount(),
				ExpenseID: expenseID,
				UserID:   p.GetUserId(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の金額更新に失敗: %w", err))
			}
		} else {
			if err := qtx.CreateTicketExpenseParticipant(ctx, queries.CreateTicketExpenseParticipantParams{
				ExpenseID: expenseID,
				UserID:   p.GetUserId(),
				Amount:   p.GetAmount(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の追加に失敗: %w", err))
			}
		}
	}
	for _, e := range existing {
		if _, ok := requested[e.UserID]; !ok {
			if err := qtx.DeleteTicketExpenseParticipant(ctx, queries.DeleteTicketExpenseParticipantParams{
				ExpenseID: expenseID,
				UserID:   e.UserID,
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の削除に失敗: %w", err))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketExpenseResponse{}), nil
}

func (s *ticketService) DeleteTicketExpense(ctx context.Context, req *connect.Request[nazobuv1.DeleteTicketExpenseRequest]) (*connect.Response[nazobuv1.DeleteTicketExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	expenseID := strings.TrimSpace(req.Msg.GetExpenseId())
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}

	expense, err := s.q.GetTicketExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketExpense(user, expense.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("追加精算の削除は admin もしくは立替者のみ"))
	}

	if err := s.q.DeleteTicketExpense(ctx, expenseID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の削除に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.DeleteTicketExpenseResponse{}), nil
}

func (s *ticketService) UpdateTicketExpenseSettlement(ctx context.Context, req *connect.Request[nazobuv1.UpdateTicketExpenseSettlementRequest]) (*connect.Response[nazobuv1.UpdateTicketExpenseSettlementResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	expenseID := strings.TrimSpace(req.Msg.GetExpenseId())
	userID := strings.TrimSpace(req.Msg.GetUserId())
	settled := req.Msg.GetSettled()
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}
	if userID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id は必須"))
	}

	expense, err := s.q.GetTicketExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された追加精算は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("追加精算の取得に失敗: %w", err))
	}
	if !canEditTicketExpense(user, expense.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("精算状態の更新は admin もしくは立替者のみ"))
	}
	if userID == expense.PaidBy {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("立替者本人は精算操作の対象外"))
	}

	count, err := s.q.CountTicketExpenseParticipant(ctx, queries.CountTicketExpenseParticipantParams{
		ExpenseID: expenseID,
		UserID:   userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("対象者の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された対象者は追加精算に紐づいていない"))
	}

	if settled {
		if err := s.q.MarkTicketExpenseParticipantSettled(ctx, queries.MarkTicketExpenseParticipantSettledParams{
			ExpenseID: expenseID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("精算済みの登録に失敗: %w", err))
		}
	} else {
		if err := s.q.MarkTicketExpenseParticipantUnsettled(ctx, queries.MarkTicketExpenseParticipantUnsettledParams{
			ExpenseID: expenseID,
			UserID:   userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の登録に失敗: %w", err))
		}
	}
	return connect.NewResponse(&nazobuv1.UpdateTicketExpenseSettlementResponse{}), nil
}

// canEditTicketExpense は admin もしくは expense の立替者本人なら編集可とする。
// チケット本体（canEditTicket）とは別で、チケット立替者であっても他人の expense は編集できない。
func canEditTicketExpense(user *auth.User, paidBy string) bool {
	return user.Role == auth.RoleAdmin || user.ID == paidBy
}

// validateExpenseTitle は費目名の必須・長さを検証し、trim 済みの値を返す。
func validateExpenseTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("title は必須"))
	}
	if len(title) > expenseTitleMaxLen {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("title は %d 文字以内", expenseTitleMaxLen))
	}
	return title, nil
}

// validateExpenseParticipants は対象者リストの必須・重複・金額の下限を検証する。
// 金額は対象者ごとに異なってよい（不均等割りを許容する）。
func validateExpenseParticipants(in []*nazobuv1.TicketExpenseParticipantInput) ([]*nazobuv1.TicketExpenseParticipantInput, error) {
	if len(in) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants は 1 件以上"))
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]*nazobuv1.TicketExpenseParticipantInput, 0, len(in))
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
		out = append(out, &nazobuv1.TicketExpenseParticipantInput{UserId: userID, Amount: p.GetAmount()})
	}
	return out, nil
}

// assertExpenseParticipantsAreTicketParticipants は expense の対象者が全員
// ticket の参加者であることを確認する（対象者はチケット参加者から選ぶ仕様）。
func (s *ticketService) assertExpenseParticipantsAreTicketParticipants(ctx context.Context, ticketID string, participants []*nazobuv1.TicketExpenseParticipantInput) error {
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

// loadTicketExpenses は ticket に紐づく追加精算を表示用の形で組み立てる。GetTicket 用。
func loadTicketExpenses(ctx context.Context, q *queries.Queries, user *auth.User, ticketID string) ([]*nazobuv1.TicketExpense, error) {
	rows, err := q.ListTicketExpensesByTicketID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	expenses := make([]*nazobuv1.TicketExpense, 0, len(rows))
	if len(rows) == 0 {
		return expenses, nil
	}

	indexByExpense := make(map[string]*nazobuv1.TicketExpense, len(rows))
	expenseIDs := make([]string, 0, len(rows))
	paidByByExpense := make(map[string]string, len(rows))
	for _, r := range rows {
		c := &nazobuv1.TicketExpense{
			Id:           r.ID,
			Title:        r.Title,
			PaidByUserId: r.PaidBy,
			PayerName:    r.PayerName,
			Participants: []*nazobuv1.TicketExpenseParticipant{},
			CanEdit:      canEditTicketExpense(user, r.PaidBy),
		}
		expenses = append(expenses, c)
		indexByExpense[r.ID] = c
		expenseIDs = append(expenseIDs, r.ID)
		paidByByExpense[r.ID] = r.PaidBy
	}

	parts, err := q.ListTicketExpenseParticipantsByExpenseIDs(ctx, expenseIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range parts {
		c, ok := indexByExpense[p.ExpenseID]
		if !ok {
			continue
		}
		isPayer := p.UserID == paidByByExpense[p.ExpenseID]
		c.Participants = append(c.Participants, &nazobuv1.TicketExpenseParticipant{
			UserId:  p.UserID,
			Name:    p.Name,
			Amount:  p.Amount,
			Settled: isPayer || p.SettledAt.Valid,
			IsPayer: isPayer,
		})
	}
	return expenses, nil
}
