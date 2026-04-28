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
	ID          string
	Username    string
	DisplayName sql.NullString
	AvatarURL   sql.NullString
}

// UserProfile は IdP から取得した、user 表示用プロフィールのスナップショット。
// ログインのたびに users テーブルへキャッシュ更新される。
type UserProfile struct {
	Username    string
	DisplayName string
	AvatarURL   string
}

// UpsertUserFromIdentity は (provider, subject) で識別される IdP identity を内部 user に
// upsert する。既存 identity があれば紐づく user のプロフィールを更新、無ければ users と
// user_identities を同一トランザクションで作る。
func UpsertUserFromIdentity(ctx context.Context, db *sql.DB, provider, subject string, profile UserProfile) (*User, error) {
	displayName := nullString(profile.DisplayName)
	avatarURL := nullString(profile.AvatarURL)

	var existingUserID string
	err := db.QueryRowContext(ctx, `
		SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?
	`, provider, subject).Scan(&existingUserID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		newUserID := ulid.Make().String()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, username, display_name, avatar_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, NOW(6), NOW(6))
		`, newUserID, profile.Username, displayName, avatarURL); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_identities (user_id, provider, subject, created_at, updated_at)
			VALUES (?, ?, ?, NOW(6), NOW(6))
		`, newUserID, provider, subject); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &User{
			ID:          newUserID,
			Username:    profile.Username,
			DisplayName: displayName,
			AvatarURL:   avatarURL,
		}, nil

	case err != nil:
		return nil, err

	default:
		if _, err := db.ExecContext(ctx, `
			UPDATE users
			SET username = ?, display_name = ?, avatar_url = ?, updated_at = NOW(6)
			WHERE id = ?
		`, profile.Username, displayName, avatarURL, existingUserID); err != nil {
			return nil, err
		}
		return &User{
			ID:          existingUserID,
			Username:    profile.Username,
			DisplayName: displayName,
			AvatarURL:   avatarURL,
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
func LookupSession(ctx context.Context, db *sql.DB, rawToken string) (*User, error) {
	var u User
	var expiresAt time.Time
	err := db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_url, s.expires_at
		FROM sessions s
		INNER JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?
	`, hashToken(rawToken)).Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL,
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
