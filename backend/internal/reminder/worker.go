// Package reminder は公演リマインド通知（前日 20 時 / 集合 2 時間前）を
// backend の start プロセス内で定期実行する in-process ワーカーを提供する。
//
// cron 等の外部スケジューラは使わず 1 分間隔の ticker でポーリングする。集合
// 2 時間前通知が meeting_at に依存し固定時刻 cron で表現できないため、両種別を
// 同じポーリングの仕組みに乗せている。冪等性は tickets.day_before_notified_at /
// meeting_notified_at（NULL=未送信）で担保し、再起動後のキャッチアップも自然に効く。
package reminder

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

// jst は JST 固定オフセット。tzdata 非依存にするため LoadLocation ではなく
// FixedZone を使う（日本は DST が無いので +9 固定で正しい）。
var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

const (
	// ポーリング間隔。締切判定は分解能 1 分で十分。
	tickInterval = time.Minute
	// 前日リマインドの締切時刻（JST の時）。開演日の前日 20:00 が期限。
	dayBeforeHour = 20
	// 集合リマインドの締切は集合時刻の何時間前か。
	meetingLeadTime = 2 * time.Hour
	// 猶予窓。締切がこれより古い通知は送らない（長時間停止後の過去分大量送信を防ぐ）。
	graceWindow = 2 * time.Hour
)

// Worker は定期ポーリングでリマインドを送るワーカー。
type Worker struct {
	q      *queries.Queries
	poster poster
	// frontendURL は nazobu フロントエンドのベース URL。チケット詳細リンクに使う。
	frontendURL string
	// now は現在時刻（JST）を返す。テストで固定するため関数で持つ。
	now func() time.Time
}

// NewWorker は本番用の Worker を組み立てる。webhookURL の投稿先へ client で POST する。
// frontendURL はリマインドに載せるチケット詳細リンクのベース URL。
func NewWorker(db *sql.DB, client *http.Client, webhookURL, frontendURL string) *Worker {
	return &Worker{
		q:           queries.New(db),
		poster:      &discordWebhook{url: webhookURL, client: client},
		frontendURL: frontendURL,
		now:         func() time.Time { return time.Now().In(jst) },
	}
}

// Run はワーカーのループ。ctx が done になるまで tickInterval ごとに実行する。
// 起動直後にも 1 度走らせて再起動後のキャッチアップを速める。
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	w.runOnce(ctx, w.now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx, w.now())
		}
	}
}

// runOnce は「now 時点で送るべきリマインド」を 1 巡分処理する。
// 種別ごとに独立して扱い、片方の失敗がもう片方を止めないようにする。
func (w *Worker) runOnce(ctx context.Context, now time.Time) {
	if err := w.processDayBefore(ctx, now); err != nil {
		log.Printf("リマインド: 前日通知の処理に失敗: %v", err)
	}
	if err := w.processMeeting(ctx, now); err != nil {
		log.Printf("リマインド: 集合通知の処理に失敗: %v", err)
	}
}

// processDayBefore は前日リマインドを開演日ごとにまとめて送る。
func (w *Worker) processDayBefore(ctx context.Context, now time.Time) error {
	rows, err := w.q.ListTicketsForDayBeforeNotification(ctx, now)
	if err != nil {
		return fmt.Errorf("候補の取得: %w", err)
	}
	for _, group := range groupByStartDate(rows) {
		// 同一開演日のチケットは締切（前日 20:00）も同じなので代表 1 件で判定する。
		deadline := dayBeforeDeadline(group[0].StartAt)
		if !isDue(deadline, now, graceWindow) {
			continue
		}
		w.sendDayBeforeGroup(ctx, now, group)
	}
	return nil
}

// sendDayBeforeGroup は同日 1 グループ分を 1 通にまとめて送り、含めた全チケットをマークする。
// グループ単位でエラーを閉じ込め、1 グループの失敗が他に波及しないようにする。
func (w *Worker) sendDayBeforeGroup(ctx context.Context, now time.Time, group []queries.ListTicketsForDayBeforeNotificationRow) {
	ticketIDs := make([]string, len(group))
	for i, t := range group {
		ticketIDs[i] = t.ID
	}

	subjectsByTicket, err := w.mentionsByTicket(ctx, ticketIDs)
	if err != nil {
		log.Printf("リマインド: 前日通知のメンション取得に失敗: %v", err)
		return
	}

	content, mentions := formatDayBefore(w.frontendURL, group, subjectsByTicket)
	if len(mentions) == 0 {
		// 通知対象が誰もいない。送信もマークもせず、後から参加者が増えたら拾えるよう NULL のままにする。
		return
	}

	if err := w.poster.post(ctx, content, mentions); err != nil {
		log.Printf("リマインド: 前日通知の送信に失敗: %v", err)
		return
	}

	if err := w.q.MarkTicketsDayBeforeNotified(ctx, queries.MarkTicketsDayBeforeNotifiedParams{
		DayBeforeNotifiedAt: sql.NullTime{Time: now, Valid: true},
		Ids:                 ticketIDs,
	}); err != nil {
		// 送信済みだがマークに失敗。次回 tick で重複送信されうる（at-least-once）。
		log.Printf("リマインド: 前日通知は送信したがマークに失敗（重複送信の可能性）: %v", err)
	}
}

