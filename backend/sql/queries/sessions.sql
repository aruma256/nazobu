-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
VALUES (?, ?, ?, ?, NOW(6));

-- name: GetSessionUserByTokenHash :one
-- 期限切れ判定は呼び出し側で行う。
SELECT u.id, u.display_name, u.avatar_url, u.role, s.expires_at
FROM sessions s
INNER JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ?;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = ?;
