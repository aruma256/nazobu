-- name: ListUsers :many
SELECT id, display_name
FROM users
ORDER BY display_name ASC, id ASC;

-- name: CountUsersByIDs :one
SELECT COUNT(*) FROM users
WHERE id IN (sqlc.slice('ids'));

-- name: CreateUser :exec
INSERT INTO users (id, display_name, avatar_url, created_at, updated_at)
VALUES (?, ?, ?, NOW(6), NOW(6));

-- name: UpdateUserProfile :exec
UPDATE users
SET display_name = ?, avatar_url = ?, updated_at = NOW(6)
WHERE id = ?;

-- name: UpdateUserRole :exec
UPDATE users
SET role = ?, updated_at = NOW(6)
WHERE id = ?;
