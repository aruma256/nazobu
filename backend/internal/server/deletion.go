package server

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/go-sql-driver/mysql"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
)

func (s *eventService) DeleteEvent(ctx context.Context, req *connect.Request[nazobuv1.DeleteEventRequest]) (*connect.Response[nazobuv1.DeleteEventResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("公演の削除は管理者のみ可能です"))
	}
	eventID := strings.TrimSpace(req.Msg.GetEventId())
	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	// FK の制約で、同時にチケットが追加された場合も連鎖削除せず拒否する。
	count, err := s.q.DeleteEvent(ctx, eventID)
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1451 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("チケットが残っている公演は削除できません。先にチケットを削除してください"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if count == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された公演は存在しません"))
	}
	return connect.NewResponse(&nazobuv1.DeleteEventResponse{}), nil
}

func (s *ticketService) DeleteTicket(ctx context.Context, req *connect.Request[nazobuv1.DeleteTicketRequest]) (*connect.Response[nazobuv1.DeleteTicketResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)
	purchasedBy, err := qtx.LockTicketForDeletion(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定されたチケットは存在しません"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !canEditTicket(user, purchasedBy) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("チケットの削除は管理者もしくは立替者のみ可能です"))
	}
	if err := qtx.DeleteTicketParticipants(ctx, ticketID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := qtx.DeleteTicket(ctx, ticketID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&nazobuv1.DeleteTicketResponse{}), nil
}
