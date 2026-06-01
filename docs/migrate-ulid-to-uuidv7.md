# ULID → UUIDv7 移行 runbook

既存の ULID 主キー/参照を valid な UUIDv7 へ移行する一回限りの手順。
ID 列は `VARCHAR(26)` → `CHAR(36)`、値は ULID の 128bit を保ったまま
version/variant のみ書き換える（タイムスタンプ・ソート順・参照を保持、再実行安全）。

## 注意

本番 backend は起動コマンドに `go run . migrate` を含むため、**普通に
`up -d --force-recreate` すると migrate（列拡張）だけ走って convert が漏れ、
ULID と UUIDv7 が混在する**。必ず下記の順（writer 停止 → dump → migrate →
convert → 起動）で実施する。

## 手順（停止して実施）

```bash
cd <リポジトリ>            # 本番 compose のあるディレクトリ
git pull                  # 本移行を含むコミットを取り込む
P="docker compose -f compose.yaml -f compose.prod.yaml"

# 1) writer を止める（mysql は起動したまま）
$P stop backend frontend

# 2) dump（= ロールバックポイント）。出力は NAS ホスト側に落ちる
$P exec -T mysql sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" \
  --single-transaction --routines --triggers nazobu' \
  > nazobu-backup-$(date +%Y%m%d-%H%M%S).sql
#   末尾に "-- Dump completed on ..." が出ている／サイズが妥当 を目視確認

# 3) migrate（CHAR(36) へ拡張）→ convert（ULID→UUIDv7）を停止中に単発実行
$P run --rm --no-deps backend sh -c \
  "go install github.com/sqldef/sqldef/v3/cmd/mysqldef@v3.11.1 \
   && go run . migrate \
   && go run . convert-ulid-to-uuidv7"

# 4) 再起動（start 時の migrate は差分ゼロで no-op）
$P up -d
```

`convert-ulid-to-uuidv7` は 36 文字（変換済み）の値を skip するので、途中で
中断しても再実行して問題ない。

## ロールバック

dump は冒頭で `FOREIGN_KEY_CHECKS=0` を立て各テーブルを `DROP TABLE IF EXISTS`
するため、流し戻すだけで FK 順序を気にせず復元できる。

```bash
P="docker compose -f compose.yaml -f compose.prod.yaml"
$P stop backend frontend
$P exec -T mysql sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" nazobu' \
  < nazobu-backup-YYYYMMDD-HHMMSS.sql
git checkout <移行前のコミット>   # アプリ側も ULID 採番に戻す
$P up -d --force-recreate
```

## 仕組みメモ

- 採番は `backend/internal/id` の `id.New()`（google/uuid の UUIDv7）に集約。
- 変換ロジックは `backend/cmd/convert_ulid_to_uuidv7.go`。FK に使われている列も
  一括で書き換えるため、変換中は `FOREIGN_KEY_CHECKS=0`（同じ変換を全カラムへ
  一様に適用するので参照整合性は保たれる）。
- `migrate`（sqldef）は FK 列の型変更が MySQL error 1833 で弾かれるのを避けるため
  `--before-apply=SET FOREIGN_KEY_CHECKS=0;` を付けている。
- proto / frontend / sqlc は ID が文字列のままなので無変更（API 互換）。
