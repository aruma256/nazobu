// Package id は主キー等の ID 採番を一元化する。
// 形式は UUIDv7（先頭 48bit がミリ秒タイムスタンプの時刻順ソート可能な UUID）。
// アプリ内では文字列で扱い、DB には CHAR(36) で格納する。
package id

import "github.com/google/uuid"

// New は新しい UUIDv7 を 36 文字のハイフン区切り文字列で返す。
// uuid.NewV7 は crypto/rand を使い、枯渇等で失敗し得るが採番は失敗してはならないため
// Must で panic させる（crypto/rand の失敗はプロセス継続不能なエラーとして扱う）。
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}
