-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, NOW(6));

-- name: GetSessionUserByTokenHash :one
-- session 引きで紐づく user と expires_at を一発で取得する。
-- 期限切れ判定は呼び出し側で行う。
SELECT u.id, u.username, u.display_name, u.avatar_url, u.role, s.expires_at
FROM sessions s
INNER JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = ?;
