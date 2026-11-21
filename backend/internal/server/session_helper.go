package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
)

func sessionTokenFromHeader(h http.Header) string {
	dummy := http.Request{Header: h}
	c, err := dummy.Cookie(auth.SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func lookupSessionUser(ctx context.Context, db *sql.DB, h http.Header) (*auth.User, error) {
	if user := auth.UserFromContext(ctx); user != nil {
		return user, nil
	}
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
