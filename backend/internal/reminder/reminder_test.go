package reminder

import (
	"database/sql"
	"testing"
	"time"

	"github.com/aruma256/nazobu/backend/internal/gen/queries"
)

func ts(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, jst)
}

func nt(t time.Time) sql.NullTime { return sql.NullTime{Time: t, Valid: true} }

func TestDayBeforeDeadline(t *testing.T) {
	cases := []struct {
		name    string
		startAt time.Time
		want    time.Time
	}{
		{"開演前日の20時", ts(2026, 6, 14, 14, 0), ts(2026, 6, 13, 20, 0)},
		{"開演当日が月初なら前月末の20時へ繰り下がる", ts(2026, 7, 1, 10, 0), ts(2026, 6, 30, 20, 0)},
		{"開演時刻に関わらず締切は前日20時固定", ts(2026, 6, 14, 0, 30), ts(2026, 6, 13, 20, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dayBeforeDeadline(c.startAt); !got.Equal(c.want) {
				t.Errorf("dayBeforeDeadline = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMeetingDeadline(t *testing.T) {
	got := meetingDeadline(ts(2026, 6, 14, 14, 0))
	want := ts(2026, 6, 14, 12, 0)
	if !got.Equal(want) {
		t.Errorf("meetingDeadline = %v, want %v", got, want)
	}
}

func TestIsDue(t *testing.T) {
	deadline := ts(2026, 6, 13, 20, 0)
	grace := 2 * time.Hour
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"締切前は false", deadline.Add(-time.Minute), false},
		{"締切ちょうどは true", deadline, true},
		{"締切後・猶予窓内は true", deadline.Add(time.Hour), true},
		{"猶予窓の境界ちょうどは true", deadline.Add(grace), true},
		{"猶予窓を超えたら false", deadline.Add(grace + time.Minute), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDue(deadline, c.now, grace); got != c.want {
				t.Errorf("isDue = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFormatDateTimeFull(t *testing.T) {
	// 2026-06-14 は日曜。
	if got := formatDateTimeFull(ts(2026, 6, 14, 14, 0)); got != "6/14(日) 14:00" {
		t.Errorf("formatDateTimeFull = %q", got)
	}
}

func TestFormatTimeOnly(t *testing.T) {
	if got := formatTimeOnly(ts(2026, 6, 14, 9, 5)); got != "09:05" {
		t.Errorf("formatTimeOnly = %q", got)
	}
}

func TestMentionLine(t *testing.T) {
	if got := mentionLine([]string{"111", "222"}); got != "<@111> <@222>" {
		t.Errorf("mentionLine = %q", got)
	}
}

func TestMeetingLine(t *testing.T) {
	start := ts(2026, 6, 14, 14, 0)
	cases := []struct {
		name      string
		meetingAt sql.NullTime
		place     string
		want      string
	}{
		{"時刻+場所", nt(ts(2026, 6, 14, 13, 30)), "東京駅 銀の鈴前", "📍 集合 13:30 ／ 東京駅 銀の鈴前"},
		{"時刻のみ（場所なし）", nt(ts(2026, 6, 14, 13, 30)), "", "📍 集合 13:30"},
		{"日跨ぎ集合は日付付き", nt(ts(2026, 6, 13, 23, 0)), "東京駅", "📍 集合 6/13(土) 23:00 ／ 東京駅"},
		{"集合時刻未定でも場所があれば出す", sql.NullTime{}, "東京駅", "📍 集合場所 東京駅"},
		{"集合情報なしは空", sql.NullTime{}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := meetingLine(c.meetingAt, c.place, start); got != c.want {
				t.Errorf("meetingLine = %q, want %q", got, c.want)
			}
		})
	}
}

func TestFormatDayBefore(t *testing.T) {
	tickets := []queries.ListTicketsForDayBeforeNotificationRow{
		{ID: "t1", StartAt: ts(2026, 6, 14, 14, 0), MeetingAt: nt(ts(2026, 6, 14, 13, 30)), MeetingPlace: "東京駅 銀の鈴前", EventTitle: "東京ミステリーサーカス"},
		{ID: "t2", StartAt: ts(2026, 6, 14, 18, 0), EventTitle: "ナゾトキ街歩き"},
	}
	subjects := map[string][]string{
		"t1": {"111", "222"},
		"t2": {"333"},
	}
	content, mentions := formatDayBefore("https://nazobu.aruma256.dev", tickets, subjects)

	want := "📅 明日の公演をお知らせするよ！\n" +
		"\n" +
		"『東京ミステリーサーカス』\n" +
		"🕖 開演 6/14(日) 14:00\n" +
		"📍 集合 13:30 ／ 東京駅 銀の鈴前\n" +
		"🔗 <https://nazobu.aruma256.dev/tickets/t1>\n" +
		"<@111> <@222>\n" +
		"\n" +
		"『ナゾトキ街歩き』\n" +
		"🕖 開演 6/14(日) 18:00\n" +
		"🔗 <https://nazobu.aruma256.dev/tickets/t2>\n" +
		"<@333>"
	if content != want {
		t.Errorf("content =\n%q\nwant\n%q", content, want)
	}
	if got := mentions; len(got) != 3 || got[0] != "111" || got[1] != "222" || got[2] != "333" {
		t.Errorf("mentions = %v", got)
	}
}

func TestFormatDayBeforeMentionDedup(t *testing.T) {
	// 同じ人が同日の 2 公演に参加 → メンション行は各公演に出るが、allowed_mentions は重複排除。
	tickets := []queries.ListTicketsForDayBeforeNotificationRow{
		{ID: "t1", StartAt: ts(2026, 6, 14, 14, 0), EventTitle: "A"},
		{ID: "t2", StartAt: ts(2026, 6, 14, 18, 0), EventTitle: "B"},
	}
	subjects := map[string][]string{"t1": {"111"}, "t2": {"111", "222"}}
	_, mentions := formatDayBefore("https://nazobu.aruma256.dev", tickets, subjects)
	if len(mentions) != 2 || mentions[0] != "111" || mentions[1] != "222" {
		t.Errorf("mentions = %v, want [111 222]", mentions)
	}
}

func TestFormatMeeting(t *testing.T) {
	row := queries.ListTicketsForMeetingNotificationRow{
		ID: "t1", StartAt: ts(2026, 6, 14, 14, 0), MeetingAt: nt(ts(2026, 6, 14, 13, 30)),
		MeetingPlace: "東京駅 銀の鈴前", EventTitle: "東京ミステリーサーカス",
	}
	got := formatMeeting("https://nazobu.aruma256.dev", row, []string{"111", "222"})
	want := "⏰ 今日の公演リマインド！みんな起きてる？\n" +
		"\n" +
		"『東京ミステリーサーカス』\n" +
		"📍 集合 13:30 ／ 東京駅 銀の鈴前\n" +
		"🎪 開演 14:00\n" +
		"🔗 <https://nazobu.aruma256.dev/tickets/t1>\n" +
		"<@111> <@222>"
	if got != want {
		t.Errorf("formatMeeting =\n%q\nwant\n%q", got, want)
	}
}

func TestGroupByStartDate(t *testing.T) {
	rows := []queries.ListTicketsForDayBeforeNotificationRow{
		{ID: "a", StartAt: ts(2026, 6, 14, 14, 0)},
		{ID: "b", StartAt: ts(2026, 6, 14, 18, 0)},
		{ID: "c", StartAt: ts(2026, 6, 15, 10, 0)},
	}
	groups := groupByStartDate(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(groups[0]) != 2 || groups[0][0].ID != "a" || groups[0][1].ID != "b" {
		t.Errorf("groups[0] = %v", groups[0])
	}
	if len(groups[1]) != 1 || groups[1][0].ID != "c" {
		t.Errorf("groups[1] = %v", groups[1])
	}
}

func TestDedupePreserveOrder(t *testing.T) {
	got := dedupePreserveOrder([]string{"1", "1", "2", "3", "2"})
	if len(got) != 3 || got[0] != "1" || got[1] != "2" || got[2] != "3" {
		t.Errorf("dedupePreserveOrder = %v", got)
	}
}
