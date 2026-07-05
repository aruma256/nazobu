# 謎部 (nazobu)

謎解き仲間のための Web サービス。

- ドメイン: `nazobu.aruma256.dev`
- ホスティング: 自宅 Synology NAS 上の docker compose
- インターネット公開: Cloudflare Tunnel 経由
- 想定機能（初期）:
  - 公演ごとの参加者管理（誰がどの公演に参加するか）
  - 参加費の精算管理（誰の精算が済んでいるか）
  - 自分が今月参加した公演のリスト表示
  - 前日のリマインド通知


## アーキテクチャ

- 構成: monorepo
- backend: Go 1.26（cobra CLI + Connect/gRPC）
- DB: MySQL 8（utf8mb4 / InnoDB）、主キーは UUIDv7（CHAR(36)、採番は `backend/internal/id`）
- DB マイグレーション: sqldef による宣言型。`backend/sql/schema.sql` が SSOT
- DB アクセス: sqlc（設定は `backend/sqlc.yaml`）。クエリは `backend/sql/queries/*.sql`、生成物は `backend/internal/gen/queries/`。schema は sqldef と同じ `schema.sql` を参照する。生成物もコミットする（compose では codegen を回さない）
- 認証: DB 保存セッション（Cookie + token hash）+ OIDC
- ローカル開発: docker compose（backend は起動時に sqldef で自動マイグレーション）
- RPC: proto は `proto/nazobu/v1/*.proto` が SSOT。`buf generate` で `backend/internal/gen/` と `frontend/app/gen/` を生成し、生成物もコミットする（compose では codegen を回さない）
- 統合テスト: 実 MySQL を使う（ローカルは compose の `mysql-test`、CI は workflow の service container）。接続先は `TEST_DB_*` 環境変数で渡し、未設定なら skip してユニットテストのみ走る。ヘルパーは `backend/internal/testdb`（初回に `schema.sql` を全適用、テストごとに全テーブル TRUNCATE）

## ポリシー

- PR タイトル / 本文・commit メッセージ・コードコメントは原則すべて日本語
- コンテナイメージの取得元は `mirror.gcr.io`
