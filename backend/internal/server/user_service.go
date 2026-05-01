package server

import (
	"context"
	"database/sql"
	"fmt"

	"connectrpc.com/connect"

	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

type userService struct {
	db *sql.DB
	q  *queries.Queries
}

func newUserService(db *sql.DB) nazobuv1connect.UserServiceHandler {
	return &userService{db: db, q: queries.New(db)}
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

func (s *userService) ListUsers(ctx context.Context, req *connect.Request[nazobuv1.ListUsersRequest]) (*connect.Response[nazobuv1.ListUsersResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("user 一覧の取得に失敗: %w", err))
	}

	users := make([]*nazobuv1.User, 0, len(rows))
	for _, r := range rows {
		users = append(users, &nazobuv1.User{
			Id:          r.ID,
			Username:    r.Username,
			DisplayName: r.DisplayName.String,
		})
	}
	return connect.NewResponse(&nazobuv1.ListUsersResponse{Users: users}), nil
}
