-- name: GetUserIDByIdentity :one
SELECT user_id
FROM user_identities
WHERE provider = ? AND subject = ?;

-- name: CreateUserIdentity :exec
INSERT INTO user_identities (user_id, provider, subject, created_at, updated_at)
VALUES (?, ?, ?, NOW(6), NOW(6));
