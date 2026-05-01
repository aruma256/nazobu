-- name: ListUnsettledTicketsByUserID :many
-- 自分が参加したチケットのうち「立替者が自分以外」かつ「未精算」を取る。
-- 立替者本人の自己持ち分は精算対象ではないので除外する。
SELECT t.id, e.title AS event_title,
       t.price_per_person, t.attended_on,
       COALESCE(NULLIF(pu.display_name, ''), pu.username) AS payee_name
FROM ticket_participants tp
JOIN tickets t  ON t.id  = tp.ticket_id
JOIN events  e  ON e.id  = t.event_id
JOIN users   pu ON pu.id = t.purchased_by
WHERE tp.user_id    = sqlc.arg('user_id')
  AND tp.settled_at IS NULL
  AND t.purchased_by <> tp.user_id
ORDER BY t.attended_on ASC, t.id ASC;

-- name: ListUpcomingTicketsByUserID :many
-- 今日以降に attended_on を持つ自分の参加チケット。
SELECT t.id, e.title AS event_title, e.url AS event_url, t.attended_on
FROM ticket_participants tp
JOIN tickets t ON t.id = tp.ticket_id
JOIN events  e ON e.id = t.event_id
WHERE tp.user_id     = sqlc.arg('user_id')
  AND t.attended_on >= sqlc.arg('today')
ORDER BY t.attended_on ASC, t.id ASC;

-- name: ListMyMonthlyTicketsByUserID :many
-- 当月分（[month_start, next_month_start) 半開区間）の自分の参加チケット。
-- 立替者本人 or 精算済みなら settled = 1 とみなす。
-- MySQL に BOOLEAN 型がないため CAST で UNSIGNED に固定し、sqlc に NullBool 推論させずに int64 で受ける。
SELECT t.id, e.title AS event_title, t.attended_on,
       CAST((tp.settled_at IS NOT NULL OR t.purchased_by = tp.user_id) AS UNSIGNED) AS settled
FROM ticket_participants tp
JOIN tickets t ON t.id = tp.ticket_id
JOIN events  e ON e.id = t.event_id
WHERE tp.user_id     = sqlc.arg('user_id')
  AND t.attended_on >= sqlc.arg('month_start')
  AND t.attended_on <  sqlc.arg('next_month_start')
ORDER BY t.attended_on DESC, t.id ASC;

-- name: ListCompanionNamesByTicketIDs :many
-- 自分以外の参加者（同行者）名を ticket_id ごとにまとめて引く（N+1 回避）。
SELECT tp.ticket_id,
       COALESCE(NULLIF(u.display_name, ''), u.username) AS name
FROM ticket_participants tp
JOIN users u ON u.id = tp.user_id
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
  AND tp.user_id   <> sqlc.arg('exclude_user_id')
ORDER BY tp.ticket_id, tp.created_at ASC;
