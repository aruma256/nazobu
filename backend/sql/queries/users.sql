-- name: ListUsers :many
-- 表示用の最低限フィールドだけ返す。avatar_url 等は GetMe 経路（session join）で取る。
SELECT id, username, display_name
FROM users
ORDER BY username ASC, id ASC;

-- name: CountUsersByIDs :one
-- 参照整合性のフレンドリーなプリチェック用。FK でも担保されるが UX のために事前に数を確認する。
SELECT COUNT(*) FROM users
WHERE id IN (sqlc.slice('ids'));

-- name: CreateUser :exec
INSERT INTO users (id, username, display_name, avatar_url, created_at, updated_at)
VALUES (?, ?, ?, ?, NOW(6), NOW(6));

-- name: UpdateUserProfile :exec
UPDATE users
SET username = ?, display_name = ?, avatar_url = ?, updated_at = NOW(6)
WHERE id = ?;
