package server

// myPageService の統合テスト。now を固定した myPageService を直接組み立て、
// 実 MySQL 上で時刻境界（未来分除外・終演後除外・履歴の当日クリップ）と
// 精算操作による状態遷移を検証する。

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aruma256/nazobu/backend/internal/auth"
	nazobuv1 "github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/testdb"
)

// mustCreateTicket は admin セッションで CreateTicket を呼び、ticket ID を返す。
func mustCreateTicket(t *testing.T, db *sql.DB, adminID, eventID, startAt string, participantIDs []string) string {
	t.Helper()
	svc := newTicketService(db)
	req := connect.NewRequest(&nazobuv1.CreateTicketRequest{
		EventId:            eventID,
		StartAt:            startAt,
		PricePerPerson:     1000,
		MaxParticipants:    int32(len(participantIDs)),
		ParticipantUserIds: participantIDs,
	})
	setSessionCookie(t, db, req, adminID)
	res, err := svc.CreateTicket(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateTicket(%s) に失敗: %v", startAt, err)
	}
	return res.Msg.Ticket.Id
}

func ticketIDs(tickets []*nazobuv1.Ticket) []string {
	ids := make([]string, 0, len(tickets))
	for _, tk := range tickets {
		ids = append(ids, tk.Id)
	}
	return ids
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestIntegrationMyPageAndSettlement(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()

	// 基準時刻を 2026-07-15 12:00 JST に固定する
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, jst)
	mypage := &myPageService{db: db, q: queries.New(db), now: func() time.Time { return now }}

	adminID := createTestUser(t, db, "admin-user", auth.RoleAdmin)
	memberID := createTestUser(t, db, "member-user", auth.RoleMember)
	eventID := createTestEvent(t, db, "テスト公演")

	// 立替者はすべて admin。expected_duration_minutes は既定の 120 分。
	// - past:        7/10 開催。member が未精算 → 未精算/未回収/月次履歴に出る
	// - future:      7/20 開催。開催前なので精算系には出ず、今後の予定には出る
	// - todayEnded:  当日 9:00 開演 → 終演 11:00 は now(12:00) より前なので今後の予定から除外
	// - todayOngoing: 当日 10:30 開演 → 終演 12:30 は now より後なので今後の予定に残る
	pastID := mustCreateTicket(t, db, adminID, eventID, "2026-07-10T14:00:00+09:00", []string{adminID, memberID})
	futureID := mustCreateTicket(t, db, adminID, eventID, "2026-07-20T14:00:00+09:00", []string{adminID, memberID})
	_ = mustCreateTicket(t, db, adminID, eventID, "2026-07-15T09:00:00+09:00", []string{adminID})
	todayOngoingID := mustCreateTicket(t, db, adminID, eventID, "2026-07-15T10:30:00+09:00", []string{adminID})

	assertUnsettled := func(t *testing.T, want []string) {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMyUnsettledTicketsRequest{})
		setSessionCookie(t, db, req, memberID)
		res, err := mypage.ListMyUnsettledTickets(ctx, req)
		if err != nil {
			t.Fatalf("ListMyUnsettledTickets に失敗: %v", err)
		}
		if got := ticketIDs(res.Msg.Tickets); !equalIDs(got, want) {
			t.Errorf("未精算 = %v, want %v", got, want)
		}
	}
	assertReceivables := func(t *testing.T, want []string) {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMyUnsettledReceivablesRequest{})
		setSessionCookie(t, db, req, adminID)
		res, err := mypage.ListMyUnsettledReceivables(ctx, req)
		if err != nil {
			t.Fatalf("ListMyUnsettledReceivables に失敗: %v", err)
		}
		if got := ticketIDs(res.Msg.Tickets); !equalIDs(got, want) {
			t.Errorf("未回収 = %v, want %v", got, want)
		}
	}

	// 精算前: past だけが member の未精算・admin の未回収に出る（future は開催前なので出ない）
	assertUnsettled(t, []string{pastID})
	assertReceivables(t, []string{pastID})

	// 今後の予定（admin）: 終演済みの todayEnded は除外され、進行中 + 未来分が開演順に並ぶ
	upcomingReq := connect.NewRequest(&nazobuv1.ListMyUpcomingTicketsRequest{})
	setSessionCookie(t, db, upcomingReq, adminID)
	upcomingRes, err := mypage.ListMyUpcomingTickets(ctx, upcomingReq)
	if err != nil {
		t.Fatalf("ListMyUpcomingTickets に失敗: %v", err)
	}
	if got := ticketIDs(upcomingRes.Msg.Tickets); !equalIDs(got, []string{todayOngoingID, futureID}) {
		t.Errorf("今後の予定 = %v, want %v", got, []string{todayOngoingID, futureID})
	}

	// 月次履歴（member, 2026-07）: 当日 0:00 でクリップされるため past だけが出て、未精算
	assertMonthly := func(t *testing.T, wantSettled bool) {
		t.Helper()
		req := connect.NewRequest(&nazobuv1.ListMonthlyTicketsRequest{Year: 2026, Month: 7})
		setSessionCookie(t, db, req, memberID)
		res, err := mypage.ListMonthlyTickets(ctx, req)
		if err != nil {
			t.Fatalf("ListMonthlyTickets に失敗: %v", err)
		}
		if len(res.Msg.Monthly) != 1 || res.Msg.Monthly[0].TicketId != pastID {
			t.Fatalf("月次履歴 = %+v, want past のみ", res.Msg.Monthly)
		}
		if res.Msg.Monthly[0].Settled != wantSettled {
			t.Errorf("Settled = %v, want %v", res.Msg.Monthly[0].Settled, wantSettled)
		}
	}
	assertMonthly(t, false)

	// admin が member の精算を登録すると、未精算・未回収から消えて月次履歴が精算済みになる
	ticketSvc := newTicketService(db)
	settleReq := connect.NewRequest(&nazobuv1.UpdateTicketParticipantSettlementRequest{
		TicketId: pastID,
		UserId:   memberID,
		Settled:  true,
	})
	setSessionCookie(t, db, settleReq, adminID)
	if _, err := ticketSvc.UpdateTicketParticipantSettlement(ctx, settleReq); err != nil {
		t.Fatalf("UpdateTicketParticipantSettlement に失敗: %v", err)
	}
	assertUnsettled(t, []string{})
	assertReceivables(t, []string{})
	assertMonthly(t, true)

	// 精算解除で未精算に戻ること
	unsettleReq := connect.NewRequest(&nazobuv1.UpdateTicketParticipantSettlementRequest{
		TicketId: pastID,
		UserId:   memberID,
		Settled:  false,
	})
	setSessionCookie(t, db, unsettleReq, adminID)
	if _, err := ticketSvc.UpdateTicketParticipantSettlement(ctx, unsettleReq); err != nil {
		t.Fatalf("精算解除に失敗: %v", err)
	}
	assertUnsettled(t, []string{pastID})
	assertMonthly(t, false)
}
