package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

// LinkEventSpoilerChannel は既存チャンネルの紐づけだけを行う。Discord 権限は変更しない。
func (s *eventService) LinkEventSpoilerChannel(ctx context.Context, req *connect.Request[nazobuv1.LinkEventSpoilerChannelRequest]) (*connect.Response[nazobuv1.LinkEventSpoilerChannelResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("Discord ネタバレチャンネルの操作は admin のみ"))
	}
	eventID := strings.TrimSpace(req.Msg.GetEventId())
	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	channelID, guildID, err := parseDiscordChannel(req.Msg.GetDiscordChannel())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	manager := s.spoilerChannelManager
	if manager == nil || !manager.Configured() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord ネタバレチャンネル設定が未完了"))
	}
	if guildID != "" && manager.ChannelURL(channelID) != "https://discord.com/channels/"+guildID+"/"+channelID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("設定済み Discord サーバーのチャンネルを指定してください"))
	}
	event, err := s.q.GetEventByID(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された event は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if event.DiscordSpoilerChannelID.Valid && event.DiscordSpoilerChannelID.String != channelID {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("この公演には別の Discord チャンネルが紐づけ済みです"))
	}
	if err := manager.ValidateSpoilerChannel(ctx, channelID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("Discord チャンネルを確認できません: %w", err))
	}
	if !event.DiscordSpoilerChannelID.Valid {
		// 自動作成と競合しても既存の紐づけを上書きしない。
		updated, err := s.q.SetEventDiscordSpoilerChannelID(ctx, queries.SetEventDiscordSpoilerChannelIDParams{
			ID: eventID, DiscordSpoilerChannelID: sql.NullString{String: channelID, Valid: true},
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if updated != 1 {
			return nil, connect.NewError(connect.CodeAborted, errors.New("公演の紐づけが変更されました。再読み込みしてください"))
		}
	}
	return connect.NewResponse(&nazobuv1.LinkEventSpoilerChannelResponse{DiscordChannelUrl: manager.ChannelURL(channelID)}), nil
}

func parseDiscordChannel(raw string) (channelID, guildID string, err error) {
	raw = strings.TrimSpace(raw)
	if validDiscordSnowflake(raw) {
		return raw, "", nil
	}
	u, parseErr := url.Parse(raw)
	if parseErr == nil && u.Scheme == "https" && u.Host == "discord.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.RawPath == "" {
		parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
		if len(parts) == 4 && parts[1] == "channels" && validDiscordSnowflake(parts[2]) && validDiscordSnowflake(parts[3]) {
			return parts[3], parts[2], nil
		}
	}
	return "", "", errors.New("Discord チャンネル ID または https://discord.com/channels/サーバーID/チャンネルID を指定してください")
}

func validDiscordSnowflake(s string) bool {
	if s == "" || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// JoinEventSpoilerChannel は参加記録に関係なく本人だけに閲覧権限を付与する。
func (s *eventService) JoinEventSpoilerChannel(ctx context.Context, req *connect.Request[nazobuv1.JoinEventSpoilerChannelRequest]) (*connect.Response[nazobuv1.JoinEventSpoilerChannelResponse], error) {
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	eventID := strings.TrimSpace(req.Msg.GetEventId())
	if eventID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_id は必須"))
	}
	event, err := s.q.GetEventByID(ctx, eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された公演は存在しません"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !event.DiscordSpoilerChannelID.Valid || event.DiscordSpoilerChannelID.String == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("この公演にはネタバレチャンネルが紐づいていません"))
	}
	manager := s.spoilerChannelManager
	if manager == nil || !manager.Configured() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord ネタバレチャンネル設定が未完了"))
	}
	subject, err := s.q.GetDiscordSubjectByUserID(ctx, user.ID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && strings.TrimSpace(subject) == "") {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord アカウントが紐づいていません"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := manager.GrantMembersView(ctx, event.DiscordSpoilerChannelID.String, []string{subject}); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("Discord 閲覧権限の付与に失敗: %w", err))
	}
	return connect.NewResponse(&nazobuv1.JoinEventSpoilerChannelResponse{DiscordChannelUrl: manager.ChannelURL(event.DiscordSpoilerChannelID.String)}), nil
}
