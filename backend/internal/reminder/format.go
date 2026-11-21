package reminder

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

const (
	dayBeforeHeader = "📅 明日の公演をお知らせするよ！"
	meetingHeader   = "⏰ 今日の公演リマインド！みんな起きてる？"
)

var weekdayJP = [...]string{"日", "月", "火", "水", "木", "金", "土"}

func formatDayBefore(
	baseURL string,
	tickets []queries.ListTicketsForDayBeforeNotificationRow,
	subjectsByTicket map[string][]string,
) (content string, mentions []string) {
	var b strings.Builder
	b.WriteString(dayBeforeHeader)
	b.WriteString("\n")

	var allSubjects []string
	for _, t := range tickets {
		b.WriteString("\n")
		fmt.Fprintf(&b, "『%s』\n", t.EventTitle)
		fmt.Fprintf(&b, "🕖 開演 %s\n", formatDateTimeFull(t.StartAt))
		if line := meetingLine(t.MeetingAt, t.MeetingPlace, t.StartAt); line != "" {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString(urlLine(ticketURL(baseURL, t.ID)))
		b.WriteString("\n")
		if subs := subjectsByTicket[t.ID]; len(subs) > 0 {
			b.WriteString(mentionLine(subs))
			b.WriteString("\n")
			allSubjects = append(allSubjects, subs...)
		}
	}
	return strings.TrimRight(b.String(), "\n"), dedupePreserveOrder(allSubjects)
}

func formatMeeting(baseURL string, t queries.ListTicketsForMeetingNotificationRow, subjects []string) string {
	var b strings.Builder
	b.WriteString(meetingHeader)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "『%s』\n", t.EventTitle)
	b.WriteString("📍 集合 " + formatTimeOnly(t.MeetingAt.Time))
	if t.MeetingPlace != "" {
		b.WriteString(" ／ " + t.MeetingPlace)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "🎪 開演 %s\n", formatTimeOnly(t.StartAt))
	b.WriteString(urlLine(ticketURL(baseURL, t.ID)))
	b.WriteString("\n")
	b.WriteString(mentionLine(subjects))
	return b.String()
}

func meetingLine(meetingAt sql.NullTime, place string, startAt time.Time) string {
	if !meetingAt.Valid {
		if place != "" {
			return "📍 集合場所 " + place
		}
		return ""
	}
	var when string
	if sameDate(meetingAt.Time, startAt) {
		when = formatTimeOnly(meetingAt.Time)
	} else {
		when = formatDateTimeFull(meetingAt.Time)
	}
	if place != "" {
		return fmt.Sprintf("📍 集合 %s ／ %s", when, place)
	}
	return "📍 集合 " + when
}

func formatDateTimeFull(t time.Time) string {
	t = t.In(jst)
	return fmt.Sprintf("%d/%d(%s) %02d:%02d",
		int(t.Month()), t.Day(), weekdayJP[t.Weekday()], t.Hour(), t.Minute())
}

func formatTimeOnly(t time.Time) string {
	t = t.In(jst)
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

func ticketURL(baseURL, ticketID string) string {
	return strings.TrimRight(baseURL, "/") + "/tickets/" + ticketID
}

// urlLine はチケット詳細へのリンク行を組み立てる。Discord の埋め込みプレビューを
// 抑止するため URL を <...> で囲む。
func urlLine(url string) string {
	return "🔗 <" + url + ">"
}

func mentionLine(subjects []string) string {
	parts := make([]string, len(subjects))
	for i, s := range subjects {
		parts[i] = "<@" + s + ">"
	}
	return strings.Join(parts, " ")
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.In(jst).Date()
	by, bm, bd := b.In(jst).Date()
	return ay == by && am == bm && ad == bd
}

func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
