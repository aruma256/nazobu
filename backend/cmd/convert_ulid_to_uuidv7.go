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

// idColumns は ID（主キーおよび ID を参照する FK）を保持する全カラム。
// 変換は決定論的に全カラムへ一様に適用するため、同じ ULID は常に同じ UUIDv7 へ写り、
// 参照（FK）の整合性は保たれる。
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

var convertULIDToUUIDv7Cmd = &cobra.Command{
	Use:   "convert-ulid-to-uuidv7",
	Short: "既存の ULID 主キー/参照を valid な UUIDv7 へ変換する（一回限りのバッチ）",
	Long: "ULID(VARCHAR(26)) で採番された既存 ID を UUIDv7(CHAR(36)) へ移行する。\n" +
		"事前に migrate で全 ID カラムを CHAR(36) へ拡張しておくこと。\n" +
		"ULID の 128bit を保ったまま version/variant のみ上書きするため、タイムスタンプ・\n" +
		"ソート順・参照関係を保持する。決定論的かつ再実行安全（変換済みの 36 文字値は skip）。",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()
		conn, err := db.Open(cfg.DB)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		return convertULIDToUUIDv7(cmd.Context(), conn)
	},
}

func init() {
	rootCmd.AddCommand(convertULIDToUUIDv7Cmd)
}

// convertULIDToUUIDv7 は idColumns の全カラムを 1 トランザクションで変換する。
// PK と FK を同時に書き換えるため、変換中は FOREIGN_KEY_CHECKS を無効化する
// （同一の変換を全カラムへ適用するので、commit 後の参照整合性は保たれる）。
func convertULIDToUUIDv7(ctx context.Context, conn *sql.DB) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("トランザクション開始に失敗: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// PK→FK の順序に依存せず一括変換するため FK チェックを一時的に無効化する。
	// この設定は tx が握る単一コネクションに対してのみ効く。
	if _, err := tx.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return fmt.Errorf("FOREIGN_KEY_CHECKS=0 に失敗: %w", err)
	}

	total := 0
	for _, c := range idColumns {
		n, err := convertColumn(ctx, tx, c.table, c.column)
		if err != nil {
			return fmt.Errorf("%s.%s の変換に失敗: %w", c.table, c.column, err)
		}
		fmt.Printf("%s.%s: %d 件変換\n", c.table, c.column, n)
		total += n
	}

	// commit 前にコネクションの FK チェックを元に戻す（pool 返却後の他処理への漏れ防止）。
	if _, err := tx.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1"); err != nil {
		return fmt.Errorf("FOREIGN_KEY_CHECKS=1 に失敗: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit に失敗: %w", err)
	}
	fmt.Printf("変換完了: 合計 %d 件\n", total)
	return nil
}

// convertColumn は 1 カラム内の ULID 値（26 文字）を UUIDv7（36 文字）へ書き換え、
// 変換した件数を返す。36 文字値は変換済みとみなして skip するため再実行しても安全。
func convertColumn(ctx context.Context, tx *sql.Tx, table, column string) (int, error) {
	// table/column はコード内の定数（idColumns）のみで、外部入力ではないため文字列連結で組む。
	// 値そのものはプレースホルダでバインドする。
	rows, err := tx.QueryContext(ctx,
		fmt.Sprintf("SELECT DISTINCT `%s` FROM `%s`", column, table))
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return 0, err
		}
		values = append(values, v)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// 走査中に同テーブルへ UPDATE を流せないため、値を読み切ってから更新する。
	_ = rows.Close()

	updateSQL := fmt.Sprintf("UPDATE `%s` SET `%s` = ? WHERE `%s` = ?", table, column, column)
	n := 0
	for _, old := range values {
		switch len(old) {
		case 36:
			continue // 変換済み（UUIDv7）
		case 26:
			// 想定どおりの ULID。下で変換する。
		default:
			return 0, fmt.Errorf("想定外の長さ %d の値: %q", len(old), old)
		}

		newID, err := ulidStringToUUIDv7(old)
		if err != nil {
			return 0, fmt.Errorf("値 %q の変換に失敗: %w", old, err)
		}
		if _, err := tx.ExecContext(ctx, updateSQL, newID, old); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// ulidStringToUUIDv7 は ULID 文字列を valid な UUIDv7 文字列へ変換する。
// ULID の 128bit のうち version nibble（byte 6 上位 4bit）を 0b0111、
// variant（byte 8 上位 2bit）を 0b10 に上書きするだけなので、先頭 48bit の
// ミリ秒タイムスタンプは保たれ、ソート順・時刻情報が維持される。純粋関数で決定論的。
func ulidStringToUUIDv7(s string) (string, error) {
	u, err := ulid.Parse(s)
	if err != nil {
		return "", err
	}
	b := [16]byte(u) // ulid.ULID は [16]byte。先頭 6byte が timestamp。
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	out, err := uuid.FromBytes(b[:])
	if err != nil {
		return "", err
	}
	return out.String(), nil
}
