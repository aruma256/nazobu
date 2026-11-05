-- name: ListTickets :many
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.unregistered_participants_count,
       t.meeting_place,
       t.memo,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
ORDER BY t.start_at DESC, t.id ASC;

-- name: ListTicketsByIDs :many
-- 列は ListTickets と揃える。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.unregistered_participants_count,
       t.meeting_place,
       t.memo,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.id IN (sqlc.slice('ids'))
ORDER BY t.start_at DESC, t.id ASC;

-- name: CreateTicket :exec
INSERT INTO tickets (id, event_id, start_at, meeting_at, price_per_person, max_participants, unregistered_participants_count, purchased_by, meeting_place, memo, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: CreateTicketParticipant :exec
INSERT INTO ticket_participants (ticket_id, user_id, created_at)
VALUES (?, ?, NOW(6));

-- name: ListTicketParticipantNamesByTicketIDs :many
SELECT tp.ticket_id,
       u.display_name AS name
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
ORDER BY tp.ticket_id, tp.created_at ASC;

-- name: GetTicketByID :one
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       e.discord_spoiler_channel_id,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.unregistered_participants_count,
       t.meeting_place,
       t.memo,
       t.purchased_by,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id  = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.id = ?;

-- name: ListTicketParticipantDiscordIdentities :many
-- identity の欠落を検出できるよう LEFT JOIN にする。
SELECT tp.user_id,
       u.display_name,
       ui.subject AS discord_subject
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
LEFT JOIN user_identities ui ON ui.user_id = tp.user_id AND ui.provider = 'discord'
WHERE tp.ticket_id = ?
ORDER BY tp.created_at ASC, tp.user_id ASC;

-- name: ListTicketParticipantsByTicketID :many
SELECT tp.user_id,
       u.display_name AS name,
       tp.settled_at
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id = ?
ORDER BY tp.created_at ASC, tp.user_id ASC;

-- name: UpdateTicket :exec
UPDATE tickets
SET start_at         = ?,
    meeting_at       = ?,
    price_per_person = ?,
    max_participants = ?,
    unregistered_participants_count = ?,
    meeting_place    = ?,
    memo             = ?,
    purchased_by     = ?,
    updated_at       = NOW(6)
WHERE id = ?;

-- name: DeleteTicketParticipant :exec
DELETE FROM ticket_participants WHERE ticket_id = ? AND user_id = ?;

-- name: MarkTicketParticipantSettled :exec
UPDATE ticket_participants
SET settled_at = NOW(6)
WHERE ticket_id = ? AND user_id = ?;

-- name: MarkTicketParticipantUnsettled :exec
UPDATE ticket_participants
SET settled_at = NULL
WHERE ticket_id = ? AND user_id = ?;

-- name: CountTicketParticipant :one
SELECT COUNT(*) FROM ticket_participants WHERE ticket_id = ? AND user_id = ?;

-- name: CountTicketParticipantsByTicketID :one
SELECT COUNT(*) FROM ticket_participants WHERE ticket_id = ?;

-- name: LockTicketForDeletion :one
-- 削除の権限判定と参加者削除の間に立替者変更・参加者追加が割り込むのを防ぐ。
SELECT purchased_by FROM tickets WHERE id = ? FOR UPDATE;

-- name: DeleteTicketParticipants :exec
DELETE FROM ticket_participants WHERE ticket_id = ?;

-- name: DeleteTicket :exec
DELETE FROM tickets WHERE id = ?;
