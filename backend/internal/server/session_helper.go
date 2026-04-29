package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

// sessionTokenFromHeader は Connect の Request header に乗ってきた cookie から
// session token を取り出す。Connect-Go は http.Header をそのまま見せてくれるので、
// http.Request 風に Cookie ヘッダをパースしている。
func sessionTokenFromHeader(h http.Header) string {
	dummy := http.Request{Header: h}
	c, err := dummy.Cookie(auth.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// lookupSessionUser は Request header の cookie から user を引く。
// 未ログイン / 期限切れは connect.CodeUnauthenticated を返す。
func lookupSessionUser(ctx context.Context, db *sql.DB, h http.Header) (*auth.User, error) {
	rawToken := sessionTokenFromHeader(h)
	if rawToken == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session cookie が無い"))
	}
	user, err := auth.LookupSession(ctx, db, rawToken)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return user, nil
}
