package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

type userService struct {
	db *sql.DB
}

func newUserService(db *sql.DB) nazobuv1connect.UserServiceHandler {
	return &userService{db: db}
}

func (s *userService) GetMe(ctx context.Context, req *connect.Request[nazobuv1.GetMeRequest]) (*connect.Response[nazobuv1.GetMeResponse], error) {
	rawToken := sessionTokenFromHeader(req.Header())
	if rawToken == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session cookie が無い"))
	}
	user, err := auth.LookupSession(ctx, s.db, rawToken)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&nazobuv1.GetMeResponse{
		Id:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName.String,
		AvatarUrl:   user.AvatarURL.String,
	}), nil
}

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
