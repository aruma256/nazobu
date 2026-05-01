-- name: ListTickets :many
-- ticket 一覧画面用。event 名と立替者名を join して返す。
SELECT t.id, t.event_id, e.title AS event_title,
       t.attended_on, t.price_per_person,
       t.meeting_time, t.meeting_place,
       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
ORDER BY t.attended_on DESC, t.id ASC;

-- name: ListTicketsByIDs :many
-- CreateTicket 直後の返却用。1 件のことが多いがインタフェースは ListTickets と揃える。
SELECT t.id, t.event_id, e.title AS event_title,
       t.attended_on, t.price_per_person,
       t.meeting_time, t.meeting_place,
       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.id IN (sqlc.slice('ids'))
ORDER BY t.attended_on DESC, t.id ASC;

-- name: CreateTicket :exec
INSERT INTO tickets (id, event_id, attended_on, price_per_person, purchased_by, meeting_time, meeting_place, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: CreateTicketParticipant :exec
INSERT INTO ticket_participants (ticket_id, user_id, created_at)
VALUES (?, ?, NOW(6));

-- name: ListTicketParticipantNamesByTicketIDs :many
-- ticket 一覧 / 公演一覧で、各 ticket の参加者名をまとめて引く（N+1 回避）。
-- 呼び出し側で ticket_id ごとに in-memory で振り分ける。
SELECT tp.ticket_id,
       COALESCE(NULLIF(u.display_name, ''), u.username) AS name
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
ORDER BY tp.ticket_id, tp.created_at ASC;
