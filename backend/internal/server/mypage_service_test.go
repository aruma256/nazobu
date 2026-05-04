package server

import (
	"testing"
	"time"
)

func TestClipHistoryEnd(t *testing.T) {
	t.Run("today < nextMonthStart なら today を返す（履歴に未来分を含めない）", func(t *testing.T) {
		today := time.Date(2026, 5, 4, 0, 0, 0, 0, jst)
		nextMonthStart := time.Date(2026, 6, 1, 0, 0, 0, 0, jst)
		got := clipHistoryEnd(nextMonthStart, today)
		if !got.Equal(today) {
			t.Errorf("got = %v, want %v", got, today)
		}
	})

	t.Run("today >= nextMonthStart なら nextMonthStart を返す（過去月そのまま）", func(t *testing.T) {
		today := time.Date(2026, 5, 4, 0, 0, 0, 0, jst)
		nextMonthStart := time.Date(2026, 4, 1, 0, 0, 0, 0, jst)
		got := clipHistoryEnd(nextMonthStart, today)
		if !got.Equal(nextMonthStart) {
			t.Errorf("got = %v, want %v", got, nextMonthStart)
		}
	})

	t.Run("today == nextMonthStart は等値なので nextMonthStart を返す", func(t *testing.T) {
		t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, jst)
		got := clipHistoryEnd(t0, t0)
		if !got.Equal(t0) {
			t.Errorf("got = %v, want %v", got, t0)
		}
	})
}

func TestJSTLocation(t *testing.T) {
	// jst が +9:00 で初期化されていることを固定する。
	_, offset := time.Now().In(jst).Zone()
	if offset != 9*3600 {
		t.Errorf("jst offset = %d, want %d", offset, 9*3600)
	}
}
