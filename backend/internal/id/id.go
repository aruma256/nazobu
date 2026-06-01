// Package id はアプリ全体で使う一意 ID を生成する。
// ID は時系列ソート可能な UUIDv7（RFC 9562）を採用する。
package id

import "github.com/google/uuid"

// New は新しい UUIDv7 を文字列（36 文字, ハイフン区切りの小文字 hex）で返す。
// uuid.NewV7 は内部で crypto/rand を使う。エントロピー取得失敗は実運用では起きず、
// 万一起きても ID を採番できないなら処理を続ける意味がないため panic させる。
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}
