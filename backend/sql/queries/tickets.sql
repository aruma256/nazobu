-- name: ListTickets :many
-- ticket 一覧画面用。event 名と立替者名を join して返す。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url,
       t.attended_on, t.price_per_person,
       t.meeting_time, t.meeting_place, t.start_time,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
ORDER BY t.attended_on DESC, t.id ASC;

-- name: ListTicketsByIDs :many
-- CreateTicket 直後の返却用。1 件のことが多いがインタフェースは ListTickets と揃える。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url,
       t.attended_on, t.price_per_person,
       t.meeting_time, t.meeting_place, t.start_time,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.id IN (sqlc.slice('ids'))
ORDER BY t.attended_on DESC, t.id ASC;

-- name: CreateTicket :exec
INSERT INTO tickets (id, event_id, attended_on, price_per_person, purchased_by, meeting_time, meeting_place, start_time, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: CreateTicketParticipant :exec
INSERT INTO ticket_participants (ticket_id, user_id, created_at)
VALUES (?, ?, NOW(6));

-- name: ListTicketParticipantNamesByTicketIDs :many
-- ticket 一覧 / 公演一覧で、各 ticket の参加者名をまとめて引く（N+1 回避）。
-- 呼び出し側で ticket_id ごとに in-memory で振り分ける。
SELECT tp.ticket_id,
       u.display_name AS name
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
ORDER BY tp.ticket_id, tp.created_at ASC;

-- name: GetTicketByID :one
-- ticket 詳細表示用。立替者の id と表示名も返す（権限判定 / UI 表示で使う）。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url,
       t.attended_on, t.price_per_person,
       t.meeting_time, t.meeting_place, t.start_time,
       t.purchased_by,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.id = ?;

-- name: ListTicketParticipantsByTicketID :many
-- ticket 詳細用。参加者の user_id / 名前 / 精算済みフラグを created_at 昇順で返す。
SELECT tp.user_id,
       u.display_name AS name,
       tp.settled_at
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id = ?
ORDER BY tp.created_at ASC, tp.user_id ASC;

-- name: UpdateTicket :exec
-- ticket 本体の更新。event_id / purchased_by は変更しない。
UPDATE tickets
SET attended_on      = ?,
    price_per_person = ?,
    meeting_time     = ?,
    meeting_place    = ?,
    start_time       = ?,
    updated_at       = NOW(6)
WHERE id = ?;

-- name: DeleteTicketParticipant :exec
DELETE FROM ticket_participants WHERE ticket_id = ? AND user_id = ?;

-- name: MarkTicketParticipantSettled :exec
-- 未精算 → 精算済み。settled_at に現在時刻を入れる。
UPDATE ticket_participants
SET settled_at = NOW(6)
WHERE ticket_id = ? AND user_id = ?;

-- name: MarkTicketParticipantUnsettled :exec
-- 精算済み → 未精算。settled_at を NULL に戻す。
UPDATE ticket_participants
SET settled_at = NULL
WHERE ticket_id = ? AND user_id = ?;

-- name: CountTicketParticipant :one
-- 参加者の存在確認。重複登録を避けるためのプリチェック。
SELECT COUNT(*) FROM ticket_participants WHERE ticket_id = ? AND user_id = ?;
