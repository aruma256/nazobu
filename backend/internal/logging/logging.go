// Package logging は個人情報を含めない構造化ログの共通処理を提供する。
package logging

import (
	"log/slog"

	"github.com/google/uuid"
)

// ID は value が UUID なら正規化して出力し、それ以外は "invalid" に置き換える。
// ID 欄に渡された任意の文字列をログへ流さないため。
func ID(key, value string) slog.Attr {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return slog.String(key, "invalid")
	}
	return slog.String(key, parsed.String())
}
