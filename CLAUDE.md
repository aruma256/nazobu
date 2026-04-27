# 謎部 (nazobu)

謎解き仲間のための身内 Web サービス。

- ドメイン: `nazobu.aruma256.dev`
- ホスティング: 自宅 Synology NAS 上の docker compose
- インターネット公開: Tailscale もしくは Cloudflare 経由（未確定）
- 想定機能（初期）:
  - 公演ごとの参加者管理（誰がどの公演に参加するか）
  - 参加費の精算管理（誰の精算が済んでいるか）
  - 自分が今月参加した公演のリスト表示
  - 前日のリマインド通知


## アーキテクチャ

- 構成: monorepo
- backend: Go 1.26（cobra CLI + Connect/gRPC）
- DB: MySQL 8（utf8mb4 / InnoDB）、主キーは ULID
- DB マイグレーション: sqldef による宣言型。`backend/sql/schema.sql` が SSOT
- 認証: DB 保存セッション（Cookie + token hash）+ OIDC
- ローカル開発: docker compose（backend は起動時に sqldef で自動マイグレーション）

## ポリシー

- PR タイトル / 本文・commit メッセージ・コードコメントは原則すべて日本語
- コンテナイメージの取得元は `mirror.gcr.io`
