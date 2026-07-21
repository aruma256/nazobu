package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

type expenseService struct {
	db *sql.DB
	q  *queries.Queries
}

func newExpenseService(db *sql.DB) nazobuv1connect.ExpenseServiceHandler {
	return &expenseService{db: db, q: queries.New(db)}
}

const expenseTitleMaxLen = 255

// expenseListRow は ListExpenses / ListExpensesByTicketID / GetExpenseByID の
// 共通部分（一覧表示に必要なカラム）。生成型が別なので詰め替えて共有する。
type expenseListRow struct {
	ID         string
	TicketID   sql.NullString
	Title      string
	OccurredOn time.Time
	PaidBy     string
	PayerName  string
	EventTitle sql.NullString
}

func (s *expenseService) ListExpenses(ctx context.Context, req *connect.Request[nazobuv1.ListExpensesRequest]) (*connect.Response[nazobuv1.ListExpensesResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	var rows []expenseListRow
	if ticketID == "" {
		all, err := s.q.ListExpenses(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense 一覧の取得に失敗: %w", err))
		}
		for _, r := range all {
			rows = append(rows, expenseListRow(r))
		}
	} else {
		byTicket, err := s.q.ListExpensesByTicketID(ctx, sql.NullString{String: ticketID, Valid: true})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense 一覧の取得に失敗: %w", err))
		}
		for _, r := range byTicket {
			rows = append(rows, expenseListRow(r))
		}
	}

	expenses := make([]*nazobuv1.Expense, 0, len(rows))
	for _, r := range rows {
		expenses = append(expenses, expenseFromListRow(r))
	}
	if err := attachExpenseParticipantSummaries(ctx, s.q, expenses); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListExpensesResponse{Expenses: expenses}), nil
}

func (s *expenseService) GetExpense(ctx context.Context, req *connect.Request[nazobuv1.GetExpenseRequest]) (*connect.Response[nazobuv1.GetExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	expenseID := strings.TrimSpace(req.Msg.GetExpenseId())
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}

	row, err := s.q.GetExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された expense は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の取得に失敗: %w", err))
	}

	parts, err := s.q.ListExpenseParticipantsByExpenseID(ctx, expenseID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}

	expense := expenseFromListRow(expenseListRow(row))
	participants := make([]*nazobuv1.ExpenseParticipant, 0, len(parts))
	for _, p := range parts {
		participants = append(participants, &nazobuv1.ExpenseParticipant{
			UserId:  p.UserID,
			Name:    p.Name,
			Amount:  p.Amount,
			Settled: p.SettledAt.Valid,
		})
		applyExpenseParticipantSummary(expense, p.Name, p.Amount, p.SettledAt.Valid)
	}

	return connect.NewResponse(&nazobuv1.GetExpenseResponse{
		Expense:      expense,
		Participants: participants,
		CanEdit:      canEditExpense(user, row.PaidBy),
	}), nil
}

