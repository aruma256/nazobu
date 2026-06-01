package cmd

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
)

// idColumns は ULID を保持している全カラム（PK / FK 双方）の一覧。
// 変換は値ごとに決定論的なので、PK と FK を個別に変換しても整合する。
// （ON UPDATE CASCADE は張っていないため、子側の FK カラムも明示的に変換する。）
var idColumns = []struct {
	table  string
	column string
}{
	{"users", "id"},
	{"user_identities", "user_id"},
	{"sessions", "id"},
	{"sessions", "user_id"},
	{"events", "id"},
	{"tickets", "id"},
	{"tickets", "event_id"},
	{"tickets", "purchased_by"},
	{"ticket_participants", "ticket_id"},
	{"ticket_participants", "user_id"},
}

var convertUlidCmd = &cobra.Command{
	Use:   "convert-ulid-to-uuidv7",
	Short: "既存データの ULID を UUIDv7 へ一括変換する（ULID→UUIDv7 移行用の一回限りのバッチ）",
	Long: "全テーブルの ID カラム（PK / FK）に入っている 26 文字の ULID を、同じ 128bit を保ったまま\n" +
		"正規の UUIDv7 文字列へ変換する。先に migrate でスキーマを CHAR(36) へ広げてから実行すること。\n" +
		"値ごとに決定論的な変換のため再実行しても安全（変換済みの値はそのまま）。",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		conn, err := db.Open(cfg.DB)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		ctx := cmd.Context()

		// FK チェックを無効化したいので 1 本のコネクションに固定して作業する。
		// PK を書き換えると FK と一時的に不整合になるため、全カラム変換が終わるまで
		// 制約チェックを止める。最後に必ず元へ戻す。
		c, err := conn.Conn(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()

		if _, err := c.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
			return fmt.Errorf("FOREIGN_KEY_CHECKS の無効化に失敗: %w", err)
		}
		defer func() { _, _ = c.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1") }()

		tx, err := c.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		total := 0
		for _, col := range idColumns {
			n, err := convertColumn(ctx, tx, col.table, col.column)
			if err != nil {
				return fmt.Errorf("%s.%s の変換に失敗: %w", col.table, col.column, err)
			}
			fmt.Printf("%s.%s: %d 件変換\n", col.table, col.column, n)
			total += n
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("コミットに失敗: %w", err)
		}
		fmt.Printf("変換完了: 合計 %d 件\n", total)
		return nil
	},
}

// convertColumn は (table, column) の全 distinct 値を ULID→UUIDv7 へ変換する。
// table / column は固定の allowlist 由来なので識別子を直接埋め込んでよい。
func convertColumn(ctx context.Context, tx *sql.Tx, table, column string) (int, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT DISTINCT %s FROM %s", column, table)) //nolint:gosec // 固定の allowlist
	if err != nil {
		return 0, err
	}
	var olds []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return 0, err
		}
		olds = append(olds, v)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()

	converted := 0
	for _, old := range olds {
		newID, err := ulidToUUIDv7(old)
		if err != nil {
			return 0, err
		}
		if newID == old {
			continue // 変換済み（既に UUID）はスキップ
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", table, column, column), //nolint:gosec // 固定の allowlist
			newID, old,
		); err != nil {
			return 0, err
		}
		converted++
	}
	return converted, nil
}

// ulidToUUIDv7 は 26 文字の ULID を、同じ 128bit を保ったまま正規の UUIDv7 文字列へ変換する。
// ULID と UUIDv7 はいずれも先頭 48bit がミリ秒タイムスタンプなので、version(4bit) と
// variant(2bit) の計 6bit だけを上書きすれば、タイムスタンプとソート順を保ったまま valid な
// UUIDv7 になる。値ごとに決定論的なので、同じ ULID は常に同じ UUID になる。
// 既に UUID 形式の値が渡された場合はそのまま返す（再実行しても安全なように冪等化）。
func ulidToUUIDv7(s string) (string, error) {
	if _, err := uuid.Parse(s); err == nil {
		return s, nil // 既に変換済み
	}
	u, err := ulid.Parse(s)
	if err != nil {
		return "", fmt.Errorf("ULID として解釈できない値: %q: %w", s, err)
	}
	b := [16]byte(u)
	b[6] = (b[6] & 0x0f) | 0x70 // version = 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant = 0b10 (RFC 4122)
	return uuid.UUID(b).String(), nil
}
