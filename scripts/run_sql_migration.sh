#!/bin/sh
# 任意の SQL ファイルを本番 DB に適用するための補助スクリプト。
# 想定: 旧スキーマ前提のコードが動いている状態で 1 回手動実行し、
# その直後に scripts/deploy.sh で新コード（schema.sql 更新済み）をデプロイする。
#
# 使い方:
#   sudo ./scripts/run_sql_migration.sh backend/sql/migrations/20260502_consolidate_datetime.sql
#
# 動作:
#   1. tickets テーブルだけ事前に mysqldump で退避（万一のロールバック用）。
#   2. 引数の SQL を docker compose の mysql コンテナに流し込む。
#
# 前提:
#   - cwd が compose の置いてあるリポジトリルート、または cd 済み。
#   - .env に DB_USER / DB_PASSWORD / DB_NAME が定義されていて compose が読める。
set -eu

if [ $# -ne 1 ]; then
  echo "usage: $0 <path-to-sql-file>" >&2
  exit 1
fi

SQL_FILE="$1"
if [ ! -f "$SQL_FILE" ]; then
  echo "SQL ファイルが見つからない: $SQL_FILE" >&2
  exit 1
fi

# .env を読み込んで DB 認証情報を取得（compose と同じ方式）。
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

: "${DB_USER:?DB_USER が未設定}"
: "${DB_PASSWORD:?DB_PASSWORD が未設定}"
: "${DB_NAME:?DB_NAME が未設定}"

BACKUP_DIR="backups"
mkdir -p "$BACKUP_DIR"
TS=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/tickets_${TS}.sql"

echo "==> tickets テーブルをバックアップ: $BACKUP_FILE"
sudo docker compose -f compose.yaml -f compose.prod.yaml exec -T mysql \
  mysqldump -u"$DB_USER" -p"$DB_PASSWORD" "$DB_NAME" tickets > "$BACKUP_FILE"

echo "==> SQL 適用: $SQL_FILE"
sudo docker compose -f compose.yaml -f compose.prod.yaml exec -T mysql \
  mysql -u"$DB_USER" -p"$DB_PASSWORD" "$DB_NAME" < "$SQL_FILE"

echo "==> 完了。新コードをデプロイする場合は scripts/deploy.sh を続けて実行してください。"