func (s *expenseService) CreateExpense(ctx context.Context, req *connect.Request[nazobuv1.CreateExpenseRequest]) (*connect.Response[nazobuv1.CreateExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	// 立替者は常にログイン中の user。member でも登録できる（飲み会の立て替えは admin に限らない）。
	paidBy := user.ID

	msg := req.Msg
	ticketID := strings.TrimSpace(msg.GetTicketId())
	title := strings.TrimSpace(msg.GetTitle())
	if err := validateExpenseTitle(title); err != nil {
		return nil, err
	}
	occurredOn, err := parseRequiredJSTDate(msg.GetOccurredOn(), "occurred_on")
	if err != nil {
		return nil, err
	}
	participants, err := normalizeExpenseParticipants(msg.GetParticipants(), paidBy)
	if err != nil {
		return nil, err
	}

	if ticketID != "" {
		if err := s.assertTicketExists(ctx, ticketID); err != nil {
			return nil, err
		}
	}
	if err := s.assertExpenseUsersExist(ctx, participantUserIDs(participants)); err != nil {
		return nil, err
	}

	expenseID := id.New()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.CreateExpense(ctx, queries.CreateExpenseParams{
		ID:         expenseID,
		TicketID:   nullableString(ticketID),
		Title:      title,
		PaidBy:     paidBy,
		OccurredOn: occurredOn,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の登録に失敗: %w", err))
	}
	for _, p := range participants {
		if err := qtx.CreateExpenseParticipant(ctx, queries.CreateExpenseParticipantParams{
			ExpenseID: expenseID,
			UserID:    p.GetUserId(),
			Amount:    p.GetAmount(),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense_participants の登録に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}

	expense, err := s.buildExpenseResponse(ctx, expenseID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&nazobuv1.CreateExpenseResponse{Expense: expense}), nil
}

func (s *expenseService) UpdateExpense(ctx context.Context, req *connect.Request[nazobuv1.UpdateExpenseRequest]) (*connect.Response[nazobuv1.UpdateExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	msg := req.Msg
	expenseID := strings.TrimSpace(msg.GetExpenseId())
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}

	existing, err := s.q.GetExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された expense は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の取得に失敗: %w", err))
	}
	if !canEditExpense(user, existing.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("expense の編集は admin もしくは立替者のみ"))
	}

	ticketID := strings.TrimSpace(msg.GetTicketId())
	title := strings.TrimSpace(msg.GetTitle())
	paidBy := strings.TrimSpace(msg.GetPaidByUserId())
	if err := validateExpenseTitle(title); err != nil {
		return nil, err
	}
	occurredOn, err := parseRequiredJSTDate(msg.GetOccurredOn(), "occurred_on")
	if err != nil {
		return nil, err
	}
	if paidBy == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("paid_by_user_id は必須"))
	}
	participants, err := normalizeExpenseParticipants(msg.GetParticipants(), paidBy)
	if err != nil {
		return nil, err
	}

	if ticketID != "" {
		if err := s.assertTicketExists(ctx, ticketID); err != nil {
			return nil, err
		}
	}
	// 立替者を変更する場合も含め、参加者 + 立替者の存在をまとめて確認する。
	if err := s.assertExpenseUsersExist(ctx, append(participantUserIDs(participants), paidBy)); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション開始に失敗: %w", err))
	}
	defer func() { _ = tx.Rollback() }()

	qtx := s.q.WithTx(tx)
	if err := qtx.UpdateExpense(ctx, queries.UpdateExpenseParams{
		ID:         expenseID,
		TicketID:   nullableString(ticketID),
		Title:      title,
		PaidBy:     paidBy,
		OccurredOn: occurredOn,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の更新に失敗: %w", err))
	}

	// 参加者は全量置換。残る参加者は amount のみ更新して settled_at を保持する。
	current, err := qtx.ListExpenseParticipantsByExpenseID(ctx, expenseID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	currentByUser := make(map[string]struct{}, len(current))
	for _, c := range current {
		currentByUser[c.UserID] = struct{}{}
	}
	nextByUser := make(map[string]struct{}, len(participants))
	for _, p := range participants {
		nextByUser[p.GetUserId()] = struct{}{}
		if _, ok := currentByUser[p.GetUserId()]; ok {
			if err := qtx.UpdateExpenseParticipantAmount(ctx, queries.UpdateExpenseParticipantAmountParams{
				Amount:    p.GetAmount(),
				ExpenseID: expenseID,
				UserID:    p.GetUserId(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の更新に失敗: %w", err))
			}
		} else {
			if err := qtx.CreateExpenseParticipant(ctx, queries.CreateExpenseParticipantParams{
				ExpenseID: expenseID,
				UserID:    p.GetUserId(),
				Amount:    p.GetAmount(),
			}); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の追加に失敗: %w", err))
			}
		}
	}
	for _, c := range current {
		if _, ok := nextByUser[c.UserID]; ok {
			continue
		}
		if err := qtx.DeleteExpenseParticipant(ctx, queries.DeleteExpenseParticipantParams{
			ExpenseID: expenseID,
			UserID:    c.UserID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の削除に失敗: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("トランザクション commit に失敗: %w", err))
	}

	expense, err := s.buildExpenseResponse(ctx, expenseID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&nazobuv1.UpdateExpenseResponse{Expense: expense}), nil
}

func (s *expenseService) DeleteExpense(ctx context.Context, req *connect.Request[nazobuv1.DeleteExpenseRequest]) (*connect.Response[nazobuv1.DeleteExpenseResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}

	expenseID := strings.TrimSpace(req.Msg.GetExpenseId())
	if expenseID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expense_id は必須"))
	}

	existing, err := s.q.GetExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された expense は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の取得に失敗: %w", err))
	}
	if !canEditExpense(user, existing.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("expense の削除は admin もしくは立替者のみ"))
	}

	if err := s.q.DeleteExpense(ctx, expenseID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の削除に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.DeleteExpenseResponse{}), nil
}

