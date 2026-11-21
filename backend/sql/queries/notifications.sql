-- name: ListTicketsForDayBeforeNotification :many
SELECT t.id, t.start_at, t.meeting_at, t.meeting_place,
       e.title AS event_title
FROM tickets t
JOIN events e ON e.id = t.event_id
WHERE t.day_before_notified_at IS NULL
  AND t.start_at > ?
ORDER BY t.start_at ASC, t.id ASC;

-- name: ListTicketsForMeetingNotification :many
SELECT t.id, t.start_at, t.meeting_at, t.meeting_place,
       e.title AS event_title
FROM tickets t
JOIN events e ON e.id = t.event_id
WHERE t.meeting_notified_at IS NULL
  AND t.meeting_at IS NOT NULL
  AND t.start_at > ?
ORDER BY t.meeting_at ASC, t.id ASC;

-- name: ListNotifiableDiscordSubjectsByTicketIDs :many
SELECT tp.ticket_id,
       ui.subject
FROM ticket_participants tp
JOIN users u            ON u.id = tp.user_id
JOIN user_identities ui ON ui.user_id = u.id AND ui.provider = 'discord'
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
  AND u.notifications_enabled = 1
ORDER BY tp.ticket_id, tp.created_at ASC;

-- name: MarkTicketsDayBeforeNotified :exec
UPDATE tickets
SET day_before_notified_at = ?
WHERE id IN (sqlc.slice('ids'))
  AND day_before_notified_at IS NULL;

-- name: MarkTicketMeetingNotified :exec
UPDATE tickets
SET meeting_notified_at = ?
WHERE id = ?
  AND meeting_notified_at IS NULL;
