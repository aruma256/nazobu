-- name: ListExpenses :many
-- expense 一覧画面用。立替者名と、ticket 紐付き時の公演名を join して返す。
SELECT ex.id, ex.ticket_id, ex.title, ex.occurred_on,
       ex.paid_by,
       pu.display_name AS payer_name,
       e.title AS event_title
FROM expenses ex
JOIN users pu ON pu.id = ex.paid_by
LEFT JOIN tickets t ON t.id = ex.ticket_id
LEFT JOIN events  e ON e.id = t.event_id
ORDER BY ex.occurred_on DESC, ex.id ASC;

-- name: ListExpensesByTicketID :many
-- ticket 詳細画面の「追加の精算」セクション用。
SELECT ex.id, ex.ticket_id, ex.title, ex.occurred_on,
       ex.paid_by,
       pu.display_name AS payer_name,
       e.title AS event_title
FROM expenses ex
JOIN users pu ON pu.id = ex.paid_by
LEFT JOIN tickets t ON t.id = ex.ticket_id
LEFT JOIN events  e ON e.id = t.event_id
WHERE ex.ticket_id = ?
ORDER BY ex.occurred_on DESC, ex.id ASC;

-- name: GetExpenseByID :one
-- expense 詳細表示・権限判定用。
SELECT ex.id, ex.ticket_id, ex.title, ex.occurred_on,
       ex.paid_by,
       pu.display_name AS payer_name,
       e.title AS event_title
FROM expenses ex
JOIN users pu ON pu.id = ex.paid_by
LEFT JOIN tickets t ON t.id = ex.ticket_id
LEFT JOIN events  e ON e.id = t.event_id
WHERE ex.id = ?;

-- name: CreateExpense :exec
INSERT INTO expenses (id, ticket_id, title, paid_by, occurred_on, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: UpdateExpense :exec
-- expense 本体の更新。参加者の付け替えは呼び出し側が別クエリで行う。
UPDATE expenses
SET ticket_id   = ?,
    title       = ?,
    paid_by     = ?,
    occurred_on = ?,
    updated_at  = NOW(6)
WHERE id = ?;

-- name: DeleteExpense :exec
-- expense_participants は FK CASCADE で同時に消える。
DELETE FROM expenses WHERE id = ?;

-- name: ListExpenseParticipantsByExpenseID :many
-- expense 詳細用。参加者の user_id / 名前 / 負担額 / 精算済みフラグを created_at 昇順で返す。
SELECT ep.user_id,
       u.display_name AS name,
       ep.amount,
       ep.settled_at
FROM expense_participants ep
JOIN users u ON u.id = ep.user_id
WHERE ep.expense_id = ?
ORDER BY ep.created_at ASC, ep.user_id ASC;

-- name: ListExpenseParticipantsByExpenseIDs :many
-- expense 一覧で、各 expense の参加者（金額・精算状態つき）をまとめて引く（N+1 回避）。
-- 呼び出し側で expense_id ごとに in-memory で振り分ける。
SELECT ep.expense_id,
       ep.user_id,
       u.display_name AS name,
       ep.amount,
       ep.settled_at
FROM expense_participants ep
JOIN users u ON u.id = ep.user_id
WHERE ep.expense_id IN (sqlc.slice('expense_ids'))
ORDER BY ep.expense_id, ep.created_at ASC, ep.user_id ASC;

-- name: CreateExpenseParticipant :exec
INSERT INTO expense_participants (expense_id, user_id, amount, created_at)
VALUES (?, ?, ?, NOW(6));

-- name: UpdateExpenseParticipantAmount :exec
-- 参加者の負担額のみ更新する。settled_at は保持する。
UPDATE expense_participants
SET amount = ?
WHERE expense_id = ? AND user_id = ?;

-- name: DeleteExpenseParticipant :exec
DELETE FROM expense_participants WHERE expense_id = ? AND user_id = ?;

-- name: MarkExpenseParticipantSettled :exec
-- 未精算 → 精算済み。settled_at に現在時刻を入れる。
UPDATE expense_participants
SET settled_at = NOW(6)
WHERE expense_id = ? AND user_id = ?;

-- name: MarkExpenseParticipantUnsettled :exec
-- 精算済み → 未精算。settled_at を NULL に戻す。
UPDATE expense_participants
SET settled_at = NULL
WHERE expense_id = ? AND user_id = ?;

-- name: CountExpenseParticipant :one
-- 参加者の存在確認。精算操作の対象チェックで使う。
SELECT COUNT(*) FROM expense_participants WHERE expense_id = ? AND user_id = ?;

-- name: CountTicketByID :one
-- expense 登録時の ticket_id 存在確認。
SELECT COUNT(*) FROM tickets WHERE id = ?;
