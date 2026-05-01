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

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

const (
	SessionCookieName = "nazobu_session"
	SessionTTL        = 30 * 24 * time.Hour

	// users.role に保存する値。schema.sql の CHECK 制約と同期させる。
	RoleAdmin  = "admin"
	RoleMember = "member"
)

var ErrNoSession = errors.New("session が見つからないか期限切れ")

type User struct {
	ID          string
	Username    string
	DisplayName sql.NullString
	AvatarURL   sql.NullString
	Role        string
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

	q := queries.New(db)
	existingUserID, err := q.GetUserIDByIdentity(ctx, queries.GetUserIDByIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		newUserID := ulid.Make().String()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()

		qtx := q.WithTx(tx)
		if err := qtx.CreateUser(ctx, queries.CreateUserParams{
			ID:          newUserID,
			Username:    profile.Username,
			DisplayName: displayName,
			AvatarUrl:   avatarURL,
		}); err != nil {
			return nil, err
		}
		if err := qtx.CreateUserIdentity(ctx, queries.CreateUserIdentityParams{
			UserID:   newUserID,
			Provider: provider,
			Subject:  subject,
		}); err != nil {
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
			Role:        RoleMember,
		}, nil

	case err != nil:
		return nil, err

	default:
		if err := q.UpdateUserProfile(ctx, queries.UpdateUserProfileParams{
			Username:    profile.Username,
			DisplayName: displayName,
			AvatarUrl:   avatarURL,
			ID:          existingUserID,
		}); err != nil {
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
	if err := queries.New(db).CreateSession(ctx, queries.CreateSessionParams{
		ID:        ulid.Make().String(),
		UserID:    userID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: time.Now().Add(SessionTTL),
	}); err != nil {
		return "", err
	}
	return rawToken, nil
}

// LookupSession は raw token から有効な session に紐づく user を引く。
// 期限切れなら ErrNoSession を返し、当該 session を削除する。
func LookupSession(ctx context.Context, db *sql.DB, rawToken string) (*User, error) {
	tokenHash := hashToken(rawToken)
	q := queries.New(db)
	row, err := q.GetSessionUserByTokenHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(row.ExpiresAt) {
		_ = q.DeleteSessionByTokenHash(ctx, tokenHash)
		return nil, ErrNoSession
	}
	return &User{
		ID:          row.ID,
		Username:    row.Username,
		DisplayName: row.DisplayName,
		AvatarURL:   row.AvatarUrl,
		Role:        row.Role,
	}, nil
}

// DeleteSession は raw token に対応する session を削除する。
func DeleteSession(ctx context.Context, db *sql.DB, rawToken string) error {
	return queries.New(db).DeleteSessionByTokenHash(ctx, hashToken(rawToken))
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
