-- name: ListEvents :many
-- 公演一覧（新しい順）。詳細表示用の最低限フィールドのみ返す。
SELECT id, title, url, catchphrase, image_url, doors_open_minutes_before, entry_deadline_minutes_before, expected_duration_minutes
FROM events
ORDER BY created_at DESC, id DESC;

-- name: GetEventByID :one
-- 1 件の event を取得する。詳細・編集画面用。
SELECT id, title, url, catchphrase, image_url, doors_open_minutes_before, entry_deadline_minutes_before, expected_duration_minutes
FROM events
WHERE id = ?;

-- name: CreateEvent :exec
INSERT INTO events (id, title, url, catchphrase, image_url, doors_open_minutes_before, entry_deadline_minutes_before, expected_duration_minutes, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: UpdateEvent :exec
UPDATE events
SET title = ?,
    url = ?,
    catchphrase = ?,
    image_url = ?,
    doors_open_minutes_before = ?,
    entry_deadline_minutes_before = ?,
    expected_duration_minutes = ?,
    updated_at = NOW(6)
WHERE id = ?;

-- name: CountEventByID :one
-- 参照整合性のフレンドリーなプリチェック用。FK でも担保されるが UX のために事前に存在確認する。
SELECT COUNT(*) FROM events WHERE id = ?;

-- name: ListEventTicketsByEventIDs :many
-- 公演一覧画面で各 event に紐づく ticket をまとめて引く。
-- 呼び出し側で event_id ごとに in-memory で振り分ける（N+1 回避）。
SELECT t.id, t.event_id, t.start_at, t.price_per_person,
       t.unregistered_participants_count,
       pu.display_name AS purchaser_name
FROM tickets t
JOIN users pu ON pu.id = t.purchased_by
WHERE t.event_id IN (sqlc.slice('event_ids'))
ORDER BY t.start_at DESC, t.id ASC;