// processMeeting は集合 2 時間前リマインドをチケット単位で送る。
func (w *Worker) processMeeting(ctx context.Context, now time.Time) error {
	rows, err := w.q.ListTicketsForMeetingNotification(ctx, now)
	if err != nil {
		return fmt.Errorf("候補の取得: %w", err)
	}
	for _, t := range rows {
		// クエリで meeting_at IS NOT NULL を保証しているため Valid 前提。
		deadline := meetingDeadline(t.MeetingAt.Time)
		if !isDue(deadline, now, graceWindow) {
			continue
		}
		w.sendMeeting(ctx, now, t)
	}
	return nil
}

// sendMeeting は集合リマインド 1 件を送り、そのチケットをマークする。
func (w *Worker) sendMeeting(ctx context.Context, now time.Time, t queries.ListTicketsForMeetingNotificationRow) {
	subjectsByTicket, err := w.mentionsByTicket(ctx, []string{t.ID})
	if err != nil {
		log.Printf("リマインド: 集合通知のメンション取得に失敗: %v", err)
		return
	}
	mentions := dedupePreserveOrder(subjectsByTicket[t.ID])
	if len(mentions) == 0 {
		// 通知対象が誰もいない。送信もマークもしない。
		return
	}

	if err := w.poster.post(ctx, formatMeeting(w.frontendURL, t, mentions), mentions); err != nil {
		log.Printf("リマインド: 集合通知の送信に失敗: %v", err)
		return
	}

	if err := w.q.MarkTicketMeetingNotified(ctx, queries.MarkTicketMeetingNotifiedParams{
		MeetingNotifiedAt: sql.NullTime{Time: now, Valid: true},
		ID:                t.ID,
	}); err != nil {
		log.Printf("リマインド: 集合通知は送信したがマークに失敗（重複送信の可能性）: %v", err)
	}
}

// mentionsByTicket は ticket_id → メンション対象 subject の map を引く。
func (w *Worker) mentionsByTicket(ctx context.Context, ticketIDs []string) (map[string][]string, error) {
	rows, err := w.q.ListNotifiableDiscordSubjectsByTicketIDs(ctx, ticketIDs)
	if err != nil {
		return nil, err
	}
	m := make(map[string][]string, len(ticketIDs))
	for _, r := range rows {
		m[r.TicketID] = append(m[r.TicketID], r.Subject)
	}
	return m, nil
}

// dayBeforeDeadline は開演日時から前日 20:00(JST) の締切を返す。
func dayBeforeDeadline(startAt time.Time) time.Time {
	t := startAt.In(jst)
	// 日に -1 して time.Date に正規化させる（月初なら前月末へ繰り下がる）。
	return time.Date(t.Year(), t.Month(), t.Day()-1, dayBeforeHour, 0, 0, 0, jst)
}

// meetingDeadline は集合時刻から meetingLeadTime（2h）前の締切を返す。
func meetingDeadline(meetingAt time.Time) time.Time {
	return meetingAt.In(jst).Add(-meetingLeadTime)
}

// isDue は締切を過ぎ、かつ猶予窓内（締切が古すぎない）なら true を返す。
func isDue(deadline, now time.Time, grace time.Duration) bool {
	if now.Before(deadline) {
		return false // まだ締切前
	}
	if deadline.Before(now.Add(-grace)) {
		return false // 締切が猶予窓より古い（長時間停止後の取りこぼし）
	}
	return true
}

// groupByStartDate は候補チケットを開演日（JST）ごとにまとめる。
// 入力は start_at 昇順前提で、グループの出現順を保つ。
func groupByStartDate(rows []queries.ListTicketsForDayBeforeNotificationRow) [][]queries.ListTicketsForDayBeforeNotificationRow {
	order := make([]string, 0)
	byDate := make(map[string][]queries.ListTicketsForDayBeforeNotificationRow)
	for _, r := range rows {
		key := r.StartAt.In(jst).Format("2006-01-02")
		if _, ok := byDate[key]; !ok {
			order = append(order, key)
		}
		byDate[key] = append(byDate[key], r)
	}
	groups := make([][]queries.ListTicketsForDayBeforeNotificationRow, 0, len(order))
	for _, k := range order {
		groups = append(groups, byDate[k])
	}
	return groups
}
