-- 本番データ移行: tickets.attended_on / start_time / meeting_time を
-- start_at DATETIME(6) / meeting_at DATETIME(6) に統合する。
--
-- 実行タイミング:
--   この SQL は新コード（schema.sql 更新版）をデプロイする「直前」に手動で 1 回実行する。
--   実行後の DB は新 schema.sql と一致するため、後続の sqldef は no-op になる。
--
-- 実行方法（compose 環境例）:
--   docker compose exec -T mysql mysql -u"$DB_USER" -p"$DB_PASSWORD" "$DB_NAME" \
--     < backend/sql/migrations/20260502_consolidate_datetime.sql
--
-- 前提:
--   - DATE / TIME カラムは JST 基準で運用されている（driver loc=Asia/Tokyo）。
--   - TIMESTAMP(date, time) は naive な DATETIME を返すので、そのまま JST 基準値として保持される。
--
-- 旧カラムを落とす破壊的変更を含む。失敗時に戻せるよう、念のため事前に
-- mysqldump で tickets テーブルを退避してから実行することを推奨する。
--   docker compose exec -T mysql mysqldump -u"$DB_USER" -p"$DB_PASSWORD" \
--     "$DB_NAME" tickets > tickets_backup_$(date +%Y%m%d_%H%M%S).sql

START TRANSACTION;

-- 1. 新カラムを NULL 許容で追加。
ALTER TABLE tickets
  ADD COLUMN start_at   DATETIME(6) NULL AFTER event_id,
  ADD COLUMN meeting_at DATETIME(6) NULL AFTER start_at;

-- 2. 旧カラムから値を組み立てて backfill。
--    meeting_time が NULL の行は meeting_at も NULL のまま残す。
UPDATE tickets
SET start_at   = TIMESTAMP(attended_on, start_time),
    meeting_at = IF(meeting_time IS NULL, NULL, TIMESTAMP(attended_on, meeting_time));

-- 3. 旧カラムを削除し、start_at を NOT NULL に確定。
--    関連インデックス（idx_tickets_attended_on, idx_tickets_purchased_by_attended_on）は
--    参照カラム削除に伴い MySQL が自動で破棄する。
ALTER TABLE tickets
  DROP COLUMN attended_on,
  DROP COLUMN start_time,
  DROP COLUMN meeting_time,
  MODIFY COLUMN start_at DATETIME(6) NOT NULL;

-- 4. 新カラム向けのインデックスを張る。
ALTER TABLE tickets
  ADD INDEX idx_tickets_start_at (start_at),
  ADD INDEX idx_tickets_purchased_by_start_at (purchased_by, start_at);

COMMIT;
