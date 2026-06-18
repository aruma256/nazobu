-- name: ListUnsettledTicketsByUserID :many
-- 自分が参加したチケットのうち「立替者が自分以外」かつ「未精算」かつ「開演が現在以前」を取る。
-- 立替者本人の自己持ち分は精算対象ではないので除外する。
-- 未来分は精算対象として扱わない（公演前に表示しない）。
-- 列は ListTickets と同じ。マイページでも /tickets と同じ TicketCard で表示するため。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.meeting_place,
       pu.display_name AS purchaser_name
FROM ticket_participants tp
JOIN tickets t  ON t.id  = tp.ticket_id
JOIN events  e  ON e.id  = t.event_id
JOIN users   pu ON pu.id = t.purchased_by
WHERE tp.user_id    = sqlc.arg('user_id')
  AND tp.settled_at IS NULL
  AND t.purchased_by <> tp.user_id
  AND t.start_at <= sqlc.arg('now')
ORDER BY t.start_at ASC, t.id ASC;

-- name: ListUnsettledReceivablesByUserID :many
-- 自分が立て替えたチケットのうち「自分以外の参加者に未精算が 1 人以上残っている」かつ「開演が現在以前」を取る。
-- 受け取り側の貰い忘れ防止用。自分自身の参加分（自己持ち）は精算対象でないため EXISTS の条件から除外する。
-- 未来分は精算対象として扱わない（公演前に表示しない）。
-- 列は ListTickets と同じ。マイページでも /tickets と同じ TicketCard で表示するため。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.meeting_place,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN events e  ON e.id = t.event_id
JOIN users  pu ON pu.id = t.purchased_by
WHERE t.purchased_by = sqlc.arg('user_id')
  AND t.start_at <= sqlc.arg('now')
  AND EXISTS (
    SELECT 1 FROM ticket_participants tp
    WHERE tp.ticket_id = t.id
      AND tp.user_id <> t.purchased_by
      AND tp.settled_at IS NULL
  )
ORDER BY t.start_at ASC, t.id ASC;

-- name: ListUpcomingTicketsByUserID :many
-- 当日 0:00（JST）以降に start_at を持つ自分の参加チケット。
-- 当日中は時刻が過ぎていても表示し続ける（今日の予定として残す）。
-- 列は ListTickets と同じ。マイページでも /tickets と同じ TicketCard で表示するため。
SELECT t.id, t.event_id, e.title AS event_title, e.url AS event_url, e.catchphrase AS event_catchphrase, e.image_url AS event_image_url,
       e.expected_duration_minutes AS event_expected_duration_minutes,
       e.doors_open_minutes_before AS event_doors_open_minutes_before,
       t.start_at, t.meeting_at, t.price_per_person, t.max_participants,
       t.meeting_place,
       pu.display_name AS purchaser_name
FROM ticket_participants tp
JOIN tickets t ON t.id = tp.ticket_id
JOIN events  e ON e.id = t.event_id
JOIN users   pu ON pu.id = t.purchased_by
WHERE tp.user_id  = sqlc.arg('user_id')
  AND t.start_at >= sqlc.arg('today_start')
ORDER BY t.start_at ASC, t.id ASC;

-- name: ListMyMonthlyTicketsByUserID :many
-- 当月分（[month_start, next_month_start) 半開区間）の自分の参加チケット。
-- 立替者本人 or 精算済みなら settled = 1 とみなす。
-- MySQL に BOOLEAN 型がないため CAST で UNSIGNED に固定し、sqlc に NullBool 推論させずに int64 で受ける。
SELECT t.id, e.title AS event_title, t.start_at,
       CAST((tp.settled_at IS NOT NULL OR t.purchased_by = tp.user_id) AS UNSIGNED) AS settled
FROM ticket_participants tp
JOIN tickets t ON t.id = tp.ticket_id
JOIN events  e ON e.id = t.event_id
WHERE tp.user_id  = sqlc.arg('user_id')
  AND t.start_at >= sqlc.arg('month_start')
  AND t.start_at <  sqlc.arg('next_month_start')
ORDER BY t.start_at ASC, t.id ASC;
