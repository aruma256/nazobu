package reminder

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/aruma256/nazobu/backend/internal/discord"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/logging"
)

var jst = time.FixedZone("Asia/Tokyo", 9*60*60)

const (
	tickInterval    = time.Minute
	dayBeforeHour   = 20
	meetingLeadTime = 2 * time.Hour
	graceWindow     = 2 * time.Hour
)

type Worker struct {
	q           *queries.Queries
	poster      poster
	frontendURL string
	now         func() time.Time
}

func NewWorker(db *sql.DB, client *discord.Client, channelID, frontendURL string) *Worker {
	return &Worker{
		q:           queries.New(db),
		poster:      &discordBotPoster{client: client, channelID: channelID},
		frontendURL: frontendURL,
		now:         func() time.Time { return time.Now().In(jst) },
	}
}

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

func (w *Worker) runOnce(ctx context.Context, now time.Time) {
	if err := w.processDayBefore(ctx, now); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "day_before", "stage", "list")
	}
	if err := w.processMeeting(ctx, now); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "meeting", "stage", "list")
	}
}

func (w *Worker) processDayBefore(ctx context.Context, now time.Time) error {
	rows, err := w.q.ListTicketsForDayBeforeNotification(ctx, now)
	if err != nil {
		return fmt.Errorf("候補の取得: %w", err)
	}
	for _, group := range groupByStartDate(rows) {
		deadline := dayBeforeDeadline(group[0].StartAt)
		if !isDue(deadline, now, graceWindow) {
			continue
		}
		w.sendDayBeforeGroup(ctx, now, group)
	}
	return nil
}

func (w *Worker) sendDayBeforeGroup(ctx context.Context, now time.Time, group []queries.ListTicketsForDayBeforeNotificationRow) {
	ticketIDs := make([]string, len(group))
	for i, t := range group {
		ticketIDs[i] = t.ID
	}

	subjectsByTicket, err := w.mentionsByTicket(ctx, ticketIDs)
	if err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "day_before", "stage", "mentions", "ticket_count", len(ticketIDs))
		return
	}

	content, mentions := formatDayBefore(w.frontendURL, group, subjectsByTicket)
	if len(mentions) == 0 {
		return
	}

	if err := w.poster.post(ctx, content, mentions); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "day_before", "stage", "send", "ticket_count", len(ticketIDs))
		return
	}

	for _, ticketID := range ticketIDs {
		slog.InfoContext(ctx, "reminder sent", "kind", "day_before", logging.ID("ticket_id", ticketID))
	}

	if err := w.q.MarkTicketsDayBeforeNotified(ctx, queries.MarkTicketsDayBeforeNotifiedParams{
		DayBeforeNotifiedAt: sql.NullTime{Time: now, Valid: true},
		Ids:                 ticketIDs,
	}); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "day_before", "stage", "mark", "ticket_count", len(ticketIDs), "sent", true)
	}
}

func (w *Worker) processMeeting(ctx context.Context, now time.Time) error {
	rows, err := w.q.ListTicketsForMeetingNotification(ctx, now)
	if err != nil {
		return fmt.Errorf("候補の取得: %w", err)
	}
	for _, t := range rows {
		deadline := meetingDeadline(t.MeetingAt.Time)
		if !isDue(deadline, now, graceWindow) {
			continue
		}
		w.sendMeeting(ctx, now, t)
	}
	return nil
}

func (w *Worker) sendMeeting(ctx context.Context, now time.Time, t queries.ListTicketsForMeetingNotificationRow) {
	subjectsByTicket, err := w.mentionsByTicket(ctx, []string{t.ID})
	if err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "meeting", "stage", "mentions", logging.ID("ticket_id", t.ID))
		return
	}
	mentions := dedupePreserveOrder(subjectsByTicket[t.ID])
	if len(mentions) == 0 {
		return
	}

	if err := w.poster.post(ctx, formatMeeting(w.frontendURL, t, mentions), mentions); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "meeting", "stage", "send", logging.ID("ticket_id", t.ID))
		return
	}

	slog.InfoContext(ctx, "reminder sent", "kind", "meeting", logging.ID("ticket_id", t.ID))

	if err := w.q.MarkTicketMeetingNotified(ctx, queries.MarkTicketMeetingNotifiedParams{
		MeetingNotifiedAt: sql.NullTime{Time: now, Valid: true},
		ID:                t.ID,
	}); err != nil {
		slog.ErrorContext(ctx, "reminder failed", "kind", "meeting", "stage", "mark", logging.ID("ticket_id", t.ID), "sent", true)
	}
}

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

func dayBeforeDeadline(startAt time.Time) time.Time {
	t := startAt.In(jst)
	return time.Date(t.Year(), t.Month(), t.Day()-1, dayBeforeHour, 0, 0, 0, jst)
}

func meetingDeadline(meetingAt time.Time) time.Time {
	return meetingAt.In(jst).Add(-meetingLeadTime)
}

func isDue(deadline, now time.Time, grace time.Duration) bool {
	if now.Before(deadline) {
		return false
	}
	if deadline.Before(now.Add(-grace)) {
		return false
	}
	return true
}

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
