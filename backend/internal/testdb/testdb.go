// Package testdb は実 MySQL を使う統合テストの土台を提供する。
//
// 接続先は TEST_DB_* 環境変数で指定する（compose では mysql-test サービス、
// CI では workflow の services で立てた MySQL を指す）。TEST_DB_HOST が
// 未設定の環境では統合テストを skip し、ユニットテストだけが走る。
//
// スキーマはテストプロセス初回に DROP DATABASE → CREATE DATABASE →
// schema.sql 全適用で作り直す。テスト用 DB は毎回まっさらな前提なので
// sqldef の差分適用は使わず、SSOT の schema.sql をそのまま流す。
//
// go test ./... はパッケージごとのテストバイナリを並列プロセスで走らせるため、
// DB 名は TEST_DB_NAME にパッケージ由来の suffix を付けてプロセス間で分離する
// （共有すると他パッケージの DROP DATABASE / TRUNCATE が直撃する）。
// このため TEST_DB_USER には CREATE DATABASE できるユーザー（テスト専用
// MySQL の root）を渡す。
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/db"
)

var (
	setupOnce sync.Once
	setupErr  error
)

// Open は統合テスト用の DB 接続を返す。
// TEST_DB_HOST が未設定なら t.Skip する。呼び出しのたびに全テーブルを
// TRUNCATE するため、テスト間でデータは共有されない（このため t.Parallel とは併用しない）。
func Open(t *testing.T) *sql.DB {
	t.Helper()

	cfg := configFromEnv()
	if cfg.Host == "" {
		t.Skip("TEST_DB_HOST 未設定のため統合テストを skip")
	}

	setupOnce.Do(func() {
		setupErr = recreateDatabase(cfg)
	})
	if setupErr != nil {
		t.Fatalf("テスト DB の初期化に失敗（mysql-test は起動している？ `docker compose --profile test up -d mysql-test`）: %v", setupErr)
	}

	// 本番と同じ DSN 設定（parseTime / loc=Asia/Tokyo / utf8mb4）で接続する。
	conn, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("テスト DB への接続に失敗: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := truncateAll(conn); err != nil {
		t.Fatalf("テーブルの TRUNCATE に失敗: %v", err)
	}
	return conn
}

func configFromEnv() config.DBConfig {
	port := os.Getenv("TEST_DB_PORT")
	if port == "" {
		port = "3306"
	}
	return config.DBConfig{
		Host:     os.Getenv("TEST_DB_HOST"),
		Port:     port,
		User:     os.Getenv("TEST_DB_USER"),
		Password: os.Getenv("TEST_DB_PASSWORD"),
		Name:     os.Getenv("TEST_DB_NAME") + "_" + packageSuffix(),
	}
}

// packageSuffix はテストバイナリ名（= テスト対象パッケージ名）から DB 名用の
// suffix を作る。MySQL の識別子として安全なように英数字以外は _ に潰す。
func packageSuffix() string {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
}

// recreateDatabase は DB を作り直して schema.sql を適用する。
// 前回のテスト実行の残骸（mysql-test を上げっぱなしにしたケース）を確実に消すため、
// TRUNCATE ではなく DROP DATABASE から始める。
func recreateDatabase(cfg config.DBConfig) error {
	// DB 名なし & multiStatements で管理用に接続する（schema.sql を一括 Exec するため）。
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/?multiStatements=true&charset=utf8mb4&collation=utf8mb4_0900_ai_ci",
		cfg.User, cfg.Password, cfg.Host, cfg.Port,
	)
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer func() { _ = admin.Close() }()

	stmts := fmt.Sprintf(
		"DROP DATABASE IF EXISTS `%[1]s`; CREATE DATABASE `%[1]s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci; USE `%[1]s`;",
		cfg.Name,
	)
	schema, err := os.ReadFile(schemaPath())
	if err != nil {
		return fmt.Errorf("schema.sql の読み込みに失敗: %w", err)
	}
	if _, err := admin.Exec(stmts + string(schema)); err != nil {
		return fmt.Errorf("schema の適用に失敗: %w", err)
	}
	return nil
}

// schemaPath は SSOT の backend/sql/schema.sql を返す。
// go test の作業ディレクトリはテスト対象パッケージごとに変わるため、
// このファイル自身の位置から辿る。
func schemaPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "sql", "schema.sql")
}

// truncateAll はテスト DB の全テーブルを空にする。FK 順序を気にしないで済むよう、
// 同一セッション内で FOREIGN_KEY_CHECKS を一時的に外す。
func truncateAll(conn *sql.DB) error {
	rows, err := conn.Query(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'",
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// SET はセッション単位なので、専用の 1 コネクションを掴んで実行する。
	ctx := context.Background()
	c, err := conn.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err := c.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return err
	}
	for _, table := range tables {
		if _, err := c.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE `%s`", table)); err != nil {
			return err
		}
	}
	_, err = c.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1")
	return err
}
