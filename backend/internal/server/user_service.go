package server

import (
	"context"
	"database/sql"
	"fmt"

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

func (s *userService) ListUsers(ctx context.Context, req *connect.Request[nazobuv1.ListUsersRequest]) (*connect.Response[nazobuv1.ListUsersResponse], error) {
	if _, err := lookupSessionUser(ctx, s.db, req.Header()); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, display_name
		FROM users
		ORDER BY username ASC, id ASC
	`)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("user 一覧の取得に失敗: %w", err))
	}
	defer rows.Close()

	users := []*nazobuv1.User{}
	for rows.Next() {
		var (
			id, username string
			displayName  sql.NullString
		)
		if err := rows.Scan(&id, &username, &displayName); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("user 行の読み取りに失敗: %w", err))
		}
		users = append(users, &nazobuv1.User{
			Id:          id,
			Username:    username,
			DisplayName: displayName.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("user 一覧の取得に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.ListUsersResponse{Users: users}), nil
}
