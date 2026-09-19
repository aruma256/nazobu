package server

// ticketService の統合テスト。実 MySQL（testdb パッケージ経由）に対して
// セッション認証込みで RPC ハンドラを呼び、DB 往復を含む挙動を検証する。
// TEST_DB_HOST 未設定の環境では skip される。

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

type fakeDiscordSpoilerChannelManager struct {
	grantErr      error
	configured    bool
	createdID     string
	createNames   []string
	createTopics  []string
	createMembers [][]string
	grantChannels []string
	grantMembers  [][]string
	deletedIDs    []string
}

func (f *fakeDiscordSpoilerChannelManager) Configured() bool { return f.configured }
func (f *fakeDiscordSpoilerChannelManager) ChannelURL(channelID string) string {
	return "https://discord.example/channels/" + channelID
}
func (f *fakeDiscordSpoilerChannelManager) CreateSpoilerChannel(_ context.Context, name, topic string, memberIDs []string) (string, error) {
	f.createNames = append(f.createNames, name)
	f.createTopics = append(f.createTopics, topic)
	f.createMembers = append(f.createMembers, slices.Clone(memberIDs))
	return f.createdID, nil
}
func (f *fakeDiscordSpoilerChannelManager) GrantMembersView(_ context.Context, channelID string, memberIDs []string) error {
	f.grantChannels = append(f.grantChannels, channelID)
	f.grantMembers = append(f.grantMembers, slices.Clone(memberIDs))
	return f.grantErr
}
func (f *fakeDiscordSpoilerChannelManager) DeleteChannel(_ context.Context, channelID string) error {
	f.deletedIDs = append(f.deletedIDs, channelID)
	return nil
}

// createTestUser は user を作成して ID を返す。
func createTestUser(t *testing.T, db *sql.DB, displayName, role string) string {
	t.Helper()
	ctx := context.Background()
	q := queries.New(db)
	userID := id.New()
	if err := q.CreateUser(ctx, queries.CreateUserParams{
		ID:          userID,
		DisplayName: displayName,
	}); err != nil {
		t.Fatalf("user 作成に失敗: %v", err)
	}
	if role != auth.RoleMember {
		if err := q.UpdateUserRole(ctx, queries.UpdateUserRoleParams{Role: role, ID: userID}); err != nil {
			t.Fatalf("role 更新に失敗: %v", err)
		}
	}
	return userID
}

func createTestDiscordIdentity(t *testing.T, db *sql.DB, userID, subject string) {
	t.Helper()
	if err := queries.New(db).CreateUserIdentity(context.Background(), queries.CreateUserIdentityParams{
		UserID: userID, Provider: auth.ProviderDiscord, Subject: subject,
	}); err != nil {
		t.Fatalf("Discord identity 作成に失敗: %v", err)
	}
}

// createTestEvent は event を作成して ID を返す。
func createTestEvent(t *testing.T, db *sql.DB, title string) string {
	t.Helper()
	eventID := id.New()
	if err := queries.New(db).CreateEvent(context.Background(), queries.CreateEventParams{
		ID:                      eventID,
		Title:                   title,
		Url:                     "https://example.com/event",
		ExpectedDurationMinutes: 120,
	}); err != nil {
		t.Fatalf("event 作成に失敗: %v", err)
	}
	return eventID
}

// setSessionCookie は userID の session を DB に作り、その cookie を req に載せる。
func setSessionCookie[T any](t *testing.T, db *sql.DB, req *connect.Request[T], userID string) {
	t.Helper()
	token, err := auth.CreateSession(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("session 作成に失敗: %v", err)
	}
	req.Header().Set("Cookie", auth.SessionCookieName+"="+token)
}

func connectCode(t *testing.T, err error) connect.Code {
	t.Helper()
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("connect.Error を期待したが %v (%T)", err, err)
	}
	return cerr.Code()
}

