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

// go test ./... はパッケージごとのテストバイナリを並列プロセスで走らせるため、DB 名をパッケージ単位で分ける。
func packageSuffix() string {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
}

func recreateDatabase(cfg config.DBConfig) error {
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

// go test の作業ディレクトリはテスト対象パッケージごとに変わるため、
// このファイル自身の位置から辿る。
func schemaPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "sql", "schema.sql")
}

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
