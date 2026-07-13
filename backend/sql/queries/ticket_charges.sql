-- name: ListTicketChargesByTicketID :many
-- ticket 詳細用。追加精算の一覧を立替者名つきで created_at 昇順で返す。
SELECT tc.id, tc.ticket_id, tc.title, tc.paid_by,
       u.display_name AS payer_name
FROM ticket_charges tc
JOIN users u ON u.id = tc.paid_by
WHERE tc.ticket_id = ?
ORDER BY tc.created_at ASC, tc.id ASC;

-- name: ListTicketChargeParticipantsByChargeIDs :many
-- ticket 詳細用。複数 charge の対象者をまとめて引く（N+1 回避）。
-- 呼び出し側で charge_id ごとに in-memory で振り分ける。
SELECT tcp.charge_id, tcp.user_id, tcp.amount, tcp.settled_at,
       u.display_name AS name
FROM ticket_charge_participants tcp
JOIN users u ON u.id = tcp.user_id
WHERE tcp.charge_id IN (sqlc.slice('charge_ids'))
ORDER BY tcp.charge_id, tcp.created_at ASC, tcp.user_id ASC;

-- name: GetTicketChargeByID :one
-- 権限判定（立替者かどうか）と ticket との紐付き確認に使う。
SELECT id, ticket_id, title, paid_by
FROM ticket_charges
WHERE id = ?;

-- name: CreateTicketCharge :exec
INSERT INTO ticket_charges (id, ticket_id, title, paid_by, created_at, updated_at)
VALUES (?, ?, ?, ?, NOW(6), NOW(6));

-- name: UpdateTicketCharge :exec
-- charge 本体の更新。ticket_id / paid_by は変更しない。
UPDATE ticket_charges
SET title      = ?,
    updated_at = NOW(6)
WHERE id = ?;

-- name: DeleteTicketCharge :exec
-- 対象者は FK の ON DELETE CASCADE で一緒に消える。
DELETE FROM ticket_charges WHERE id = ?;

-- name: ListTicketChargeParticipantsByChargeID :many
-- charge 更新時の差分計算用。既存対象者の user_id と精算状態を返す。
SELECT user_id, amount, settled_at
FROM ticket_charge_participants
WHERE charge_id = ?
ORDER BY created_at ASC, user_id ASC;

-- name: CreateTicketChargeParticipant :exec
INSERT INTO ticket_charge_participants (charge_id, user_id, amount, created_at)
VALUES (?, ?, ?, NOW(6));

-- name: UpdateTicketChargeParticipantAmount :exec
-- 金額のみ変更する。精算状態（settled_at）は維持する。
UPDATE ticket_charge_participants
SET amount = ?
WHERE charge_id = ? AND user_id = ?;

-- name: DeleteTicketChargeParticipant :exec
DELETE FROM ticket_charge_participants WHERE charge_id = ? AND user_id = ?;

-- name: CountTicketChargeParticipant :one
-- 対象者の存在確認。精算トグルのプリチェックで使う。
SELECT COUNT(*) FROM ticket_charge_participants WHERE charge_id = ? AND user_id = ?;

-- name: MarkTicketChargeParticipantSettled :exec
-- 未精算 → 精算済み。settled_at に現在時刻を入れる。
UPDATE ticket_charge_participants
SET settled_at = NOW(6)
WHERE charge_id = ? AND user_id = ?;

-- name: MarkTicketChargeParticipantUnsettled :exec
-- 精算済み → 未精算。settled_at を NULL に戻す。
UPDATE ticket_charge_participants
SET settled_at = NULL
WHERE charge_id = ? AND user_id = ?;
