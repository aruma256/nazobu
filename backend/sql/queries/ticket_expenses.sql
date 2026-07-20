-- name: ListTicketExpensesByTicketID :many
-- ticket 詳細用。追加精算の一覧を立替者名つきで created_at 昇順で返す。
SELECT te.id, te.ticket_id, te.title, te.paid_by,
       u.display_name AS payer_name
FROM ticket_expenses te
JOIN users u ON u.id = te.paid_by
WHERE te.ticket_id = ?
ORDER BY te.created_at ASC, te.id ASC;

-- name: ListTicketExpenseParticipantsByExpenseIDs :many
-- ticket 詳細用。複数 expense の対象者をまとめて引く（N+1 回避）。
-- 呼び出し側で expense_id ごとに in-memory で振り分ける。
SELECT tep.expense_id, tep.user_id, tep.amount, tep.settled_at,
       u.display_name AS name
FROM ticket_expense_participants tep
JOIN users u ON u.id = tep.user_id
WHERE tep.expense_id IN (sqlc.slice('expense_ids'))
ORDER BY tep.expense_id, tep.created_at ASC, tep.user_id ASC;

-- name: GetTicketExpenseByID :one
-- 権限判定（立替者かどうか）と ticket との紐付き確認に使う。
SELECT id, ticket_id, title, paid_by
FROM ticket_expenses
WHERE id = ?;

-- name: CreateTicketExpense :exec
INSERT INTO ticket_expenses (id, ticket_id, title, paid_by, created_at, updated_at)
VALUES (?, ?, ?, ?, NOW(6), NOW(6));

-- name: UpdateTicketExpense :exec
-- expense 本体の更新。ticket_id / paid_by は変更しない。
UPDATE ticket_expenses
SET title      = ?,
    updated_at = NOW(6)
WHERE id = ?;

-- name: DeleteTicketExpense :exec
-- 対象者は FK の ON DELETE CASCADE で一緒に消える。
DELETE FROM ticket_expenses WHERE id = ?;

-- name: ListTicketExpenseParticipantsByExpenseID :many
-- expense 更新時の差分計算用。既存対象者の user_id と精算状態を返す。
SELECT user_id, amount, settled_at
FROM ticket_expense_participants
WHERE expense_id = ?
ORDER BY created_at ASC, user_id ASC;

-- name: CreateTicketExpenseParticipant :exec
INSERT INTO ticket_expense_participants (expense_id, user_id, amount, created_at)
VALUES (?, ?, ?, NOW(6));

-- name: UpdateTicketExpenseParticipantAmount :exec
-- 金額のみ変更する。精算状態（settled_at）は維持する。
UPDATE ticket_expense_participants
SET amount = ?
WHERE expense_id = ? AND user_id = ?;

-- name: DeleteTicketExpenseParticipant :exec
DELETE FROM ticket_expense_participants WHERE expense_id = ? AND user_id = ?;

-- name: CountTicketExpenseParticipant :one
-- 対象者の存在確認。精算トグルのプリチェックで使う。
SELECT COUNT(*) FROM ticket_expense_participants WHERE expense_id = ? AND user_id = ?;

-- name: MarkTicketExpenseParticipantSettled :exec
-- 未精算 → 精算済み。settled_at に現在時刻を入れる。
UPDATE ticket_expense_participants
SET settled_at = NOW(6)
WHERE expense_id = ? AND user_id = ?;

-- name: MarkTicketExpenseParticipantUnsettled :exec
-- 精算済み → 未精算。settled_at を NULL に戻す。
UPDATE ticket_expense_participants
SET settled_at = NULL
WHERE expense_id = ? AND user_id = ?;
