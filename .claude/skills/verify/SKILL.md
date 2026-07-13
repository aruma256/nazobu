---
name: verify
description: nazobu をローカルで起動して変更を実際に動かして確認する手順（docker デーモンが無い環境向け）
---

# nazobu の動作確認手順

compose が使えない環境（docker デーモンなし）で backend + frontend を素で起動し、
ブラウザ（Playwright）で変更を実際に操作して確認する。

## 前提ツール

- MySQL 8 サーバ: `apt-get install -y mysql-server && service mysql start`
  - root パスワード設定: `mysql -uroot -e "ALTER USER 'root'@'localhost' IDENTIFIED WITH caching_sha2_password BY 'test'"`
- codegen が必要なら: `go install github.com/bufbuild/buf/cmd/buf@latest` / `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0`
  （sqlc はバージョンを揃えないと生成物のヘッダに差分ノイズが出る）

## DB 準備

```sh
mysql -uroot -ptest -e "DROP DATABASE IF EXISTS nazobu; CREATE DATABASE nazobu CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"
mysql -uroot -ptest nazobu < backend/sql/schema.sql
```

シード投入時は **必ず `--default-character-set=utf8mb4` を付ける**（付けないと日本語が文字化けする）。

## セッション（ログイン省略）

Discord OIDC を通さず、users + sessions を直接 INSERT する。
`sessions.token_hash` は cookie 値の SHA-256 hex（`printf 'トークン文字列' | sha256sum`）。
ブラウザには cookie `nazobu_session=<トークン文字列>` を積む。

## 起動

```sh
# backend（:8080）
cd backend && DB_HOST=127.0.0.1 DB_USER=root DB_PASSWORD=test DB_NAME=nazobu HTTP_ADDR=:8080 go run . start

# frontend（:3000、rewrites が localhost:8080 に proxy する）
cd frontend && pnpm install && pnpm build && pnpm start
```

## 操作

Playwright（`playwright-core` + `executablePath: '/opt/pw-browsers/chromium-1194/chrome-linux/chrome'`）で
`http://localhost:3000` を cookie 付きで開いて操作・スクリーンショット。
RPC の直接確認は `curl -X POST http://localhost:8080/nazobu.v1.<Service>/<Method> -H 'Content-Type: application/json' -H 'Cookie: nazobu_session=...' -d '{}'`。

## 統合テスト（参考）

```sh
TEST_DB_HOST=127.0.0.1 TEST_DB_PORT=3306 TEST_DB_USER=root TEST_DB_PASSWORD=test TEST_DB_NAME=nazobu_test go test ./...
```

## 注意

- `pkill -f next` は自分のセッションプロセスまで巻き込むことがある。frontend の再起動は
  `pkill -f next-server` → run_in_background で `pnpm start`。
- 古い next-server が残っていると再ビルド後も古いチャンクを配って 500 になる。再起動を確実に。