func TestIntegrationCreateAndGetTicket(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// admin がグループチケットを登録する
	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		MeetingAt:          "2026-08-01T13:30:00+09:00",
		PricePerPerson:     3500,
		MaxParticipants:    2,
		MeetingPlace:       "会場前",
		ParticipantUserIds: []string{adminID, memberID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id
	if ticketID == "" {
		t.Fatal("作成された ticket の ID が空")
	}

	// 登録した内容が DB 経由でそのまま取れること（JST の日時表現含む）
	getReq := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
	setSessionCookie(t, db, getReq, memberID)
	getRes, err := svc.GetTicket(ctx, getReq)
	if err != nil {
		t.Fatalf("GetTicket に失敗: %v", err)
	}
	ticket := getRes.Msg.Ticket
	if ticket.EventId != eventID {
		t.Errorf("EventId = %q, want %q", ticket.EventId, eventID)
	}
	if ticket.EventTitle != "テスト公演" {
		t.Errorf("EventTitle = %q, want %q", ticket.EventTitle, "テスト公演")
	}
	if ticket.StartAt != "2026-08-01T14:00:00+09:00" {
		t.Errorf("StartAt = %q, DB 往復で JST 表現が保たれていない", ticket.StartAt)
	}
	if ticket.PricePerPerson != 3500 {
		t.Errorf("PricePerPerson = %d, want 3500", ticket.PricePerPerson)
	}

	// 参加者 2 名が入っていて、立替者（admin）に印がつくこと
	participants := getRes.Msg.Participants
	if len(participants) != 2 {
		t.Fatalf("参加者数 = %d, want 2", len(participants))
	}
	names := []string{participants[0].Name, participants[1].Name}
	for _, want := range []string{"admin-user", "member-user"} {
		if !slices.Contains(names, want) {
			t.Errorf("参加者に %q がいない: %v", want, names)
		}
	}
	for _, p := range participants {
		if got, want := p.IsPurchaser, p.UserId == adminID; got != want {
			t.Errorf("IsPurchaser(%s) = %v, want %v", p.Name, got, want)
		}
	}

	// member（非立替者）には編集権限がないこと
	if getRes.Msg.CanEdit {
		t.Error("非 admin・非立替者の CanEdit が true になっている")
	}
}

func TestIntegrationCreateTicketAuthorization(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	newCreateReq := func() *connect.Request[nazobuv1.CreateTicketRequest] {
		return connect.NewRequest(&nazobuv1.CreateTicketRequest{
			EventId:            eventID,
			StartAt:            "2026-08-01T14:00:00+09:00",
			PricePerPerson:     1000,
			MaxParticipants:    1,
			ParticipantUserIds: []string{memberID},
		})
	}

	t.Run("session cookie なしは Unauthenticated", func(t *testing.T) {
		_, err := svc.CreateTicket(ctx, newCreateReq())
		if got := connectCode(t, err); got != connect.CodeUnauthenticated {
			t.Errorf("code = %v, want %v", got, connect.CodeUnauthenticated)
		}
	})

	t.Run("member の登録は PermissionDenied", func(t *testing.T) {
		req := newCreateReq()
		setSessionCookie(t, db, req, memberID)
		_, err := svc.CreateTicket(ctx, req)
		if got := connectCode(t, err); got != connect.CodePermissionDenied {
			t.Errorf("code = %v, want %v", got, connect.CodePermissionDenied)
		}
	})
}

func TestIntegrationUpdateTicketMaxParticipantsLowerBound(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		PricePerPerson:     3000,
		MaxParticipants:    2,
		ParticipantUserIds: []string{adminID, memberID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id

	// 参加者が 2 名いる状態で max_participants を 1 に下げるのは
	// 現在の参加者数を DB で数えた上で弾かれること
	updateReq := connect.NewRequest(&nazobuv1.UpdateTicketRequest{
		TicketId:          ticketID,
		StartAt:           "2026-08-01T14:00:00+09:00",
		PricePerPerson:    3000,
		MaxParticipants:   1,
		PurchasedByUserId: adminID,
	})
	setSessionCookie(t, db, updateReq, adminID)
	_, err = svc.UpdateTicket(ctx, updateReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("code = %v, want %v", got, connect.CodeFailedPrecondition)
	}
}

func TestIntegrationUnregisteredParticipants(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// 未登録の同行者 2 名込みで登録でき、人数が DB 往復で保たれること
	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:                       eventID,
		StartAt:                       "2026-08-01T14:00:00+09:00",
		PricePerPerson:                3000,
		MaxParticipants:               4,
		ParticipantUserIds:            []string{adminID, memberID},
		UnregisteredParticipantsCount: 2,
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id
	if got := createRes.Msg.Ticket.UnregisteredParticipantsCount; got != 2 {
		t.Errorf("作成直後の UnregisteredParticipantsCount = %d, want 2", got)
	}

	getReq := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
	setSessionCookie(t, db, getReq, memberID)
	getRes, err := svc.GetTicket(ctx, getReq)
	if err != nil {
		t.Fatalf("GetTicket に失敗: %v", err)
	}
	if got := getRes.Msg.Ticket.UnregisteredParticipantsCount; got != 2 {
		t.Errorf("UnregisteredParticipantsCount = %d, want 2", got)
	}

	// 登録ユーザー 2 名 + 未登録 2 名で満席（max 4）なので、参加者追加は弾かれること
	otherID := createTestUser(t, db, "other-user", auth.RoleMember)
	addReq := connect.NewRequest(&nazobuv1.AddTicketParticipantsRequest{
		TicketId: ticketID,
		UserIds:  []string{otherID},
	})
	setSessionCookie(t, db, addReq, adminID)
	_, err = svc.AddTicketParticipants(ctx, addReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("満席時の追加 code = %v, want %v", got, connect.CodeFailedPrecondition)
	}

	// max_participants を参加者数 + 未登録人数より小さくする更新は弾かれること
	updateReq := connect.NewRequest(&nazobuv1.UpdateTicketRequest{
		TicketId:                      ticketID,
		StartAt:                       "2026-08-01T14:00:00+09:00",
		PricePerPerson:                3000,
		MaxParticipants:               3,
		PurchasedByUserId:             adminID,
		UnregisteredParticipantsCount: 2,
	})
	setSessionCookie(t, db, updateReq, adminID)
	_, err = svc.UpdateTicket(ctx, updateReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("max 減少 code = %v, want %v", got, connect.CodeFailedPrecondition)
	}

	// 未登録人数を減らす更新は通り、値が保存されること
	updateReq = connect.NewRequest(&nazobuv1.UpdateTicketRequest{
		TicketId:                      ticketID,
		StartAt:                       "2026-08-01T14:00:00+09:00",
		PricePerPerson:                3000,
		MaxParticipants:               4,
		PurchasedByUserId:             adminID,
		UnregisteredParticipantsCount: 1,
	})
	setSessionCookie(t, db, updateReq, adminID)
	updateRes, err := svc.UpdateTicket(ctx, updateReq)
	if err != nil {
		t.Fatalf("UpdateTicket に失敗: %v", err)
	}
	if got := updateRes.Msg.Ticket.UnregisteredParticipantsCount; got != 1 {
		t.Errorf("更新後の UnregisteredParticipantsCount = %d, want 1", got)
	}

	// 未登録 1 名に減ったので空き 1 枠に追加できること
	addReq = connect.NewRequest(&nazobuv1.AddTicketParticipantsRequest{
		TicketId: ticketID,
		UserIds:  []string{otherID},
	})
	setSessionCookie(t, db, addReq, adminID)
	if _, err := svc.AddTicketParticipants(ctx, addReq); err != nil {
		t.Fatalf("空きありでの追加に失敗: %v", err)
	}
}

func TestIntegrationCreateTicketUnregisteredValidation(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	eventID := createTestEvent(t, db, "テスト公演")

	newCreateReq := func(maxParticipants, unregistered int32) *connect.Request[nazobuv1.CreateTicketRequest] {
		req := connect.NewRequest(&nazobuv1.CreateTicketRequest{
			EventId:                       eventID,
			StartAt:                       "2026-08-01T14:00:00+09:00",
			PricePerPerson:                1000,
			MaxParticipants:               maxParticipants,
			ParticipantUserIds:            []string{adminID},
			UnregisteredParticipantsCount: unregistered,
		})
		setSessionCookie(t, db, req, adminID)
		return req
	}

	t.Run("負の未登録人数は InvalidArgument", func(t *testing.T) {
		_, err := svc.CreateTicket(ctx, newCreateReq(2, -1))
		if got := connectCode(t, err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want %v", got, connect.CodeInvalidArgument)
		}
	})

	t.Run("登録ユーザー + 未登録人数が定員超過なら InvalidArgument", func(t *testing.T) {
		_, err := svc.CreateTicket(ctx, newCreateReq(2, 2))
		if got := connectCode(t, err); got != connect.CodeInvalidArgument {
			t.Errorf("code = %v, want %v", got, connect.CodeInvalidArgument)
		}
	})

	t.Run("定員ちょうどなら登録できる", func(t *testing.T) {
		if _, err := svc.CreateTicket(ctx, newCreateReq(2, 1)); err != nil {
			t.Errorf("CreateTicket に失敗: %v", err)
		}
	})
}

func TestIntegrationTicketParticipantManagement(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := newTicketService(db)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	otherID := createTestUser(t, db, "other-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// max 2 のチケットを admin だけで作る（空きは 1）
	createReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            "2026-08-01T14:00:00+09:00",
		PricePerPerson:     2000,
		MaxParticipants:    2,
		ParticipantUserIds: []string{adminID},
	})
	setSessionCookie(t, db, createReq, adminID)
	createRes, err := svc.CreateTicket(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateTicket に失敗: %v", err)
	}
	ticketID := createRes.Msg.Ticket.Id

	addParticipants := func(t *testing.T, userIDs ...string) error {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.AddTicketParticipantsRequest{
			TicketId: ticketID,
			UserIds:  userIDs,
		})
		setSessionCookie(t, db, req, adminID)
		_, err := svc.AddTicketParticipants(ctx, req)
		return err
	}
	participantCount := func(t *testing.T) int {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: ticketID})
		setSessionCookie(t, db, req, adminID)
		res, err := svc.GetTicket(ctx, req)
		if err != nil {
			t.Fatalf("GetTicket に失敗: %v", err)
		}
		return len(res.Msg.Participants)
	}

	// 空きがあるうちは追加できる
	if err := addParticipants(t, memberID); err != nil {
		t.Fatalf("参加者追加に失敗: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Fatalf("参加者数 = %d, want 2", got)
	}

	// 満席での追加は DB の現在数を数えた上で弾かれる
	if err := addParticipants(t, otherID); connectCode(t, err) != connect.CodeFailedPrecondition {
		t.Errorf("満席時の追加 code = %v, want %v", connectCode(t, err), connect.CodeFailedPrecondition)
	}

	// 参加済みユーザの再追加は冪等（満席でもエラーにならず、数も増えない）
	if err := addParticipants(t, memberID); err != nil {
		t.Fatalf("参加済みユーザの再追加がエラー: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Errorf("再追加後の参加者数 = %d, want 2", got)
	}

	// 立替者本人は削除できない
	removeReq := connect.NewRequest(&nazobuv1.RemoveTicketParticipantRequest{
		TicketId: ticketID,
		UserId:   adminID,
	})
	setSessionCookie(t, db, removeReq, adminID)
	_, err = svc.RemoveTicketParticipant(ctx, removeReq)
	if got := connectCode(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("立替者削除 code = %v, want %v", got, connect.CodeFailedPrecondition)
	}

	// 立替者以外は削除でき、空いた枠に改めて追加できる
	removeReq = connect.NewRequest(&nazobuv1.RemoveTicketParticipantRequest{
		TicketId: ticketID,
		UserId:   memberID,
	})
	setSessionCookie(t, db, removeReq, adminID)
	if _, err := svc.RemoveTicketParticipant(ctx, removeReq); err != nil {
		t.Fatalf("参加者削除に失敗: %v", err)
	}
	if err := addParticipants(t, otherID); err != nil {
		t.Fatalf("削除後の追加に失敗: %v", err)
	}
	if got := participantCount(t); got != 2 {
		t.Errorf("入れ替え後の参加者数 = %d, want 2", got)
	}
}

func TestIntegrationGrantTicketSpoilerChannelAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	manager := &fakeDiscordSpoilerChannelManager{configured: true, createdID: "discord-channel-1"}
	svc := newTicketServiceWithDiscord(db, manager)

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	member1ID := createTestUser(t, db, "member-1", auth.RoleMember)
	member2ID := createTestUser(t, db, "member-2", auth.RoleMember)
	createTestDiscordIdentity(t, db, adminID, "discord-admin")
	createTestDiscordIdentity(t, db, member1ID, "discord-member-1")
	createTestDiscordIdentity(t, db, member2ID, "discord-member-2")
	eventID := createTestEvent(t, db, "テスト / 公演")

	createTicket := func(startAt string, participantIDs ...string) string {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.CreateTicketRequest{
			EventId: eventID, StartAt: startAt, PricePerPerson: 3000,
			MaxParticipants: int32(len(participantIDs)), ParticipantUserIds: participantIDs,
		})
		setSessionCookie(t, db, req, adminID)
		res, err := svc.CreateTicket(ctx, req)
		if err != nil {
			t.Fatalf("CreateTicket に失敗: %v", err)
		}
		return res.Msg.Ticket.Id
	}
	grant := func(ticketID, actorID string) (*nazobuv1.GrantTicketSpoilerChannelAccessResponse, error) {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.GrantTicketSpoilerChannelAccessRequest{TicketId: ticketID})
		setSessionCookie(t, db, req, actorID)
		res, err := svc.GrantTicketSpoilerChannelAccess(ctx, req)
		if err != nil {
			return nil, err
		}
		return res.Msg, nil
	}

	firstTicketID := createTicket("2026-08-30T14:00:00+09:00", adminID, member1ID)
	created, err := grant(firstTicketID, adminID)
	if err != nil {
		t.Fatalf("チャンネル作成と権限付与に失敗: %v", err)
	}
	if !created.ChannelCreated || created.DiscordChannelUrl != "https://discord.example/channels/discord-channel-1" {
		t.Errorf("create response = %+v", created)
	}
	if len(manager.createNames) != 1 || manager.createNames[0] != "20260830-テスト-公演" {
		t.Errorf("create names = %v", manager.createNames)
	}
	if !slices.Equal(manager.createMembers[0], []string{"discord-admin", "discord-member-1"}) {
		t.Errorf("create members = %v", manager.createMembers[0])
	}
	if len(manager.grantMembers) != 0 {
		t.Errorf("新規作成時に追加の grant が呼ばれた: %v", manager.grantMembers)
	}

	getReq := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: firstTicketID})
	setSessionCookie(t, db, getReq, adminID)
	getRes, err := svc.GetTicket(ctx, getReq)
	if err != nil {
		t.Fatalf("GetTicket に失敗: %v", err)
	}
	if getRes.Msg.DiscordSpoilerChannelUrl != "https://discord.example/channels/discord-channel-1" {
		t.Errorf("DiscordSpoilerChannelUrl = %q", getRes.Msg.DiscordSpoilerChannelUrl)
	}
	memberGetReq := connect.NewRequest(&nazobuv1.GetTicketRequest{TicketId: firstTicketID})
	setSessionCookie(t, db, memberGetReq, member1ID)
	memberGetRes, err := svc.GetTicket(ctx, memberGetReq)
	if err != nil {
		t.Fatalf("member の GetTicket に失敗: %v", err)
	}
	if memberGetRes.Msg.DiscordSpoilerChannelUrl != "" {
		t.Errorf("member に DiscordSpoilerChannelUrl が公開された: %q", memberGetRes.Msg.DiscordSpoilerChannelUrl)
	}

	// 同じ event の別日 ticket は既存 channel へその ticket の参加者だけ追加する。
	secondTicketID := createTicket("2026-09-15T19:00:00+09:00", member2ID)
	synced, err := grant(secondTicketID, adminID)
	if err != nil {
		t.Fatalf("別日 ticket の権限付与に失敗: %v", err)
	}
	if synced.ChannelCreated {
		t.Error("既存 channel に対し ChannelCreated = true")
	}
	if len(manager.createNames) != 1 {
		t.Errorf("チャンネルが二重作成された: %v", manager.createNames)
	}
	if len(manager.grantChannels) != 1 || manager.grantChannels[0] != "discord-channel-1" ||
		!slices.Equal(manager.grantMembers[0], []string{"discord-member-2"}) {
		t.Errorf("grant = channels:%v members:%v", manager.grantChannels, manager.grantMembers)
	}

	// admin 以外は実行できない。
	if _, err := grant(secondTicketID, member2ID); connectCode(t, err) != connect.CodePermissionDenied {
		t.Errorf("member の実行 code = %v, want %v", connectCode(t, err), connect.CodePermissionDenied)
	}

	// 機能境界より前の ticket は対象外。
	legacyEventID := createTestEvent(t, db, "旧公演")
	legacyReq := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId: legacyEventID, StartAt: "2026-08-29T23:59:59+09:00", PricePerPerson: 1000,
		MaxParticipants: 1, ParticipantUserIds: []string{adminID},
	})
	setSessionCookie(t, db, legacyReq, adminID)
	legacyRes, err := svc.CreateTicket(ctx, legacyReq)
	if err != nil {
		t.Fatalf("旧 ticket の作成に失敗: %v", err)
	}
	if _, err := grant(legacyRes.Msg.Ticket.Id, adminID); connectCode(t, err) != connect.CodeFailedPrecondition {
		t.Errorf("旧 ticket の grant code = %v, want %v", connectCode(t, err), connect.CodeFailedPrecondition)
	}
}

// createTestTicket は event に紐づく ticket を作成して ID を返す。
// 参加者は付けず本体だけを作る。
func createTestTicket(t *testing.T, db *sql.DB, eventID, purchasedBy string) string {
	t.Helper()
	ticketID := id.New()
	if err := queries.New(db).CreateTicket(context.Background(), queries.CreateTicketParams{
		ID:              ticketID,
		EventID:         eventID,
		StartAt:         time.Date(2026, 8, 1, 14, 0, 0, 0, jst),
		PricePerPerson:  3000,
		MaxParticipants: 4,
		PurchasedBy:     purchasedBy,
		MeetingPlace:    "",
	}); err != nil {
		t.Fatalf("ticket 作成に失敗: %v", err)
	}
	return ticketID
}
