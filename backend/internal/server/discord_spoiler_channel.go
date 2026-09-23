package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/logging"
)

const discordChannelNameMaxRunes = 100

var discordSpoilerChannelFeatureStartAt = time.Date(2026, time.August, 30, 0, 0, 0, 0, jst)

// discordSpoilerChannelManager は Discord API との境界。テストでは fake に差し替える。
type discordSpoilerChannelManager interface {
	Configured() bool
	ChannelURL(channelID string) string
	CreateSpoilerChannel(ctx context.Context, name, topic string, memberIDs []string) (string, error)
	GrantMembersView(ctx context.Context, channelID string, memberIDs []string) error
	DeleteChannel(ctx context.Context, channelID string) error
}

// GrantTicketSpoilerChannelAccess は event のネタバレチャンネルを必要に応じて作成し、
// 指定 ticket の現在の参加者に VIEW_CHANNEL を追加付与する。参加者から外れた人の
// 権限はこの処理では削除しない。
func (s *ticketService) GrantTicketSpoilerChannelAccess(
	ctx context.Context,
	req *connect.Request[nazobuv1.GrantTicketSpoilerChannelAccessRequest],
) (response *connect.Response[nazobuv1.GrantTicketSpoilerChannelAccessResponse], returnErr error) {
	defer logRPCFailure(ctx, "GrantTicketSpoilerChannelAccess", &returnErr)
	user, err := lookupSessionUser(ctx, s.db, req.Header())
	if err != nil {
		return nil, err
	}
	if user.Role != auth.RoleAdmin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("Discord ネタバレチャンネルの操作は admin のみ"))
	}
	if s.spoilerChannelManager == nil || !s.spoilerChannelManager.Configured() {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord ネタバレチャンネル設定が未完了"))
	}

	ticketID := strings.TrimSpace(req.Msg.GetTicketId())
	if ticketID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("ticket_id は必須"))
	}

	// 現在の構成は backend 1 インスタンス。同一 event の二重作成を避けるため、
	// Discord 作成から DB への id 保存までをプロセス内で直列化する。
	s.spoilerChannelMutation.Lock()
	defer s.spoilerChannelMutation.Unlock()

	ticket, err := s.q.GetTicketByID(ctx, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("指定された ticket は存在しない"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket の取得に失敗: %w", err))
	}
	if ticket.StartAt.Before(discordSpoilerChannelFeatureStartAt) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord ネタバレチャンネル連携は 2026-08-30 以降の ticket が対象"))
	}

	identities, err := s.q.ListTicketParticipantDiscordIdentities(ctx, ticketID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ticket 参加者の Discord identity 取得に失敗: %w", err))
	}
	memberIDs := make([]string, 0, len(identities))
	missingNames := make([]string, 0)
	for _, identity := range identities {
		if !identity.DiscordSubject.Valid || strings.TrimSpace(identity.DiscordSubject.String) == "" {
			missingNames = append(missingNames, identity.DisplayName)
			continue
		}
		memberIDs = append(memberIDs, identity.DiscordSubject.String)
	}
	if len(missingNames) > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("Discord identity が無い参加者: %s", strings.Join(missingNames, ", ")))
	}
	if len(memberIDs) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Discord 権限を付与できる参加者がいない"))
	}

	channelID := ""
	created := false
	if ticket.DiscordSpoilerChannelID.Valid {
		channelID = strings.TrimSpace(ticket.DiscordSpoilerChannelID.String)
	}
	if channelID == "" {
		channelName := discordSpoilerChannelName(ticket.StartAt, ticket.EventTitle, ticket.EventID)
		topic := "nazobu event_id=" + ticket.EventID
		channelID, err = s.spoilerChannelManager.CreateSpoilerChannel(ctx, channelName, topic, memberIDs)
		if err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("Discord ネタバレチャンネルの作成に失敗: %w", err))
		}
		updated, updateErr := s.q.SetEventDiscordSpoilerChannelID(ctx, queries.SetEventDiscordSpoilerChannelIDParams{
			DiscordSpoilerChannelID: sql.NullString{String: channelID, Valid: true},
			ID:                      ticket.EventID,
		})
		if updateErr != nil || updated != 1 {
			// DB から参照できない orphan channel を残さないよう補償する。
			deleteErr := s.spoilerChannelManager.DeleteChannel(ctx, channelID)
			if deleteErr != nil {
				slog.ErrorContext(ctx, "discord channel compensation failed", logging.ID("event_id", ticket.EventID))
			}
			if updateErr != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("Discord channel id の保存に失敗: %w（補償削除: %v）", updateErr, deleteErr))
			}
			return nil, connect.NewError(connect.CodeAborted, fmt.Errorf("Discord channel id は他の処理で保存済み（補償削除: %v）", deleteErr))
		}
		created = true
	} else if err := s.spoilerChannelManager.GrantMembersView(ctx, channelID, memberIDs); err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("Discord ネタバレチャンネルの権限付与に失敗: %w", err))
	}

	slog.InfoContext(ctx, "ticket spoiler channel access granted", logging.ID("ticket_id", ticketID), logging.ID("event_id", ticket.EventID), logging.ID("actor_user_id", user.ID), "channel_created", created)
	return connect.NewResponse(&nazobuv1.GrantTicketSpoilerChannelAccessResponse{
		DiscordChannelUrl: s.spoilerChannelManager.ChannelURL(channelID),
		ChannelCreated:    created,
	}), nil
}

func (s *ticketService) discordSpoilerChannelURL(channelID sql.NullString) string {
	if !channelID.Valid || s.spoilerChannelManager == nil || !s.spoilerChannelManager.Configured() {
		return ""
	}
	return s.spoilerChannelManager.ChannelURL(channelID.String)
}

// discordSpoilerChannelName は Discord の 1〜100 文字制限に合わせて
// "YYYYMMDD-公演名" を組み立てる。日本語や emoji は保ち、チャンネル名で
// 問題になりやすい区切り文字と制御文字だけを '-' に畳み込む。
func discordSpoilerChannelName(startAt time.Time, eventTitle, eventID string) string {
	prefix := startAt.In(jst).Format("20060102") + "-"
	titleRunes := make([]rune, 0, len([]rune(eventTitle)))
	lastWasHyphen := false
	for _, r := range strings.TrimSpace(eventTitle) {
		replaceWithHyphen := unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune("/\\#:", r)
		if replaceWithHyphen || r == '-' {
			if len(titleRunes) > 0 && !lastWasHyphen {
				titleRunes = append(titleRunes, '-')
				lastWasHyphen = true
			}
			continue
		}
		titleRunes = append(titleRunes, r)
		lastWasHyphen = false
	}
	title := strings.Trim(string(titleRunes), "-.")
	if title == "" {
		compactID := strings.ReplaceAll(eventID, "-", "")
		if len(compactID) > 8 {
			compactID = compactID[len(compactID)-8:]
		}
		if compactID == "" {
			compactID = "event"
		}
		title = "nazobu-" + compactID
	}

	maxTitleRunes := discordChannelNameMaxRunes - len([]rune(prefix))
	runes := []rune(title)
	if len(runes) > maxTitleRunes {
		runes = runes[:maxTitleRunes]
	}
	title = strings.TrimRight(string(runes), "-.")
	if title == "" {
		title = "event"
	}
	return prefix + title
}
