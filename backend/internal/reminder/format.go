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

// weekdayJP は time.Weekday（0=日曜）に対応する日本語の曜日。
var weekdayJP = [...]string{"日", "月", "火", "水", "木", "金", "土"}

// formatDayBefore は同日の前日リマインドを 1 通分の本文に整形する。
// tickets は同じ開演日のチケット群、subjectsByTicket は ticket_id → メンション対象。
// baseURL は nazobu フロントエンドのベース URL（チケット詳細リンクの組み立てに使う）。
// 返り値の第 2 戻り値は allowed_mentions 用に重複排除したメンション対象（送信順）。
func formatDayBefore(
	baseURL string,
	tickets []queries.ListTicketsForDayBeforeNotificationRow,
	subjectsByTicket map[string][]string,
) (string, []string) {
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

// formatMeeting は集合 2 時間前リマインドを 1 件分の本文に整形する。
// 集合・開演とも「当日」前提で時刻のみ表示する。baseURL はチケット詳細リンク用。
func formatMeeting(baseURL string, t queries.ListTicketsForMeetingNotificationRow, subjects []string) string {
	var b strings.Builder
	b.WriteString(meetingHeader)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "『%s』\n", t.EventTitle)
	// クエリで meeting_at IS NOT NULL を保証しているため Valid 前提。
	b.WriteString("📍 集合 " + formatTimeOnly(t.MeetingAt.Time))
	if t.MeetingPlace != "" {
		b.WriteString(" ／ " + t.MeetingPlace)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "🎭 開演 %s\n", formatTimeOnly(t.StartAt))
	b.WriteString(urlLine(ticketURL(baseURL, t.ID)))
	b.WriteString("\n")
	b.WriteString(mentionLine(subjects))
	return b.String()
}

// meetingLine は前日リマインドの集合行を組み立てる（集合情報が無ければ空文字）。
// 集合日が開演日と異なる（日跨ぎ集合）場合は日付付きで表示する。
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

// formatDateTimeFull は "6/14(日) 14:00" 形式（JST）。
func formatDateTimeFull(t time.Time) string {
	t = t.In(jst)
	return fmt.Sprintf("%d/%d(%s) %02d:%02d",
		int(t.Month()), t.Day(), weekdayJP[t.Weekday()], t.Hour(), t.Minute())
}

// formatTimeOnly は "14:00" 形式（JST）。
func formatTimeOnly(t time.Time) string {
	t = t.In(jst)
	return fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
}

// ticketURL は nazobu のチケット詳細ページ URL を組み立てる。
// フロントのルートは /tickets/[ticketId]。baseURL 末尾の "/" は重複を避けて除く。
func ticketURL(baseURL, ticketID string) string {
	return strings.TrimRight(baseURL, "/") + "/tickets/" + ticketID
}

// urlLine はチケット詳細へのリンク行を組み立てる。Discord の埋め込みプレビューを
// 抑止するため URL を <...> で囲む。
func urlLine(url string) string {
	return "🔗 <" + url + ">"
}

// mentionLine は Discord user id 群を "<@id> <@id>" のメンション行にする。
func mentionLine(subjects []string) string {
	parts := make([]string, len(subjects))
	for i, s := range subjects {
		parts[i] = "<@" + s + ">"
	}
	return strings.Join(parts, " ")
}

// sameDate は 2 つの時刻が JST で同じ日付かを返す。
func sameDate(a, b time.Time) bool {
	ay, am, ad := a.In(jst).Date()
	by, bm, bd := b.In(jst).Date()
	return ay == by && am == bm && ad == bd
}

// dedupePreserveOrder は出現順を保ったまま重複を除く。
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
