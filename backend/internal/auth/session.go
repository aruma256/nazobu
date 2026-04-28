package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	SessionCookieName = "nazobu_session"
	SessionTTL        = 30 * 24 * time.Hour
)

var ErrNoSession = errors.New("session が見つからないか期限切れ")

type User struct {
	ID      string
	Discord DiscordIdentity
}

type DiscordIdentity struct {
	DiscordUserID string
	Username      string
	DisplayName   sql.NullString
	Avatar        sql.NullString
}

// UpsertUserWithDiscord は Discord identity を内部 user に upsert する。
// 既存の Discord identity があればそれを更新、無ければ users と discord_identities を
// 同一トランザクションで作る。
func UpsertUserWithDiscord(ctx context.Context, db *sql.DB, du *DiscordUser) (*User, error) {
	displayName := nullString(du.DisplayName)
	avatar := nullString(du.Avatar)

	var existingUserID string
	err := db.QueryRowContext(ctx, `
		SELECT user_id FROM discord_identities WHERE discord_user_id = ?
	`, du.ID).Scan(&existingUserID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		newUserID := ulid.Make().String()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, created_at, updated_at) VALUES (?, NOW(6), NOW(6))
		`, newUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO discord_identities
			  (user_id, discord_user_id, username, display_name, avatar, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, NOW(6), NOW(6))
		`, newUserID, du.ID, du.Username, displayName, avatar); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &User{
			ID: newUserID,
			Discord: DiscordIdentity{
				DiscordUserID: du.ID,
				Username:      du.Username,
				DisplayName:   displayName,
				Avatar:        avatar,
			},
		}, nil

	case err != nil:
		return nil, err

	default:
		if _, err := db.ExecContext(ctx, `
			UPDATE discord_identities
			SET username = ?, display_name = ?, avatar = ?, updated_at = NOW(6)
			WHERE user_id = ?
		`, du.Username, displayName, avatar, existingUserID); err != nil {
			return nil, err
		}
		return &User{
			ID: existingUserID,
			Discord: DiscordIdentity{
				DiscordUserID: du.ID,
				Username:      du.Username,
				DisplayName:   displayName,
				Avatar:        avatar,
			},
		}, nil
	}
}

// CreateSession は新しい session を発行する。
// 返り値の rawToken はクライアントに cookie で渡す値。DB には sha256 hash しか保存しない。
func CreateSession(ctx context.Context, db *sql.DB, userID string) (string, error) {
	rawToken, err := generateToken()
	if err != nil {
		return "", err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, NOW(6))
	`, ulid.Make().String(), userID, hashToken(rawToken), time.Now().Add(SessionTTL))
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

// LookupSession は raw token から有効な session に紐づく user を引く。
// 期限切れなら ErrNoSession を返し、当該 session を削除する。
// 現状 user は必ず Discord identity を持つ（UpsertUserWithDiscord で users と
// discord_identities を同一トランザクションで作っている）ので INNER JOIN で良い。
// 将来別 IdP を追加するときに JOIN 構造を見直す。
func LookupSession(ctx context.Context, db *sql.DB, rawToken string) (*User, error) {
	var u User
	var expiresAt time.Time
	err := db.QueryRowContext(ctx, `
		SELECT u.id,
		       di.discord_user_id, di.username, di.display_name, di.avatar,
		       s.expires_at
		FROM sessions s
		INNER JOIN users u              ON u.id = s.user_id
		INNER JOIN discord_identities di ON di.user_id = u.id
		WHERE s.token_hash = ?
	`, hashToken(rawToken)).Scan(
		&u.ID,
		&u.Discord.DiscordUserID, &u.Discord.Username, &u.Discord.DisplayName, &u.Discord.Avatar,
		&expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(expiresAt) {
		_, _ = db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(rawToken))
		return nil, ErrNoSession
	}
	return &u, nil
}

// DeleteSession は raw token に対応する session を削除する。
func DeleteSession(ctx context.Context, db *sql.DB, rawToken string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(rawToken))
	return err
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
