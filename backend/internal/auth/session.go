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

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

const (
	SessionCookieName = "nazobu_session"
	SessionTTL        = 30 * 24 * time.Hour

	// users.role に保存する値。schema.sql の CHECK 制約と同期させる。
	RoleAdmin  = "admin"
	RoleMember = "member"
)

var ErrNoSession = errors.New("session が見つからないか期限切れ")

// ErrUserNotRegistered は LoginWithIdentity で identity が事前登録されていない
// ことを示す。
var ErrUserNotRegistered = errors.New("ユーザーが事前登録されていない")

// ErrUserAlreadyRegistered は RegisterUser で identity が登録済みであることを示す。
var ErrUserAlreadyRegistered = errors.New("ユーザーが登録済み")

type User struct {
	ID          string
	DisplayName string
	AvatarURL   sql.NullString
	Role        string
}

// UserProfile は IdP から取得した、user 表示用プロフィールのスナップショット。
// ログインのたびに users テーブルへキャッシュ更新される。
// DisplayName は users.display_name の NOT NULL 制約を満たすよう、IdP ごとの
// フォールバックを解決してから渡す。
type UserProfile struct {
	DisplayName string
	AvatarURL   string
}

// RegisterUser は (provider, subject) で識別される IdP identity のユーザーを事前登録する。
// users と user_identities を同一トランザクションで作る。
// 登録済みの identity に対しては既存ユーザーを更新せず ErrUserAlreadyRegistered を返す。
func RegisterUser(ctx context.Context, db *sql.DB, provider, subject string, profile UserProfile) (*User, error) {
	q := queries.New(db)
	_, err := q.GetUserIDByIdentity(ctx, queries.GetUserIDByIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if err == nil {
		return nil, ErrUserAlreadyRegistered
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	avatarURL := nullString(profile.AvatarURL)
	newUserID := id.New()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	qtx := q.WithTx(tx)
	if err := qtx.CreateUser(ctx, queries.CreateUserParams{
		ID:          newUserID,
		DisplayName: profile.DisplayName,
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
		DisplayName: profile.DisplayName,
		AvatarURL:   avatarURL,
		Role:        RoleMember,
	}, nil
}

// LoginWithIdentity は (provider, subject) で識別される IdP identity に紐づく
// 既存ユーザーを取得しプロフィールを最新化する。事前登録（RegisterUser）が
// 行われていない identity に対しては ErrUserNotRegistered を返し、新規ユーザーを
// 作成しない。サービスを招待制に保つためのログインゲート。
func LoginWithIdentity(ctx context.Context, db *sql.DB, provider, subject string, profile UserProfile) (*User, error) {
	avatarURL := nullString(profile.AvatarURL)

	q := queries.New(db)
	existingUserID, err := q.GetUserIDByIdentity(ctx, queries.GetUserIDByIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotRegistered
	}
	if err != nil {
		return nil, err
	}
	if err := q.UpdateUserProfile(ctx, queries.UpdateUserProfileParams{
		DisplayName: profile.DisplayName,
		AvatarUrl:   avatarURL,
		ID:          existingUserID,
	}); err != nil {
		return nil, err
	}
	return &User{
		ID:          existingUserID,
		DisplayName: profile.DisplayName,
		AvatarURL:   avatarURL,
	}, nil
}

// CreateSession は新しい session を発行する。
// 返り値の rawToken はクライアントに cookie で渡す値。DB には sha256 hash しか保存しない。
func CreateSession(ctx context.Context, db *sql.DB, userID string) (string, error) {
	rawToken, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := queries.New(db).CreateSession(ctx, queries.CreateSessionParams{
		ID:        id.New(),
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
		DisplayName: row.DisplayName,
		AvatarURL:   row.AvatarUrl,
		Role:        row.Role,
	}, nil
}

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
