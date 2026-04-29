package server

import (
	"context"
	"database/sql"

	"connectrpc.com/connect"

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
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&nazobuv1.GetMeResponse{
		Id:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName.String,
		AvatarUrl:   user.AvatarURL.String,
	}), nil
}
