package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

func createSessionTestUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	userID := id.New()
	if err := queries.New(db).CreateUser(context.Background(), queries.CreateUserParams{
		ID:          userID,
		DisplayName: "session-test-user",
	}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	return userID
}

func TestIntegrationSessionLifecycle(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	userID := createSessionTestUser(t, db)

	rawToken, err := CreateSession(ctx, db, userID)
	if err != nil {
		t.Fatalf("CreateSession に失敗: %v", err)
	}

	user, err := LookupSession(ctx, db, rawToken)
	if err != nil {
		t.Fatalf("LookupSession に失敗: %v", err)
	}
	if user.ID != userID {
		t.Errorf("user.ID = %q, want %q", user.ID, userID)
	}
	if user.Role != RoleMember {
		t.Errorf("user.Role = %q, want %q", user.Role, RoleMember)
	}

	if _, err := queries.New(db).GetSessionUserByTokenHash(ctx, rawToken); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("raw token を token_hash として引けてしまう（hash 保存になっていない）: err = %v", err)
	}

	if err := DeleteSession(ctx, db, rawToken); err != nil {
		t.Fatalf("DeleteSession に失敗: %v", err)
	}
	if _, err := LookupSession(ctx, db, rawToken); !errors.Is(err, ErrNoSession) {
		t.Errorf("削除済み session の err = %v, want ErrNoSession", err)
	}
}

func TestIntegrationLookupSessionExpired(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	userID := createSessionTestUser(t, db)

	rawToken, err := generateToken()
	if err != nil {
		t.Fatalf("token 生成に失敗: %v", err)
	}
	if err := queries.New(db).CreateSession(ctx, queries.CreateSessionParams{
		ID:        id.New(),
		UserID:    userID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("session 作成に失敗: %v", err)
	}

	if _, err := LookupSession(ctx, db, rawToken); !errors.Is(err, ErrNoSession) {
		t.Fatalf("期限切れ session の err = %v, want ErrNoSession", err)
	}

	if _, err := queries.New(db).GetSessionUserByTokenHash(ctx, hashToken(rawToken)); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("期限切れ session が削除されていない: err = %v", err)
	}
}