func (s *expenseService) UpdateExpenseParticipantSettlement(ctx context.Context, req *connect.Request[nazobuv1.UpdateExpenseParticipantSettlementRequest]) (*connect.Response[nazobuv1.UpdateExpenseParticipantSettlementResponse], error) {
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

	existing, err := s.q.GetExpenseByID(ctx, expenseID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された expense は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の取得に失敗: %w", err))
	}
	if !canEditExpense(user, existing.PaidBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("精算状態の更新は admin もしくは立替者のみ"))
	}

	count, err := s.q.CountExpenseParticipant(ctx, queries.CountExpenseParticipantParams{
		ExpenseID: expenseID,
		UserID:    userID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された参加者は expense に紐づいていない"))
	}

	if settled {
		if err := s.q.MarkExpenseParticipantSettled(ctx, queries.MarkExpenseParticipantSettledParams{
			ExpenseID: expenseID,
			UserID:    userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("精算済みの登録に失敗: %w", err))
		}
	} else {
		if err := s.q.MarkExpenseParticipantUnsettled(ctx, queries.MarkExpenseParticipantUnsettledParams{
			ExpenseID: expenseID,
			UserID:    userID,
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("未精算の登録に失敗: %w", err))
		}
	}
	return connect.NewResponse(&nazobuv1.UpdateExpenseParticipantSettlementResponse{}), nil
}

// buildExpenseResponse は expense_id から表示用の Expense メッセージ（参加者サマリ込み）を組み立てる。
// Create/Update 系で末尾の「最新の状態を返す」処理を共有する用途。
func (s *expenseService) buildExpenseResponse(ctx context.Context, expenseID string) (*nazobuv1.Expense, error) {
	row, err := s.q.GetExpenseByID(ctx, expenseID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("expense の取得に失敗: %w", err))
	}
	expense := expenseFromListRow(expenseListRow(row))
	if err := attachExpenseParticipantSummaries(ctx, s.q, []*nazobuv1.Expense{expense}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("参加者の取得に失敗: %w", err))
	}
	return expense, nil
}

// canEditExpense は admin もしくは立替者本人なら編集可とする。
func canEditExpense(user *auth.User, paidBy string) bool {
	return user.Role == auth.RoleAdmin || user.ID == paidBy
}

func validateExpenseTitle(title string) error {
	if title == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("title は必須"))
	}
	if len(title) > expenseTitleMaxLen {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("title は %d 文字以内", expenseTitleMaxLen))
	}
	return nil
}

