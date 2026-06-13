-- リマインド通知ワーカー用のクエリ。
-- 締切（前日 20 時 / 集合 2 時間前）と猶予窓の判定は Go 側で行う。日付や時刻の
-- 演算は JST 基準で組み立てたいが SQL では tz が絡んで間違えやすいため、ここでは
-- 「まだ通知しておらず、開演が未来」のチケットを候補として広めに引くに留める。
-- 引数 ? には Go 側で算出した「現在時刻（JST）」を渡す（driver の loc=Asia/Tokyo に
-- より JST naive な DATETIME 列と素直に比較できる）。

-- name: ListTicketsForDayBeforeNotification :many
-- 前日リマインドの候補。未送信（day_before_notified_at IS NULL）かつ
-- 開演が未来（start_at > ?）のチケットを start_at 昇順で返す。
-- 同日まとめ・締切（前日 20 時）・猶予窓の判定は呼び出し側。
SELECT t.id, t.start_at, t.meeting_at, t.meeting_place,
       e.title AS event_title
FROM tickets t
JOIN events e ON e.id = t.event_id
WHERE t.day_before_notified_at IS NULL
  AND t.start_at > ?
ORDER BY t.start_at ASC, t.id ASC;

-- name: ListTicketsForMeetingNotification :many
-- 集合 2 時間前リマインドの候補。未送信かつ集合時刻あり、開演が未来のチケット。
-- 締切（meeting_at - 2h）・猶予窓の判定は呼び出し側。
SELECT t.id, t.start_at, t.meeting_at, t.meeting_place,
       e.title AS event_title
FROM tickets t
JOIN events e ON e.id = t.event_id
WHERE t.meeting_notified_at IS NULL
  AND t.meeting_at IS NOT NULL
  AND t.start_at > ?
ORDER BY t.meeting_at ASC, t.id ASC;

-- name: ListNotifiableDiscordSubjectsByTicketIDs :many
-- 指定チケット群の参加者のうち、通知が有効（notifications_enabled = 1）で
-- Discord 連携済みのユーザーの subject（Discord user id）を返す。メンション用。
-- 呼び出し側で ticket_id ごとに振り分ける。
SELECT tp.ticket_id,
       ui.subject
FROM ticket_participants tp
JOIN users u            ON u.id = tp.user_id
JOIN user_identities ui ON ui.user_id = u.id AND ui.provider = 'discord'
WHERE tp.ticket_id IN (sqlc.slice('ticket_ids'))
  AND u.notifications_enabled = 1
ORDER BY tp.ticket_id, tp.created_at ASC;

-- name: MarkTicketsDayBeforeNotified :exec
-- 前日リマインド送信済みマーク。同日まとめ送信した全チケットに送信時刻（JST）を立てる。
-- 多重送信防止のため未送信のものだけを対象にする。
UPDATE tickets
SET day_before_notified_at = ?
WHERE id IN (sqlc.slice('ids'))
  AND day_before_notified_at IS NULL;

-- name: MarkTicketMeetingNotified :exec
-- 集合 2 時間前リマインド送信済みマーク。未送信のものだけを対象にする。
UPDATE tickets
SET meeting_notified_at = ?
WHERE id = ?
  AND meeting_notified_at IS NULL;
