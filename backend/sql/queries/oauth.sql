-- name: CreateOAuthAuthorizationCode :exec
INSERT INTO oauth_authorization_codes
  (id, code_hash, user_id, client_id, redirect_uri, scope, code_challenge, expires_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(6));

-- name: GetOAuthAuthorizationCodeByHash :one
SELECT id, user_id, client_id, redirect_uri, scope, code_challenge, expires_at
FROM oauth_authorization_codes
WHERE code_hash = ?;

-- name: DeleteOAuthAuthorizationCode :execrows
DELETE FROM oauth_authorization_codes WHERE id = ?;

-- name: CreateOAuthToken :exec
INSERT INTO oauth_tokens
  (id, user_id, client_id, scope,
   access_token_hash, access_token_expires_at,
   refresh_token_hash, refresh_token_expires_at,
   created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(6), NOW(6));

-- name: GetOAuthTokenUserByAccessTokenHash :one
-- Bearer トークン検証用。紐づく user も一発で引く。期限判定は呼び出し側で行う。
SELECT u.id, u.display_name, u.avatar_url, u.role,
       t.id AS token_id, t.scope, t.access_token_expires_at
FROM oauth_tokens t
INNER JOIN users u ON u.id = t.user_id
WHERE t.access_token_hash = ?;

-- name: GetOAuthTokenByRefreshTokenHash :one
SELECT id, user_id, client_id, scope, refresh_token_expires_at
FROM oauth_tokens
WHERE refresh_token_hash = ?;

-- name: RotateOAuthToken :exec
-- refresh grant 時にアクセストークンとリフレッシュトークンを同時にローテーションする。
UPDATE oauth_tokens
SET access_token_hash = ?,
    access_token_expires_at = ?,
    refresh_token_hash = ?,
    refresh_token_expires_at = ?,
    updated_at = NOW(6)
WHERE id = ?;

-- name: DeleteOAuthTokenByID :exec
DELETE FROM oauth_tokens WHERE id = ?;

-- name: DeleteExpiredOAuthRecords :exec
-- 期限切れの認可コードを掃除する。
DELETE FROM oauth_authorization_codes WHERE expires_at < ?;

-- name: DeleteExpiredOAuthTokens :exec
-- refresh 期限も切れたトークンを掃除する。
DELETE FROM oauth_tokens WHERE refresh_token_expires_at < ?;