// normalizeExpenseParticipants は参加者入力を検証して返す。
// user_id の重複は金額の解釈が曖昧になるためエラーにする。
// 立替者（paidBy）は精算対象外なので参加者に含められない。
func normalizeExpenseParticipants(in []*nazobuv1.ExpenseParticipantInput, paidBy string) ([]*nazobuv1.ExpenseParticipantInput, error) {
	if len(in) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants は 1 件以上"))
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]*nazobuv1.ExpenseParticipantInput, 0, len(in))
	for _, p := range in {
		uid := strings.TrimSpace(p.GetUserId())
		if uid == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants の user_id は必須"))
		}
		if uid == paidBy {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("立替者自身は参加者に含めない（自分の分は精算不要のため）"))
		}
		if _, ok := seen[uid]; ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("participants の user_id が重複している"))
		}
		if p.GetAmount() < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("amount は 0 以上"))
		}
		seen[uid] = struct{}{}
		out = append(out, &nazobuv1.ExpenseParticipantInput{UserId: uid, Amount: p.GetAmount()})
	}
	return out, nil
}

func participantUserIDs(in []*nazobuv1.ExpenseParticipantInput) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, p.GetUserId())
	}
	return out
}

func (s *expenseService) assertTicketExists(ctx context.Context, ticketID string) error {
	count, err := s.q.CountTicketByID(ctx, ticketID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の存在確認に失敗: %w", err))
	}
	if count == 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("指定された ticket は存在しない"))
	}
	return nil
}

func (s *expenseService) assertExpenseUsersExist(ctx context.Context, ids []string) error {
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

// expenseFromListRow は一覧行から参加者サマリ未設定の Expense メッセージを組み立てる。
func expenseFromListRow(r expenseListRow) *nazobuv1.Expense {
	return &nazobuv1.Expense{
		Id:               r.ID,
		TicketId:         nullStringToString(r.TicketID),
		EventTitle:       nullStringToString(r.EventTitle),
		Title:            r.Title,
		PaidByUserId:     r.PaidBy,
		PayerName:        r.PayerName,
		OccurredOn:       formatJSTDate(r.OccurredOn),
		ParticipantNames: []string{},
	}
}

// applyExpenseParticipantSummary は参加者 1 人分を Expense のサマリ項目に反映する。
func applyExpenseParticipantSummary(e *nazobuv1.Expense, name string, amount int32, settled bool) {
	e.ParticipantNames = append(e.ParticipantNames, name)
	e.TotalAmount += amount
	e.ParticipantCount++
	if settled {
		e.SettledCount++
	}
}

// attachExpenseParticipantSummaries は expenses の各 expense に
// 参加者サマリ（名前 / 合計金額 / 人数 / 精算済み数）を埋める。
func attachExpenseParticipantSummaries(ctx context.Context, q *queries.Queries, expenses []*nazobuv1.Expense) error {
	if len(expenses) == 0 {
		return nil
	}
	indexByExpense := make(map[string]*nazobuv1.Expense, len(expenses))
	expenseIDs := make([]string, 0, len(expenses))
	for _, e := range expenses {
		indexByExpense[e.Id] = e
		expenseIDs = append(expenseIDs, e.Id)
	}

	rows, err := q.ListExpenseParticipantsByExpenseIDs(ctx, expenseIDs)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if e, ok := indexByExpense[r.ExpenseID]; ok {
			applyExpenseParticipantSummary(e, r.Name, r.Amount, r.SettledAt.Valid)
		}
	}
	return nil
}

// nullableString は空文字を NULL として扱う sql.NullString に変換する。
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// formatJSTDate は DATE カラムの time.Time を YYYY-MM-DD 文字列に整形する。
func formatJSTDate(t time.Time) string {
	return t.In(jst).Format("2006-01-02")
}

// parseRequiredJSTDate は YYYY-MM-DD を必須として受け取り、JST 基準の time.Time に変換する。
func parseRequiredJSTDate(raw, field string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s は必須", field))
	}
	t, err := time.ParseInLocation("2006-01-02", s, jst)
	if err != nil {
		return time.Time{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s は YYYY-MM-DD の日付", field))
	}
	return t, nil
}
