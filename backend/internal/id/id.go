package id

import "github.com/google/uuid"

// New は新しい UUIDv7 を 36 文字のハイフン区切り文字列で返す。
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}
