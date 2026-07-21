# MCP 連携（Claude connector）

謎部を Claude のカスタムコネクタ（remote MCP server）として接続し、Claude から自分の参加予定の参照や公演・チケットの登録をできるようにする仕組み。

## 接続方法（ユーザー向け）

1. Claude.ai の Settings → Connectors → カスタムコネクタを追加
2. URL に `https://nazobu.aruma256.dev/mcp` を入力（Client Secret は空欄のまま）
3. ブラウザで謎部の Discord ログイン → 同意画面が開くので「許可する」

## 提供ツール

| ツール | 必要 scope | 内容 |
|---|---|---|
| `list_my_upcoming_tickets` | - | 自分の今後参加予定のチケット一覧（開演日時・集合時刻・場所・同行者・参加費） |
| `list_tickets` | - | 登録済みの全チケット一覧（過去の公演も含む、開演日時の降順） |
| `get_ticket` | - | チケット 1 件の詳細。参加者ごとの精算状況（精算済みか・立替者か）を含む |
| `list_users` | - | 登録メンバー一覧（`user_id` と表示名）。参加者指定の前に ID を引く用途 |
| `create_ticket_with_event` | `write` | 公演とチケットの同時登録（web の新規登録と同じ `CreateTicketWithEvent` RPC を再利用）。立替者は自分になる。admin ロールが必要 |
| `update_ticket_with_event` | `write` | チケットと紐づく公演の部分更新。admin もしくは立替者のみ |
| `list_expenses` | - | 追加精算（飲み会等）の一覧（発生日の降順）。`ticket_id` での絞り込み可 |
| `get_expense` | - | 追加精算 1 件の詳細。参加者ごとの負担額と精算状況を含む |
| `create_expense` | `write` | 追加精算の登録。立替者は自分になる（参加者に自分を含めない）。member でも可 |
| `update_expense` | `write` | 追加精算の部分更新。`participants` 指定時は全量置換（精算状態は保持）。admin もしくは立替者のみ |
| `update_expense_participant_settlement` | `write` | 追加精算の参加者 1 人の精算状態トグル。admin もしくは立替者のみ |

### update_ticket_with_event / update_expense の部分更新

`UpdateTicketWithEvent` / `UpdateExpense` RPC は全置換だが、MCP ツール側で「現在値を取得（`GetTicket` + `GetEvent` / `GetExpense`）→ 指定フィールドだけ上書き → 全フィールド送信」するラッパーにしている。web の編集 form が現在値をロードしてから全送信するのと同じクライアント責務で、置換セマンティクス・バリデーション・権限は RPC 側に集約されたまま。

- 省略（null）したフィールドは現在値を維持
- `meeting_at` / `meeting_place` / `event_catchphrase` は空文字で未設定に戻す
- `event_doors_open_minutes_before` / `event_entry_deadline_minutes_before` は `-1` で未設定に戻す
- `update_expense` の `ticket_id` は空文字で紐付け解除。`participants` は省略で現在の参加者を維持、指定で全量置換（残った参加者の settled は保持、外した参加者は記録ごと削除）

削除系（`DeleteExpense`）は誤操作リスクを踏まえ MCP には出していない（web のみ）。

### scope

- `read` / `write` の 2 つ。認可リクエストで scope 未指定の場合は両方を既定で付与する（付与内容は同意画面に明示される）
- write ツールはトークンの `write` scope をツール側で確認する。**write scope 追加前に接続したコネクタのトークンは `read` のみ**なので、書き込みを使うにはコネクタを一度削除して接続し直す必要がある

## アーキテクチャ

- MCP エンドポイント: `/mcp`（公式 [go-sdk](https://github.com/modelcontextprotocol/go-sdk) の Streamable HTTP、stateless + JSON 応答）
- 認可: backend 自身が OAuth 2.1 認可サーバを兼ねる（`backend/internal/oauth`）。クライアント登録は **CIMD**（Client ID Metadata Document）方式のみ対応で、client_id は Claude がホストする HTTPS URL。事前登録（DCR）は無い
- ツール実装は既存の Connect RPC ハンドラを in-process 呼び出しで再利用する。Bearer 認証済みの user を `auth.WithUser` で context に注入し、`lookupSessionUser` が cookie より context を優先する。role による権限チェックも RPC ハンドラ側で行われるため、web と MCP で常に同一になる

### エンドポイント一覧

| パス | 役割 |
|---|---|
| `/.well-known/oauth-authorization-server` | RFC 8414 メタデータ。`client_id_metadata_document_supported: true` と `token_endpoint_auth_methods_supported: ["none"]` の両方を広告すると Claude が CIMD 方式を選ぶ |
| `/.well-known/oauth-protected-resource` | RFC 9728 メタデータ。`resource` は Claude に入力する MCP URL と完全一致が必要 |
| `GET/POST /oauth/authorize` | CIMD の取得・検証 → 同意画面（backend 描画の素の HTML）。未ログインなら Discord ログインへ `next` 付きで往復 |
| `POST /oauth/token` | `authorization_code` / `refresh_token` grant（form-urlencoded） |
| `/mcp` | MCP 本体。トークン無しは 401 + `WWW-Authenticate: Bearer resource_metadata="..."` を返し、Claude はここから認可フローを開始する |

これらはすべて frontend の rewrites（`next.config.ts`）で backend に proxy され、外部からは frontend と同一 origin に見える。`proxy.ts` の認証ガードからも除外している。issuer / resource の base URL は `FRONTEND_URL` を流用する。

### セキュリティ上のポイント

- PKCE S256 必須、public client（クライアント認証なし）
- 認可コードは単回使用（5 分）。アクセストークン 1 時間 / リフレッシュトークン 30 日で、refresh のたびに両方ローテーション（OAuth 2.1 の public client 要件）
- コード・トークンは sessions と同様 SHA-256 hash のみ DB 保存（`oauth_authorization_codes` / `oauth_tokens`）
- redirect_uri は CIMD の登録値と完全一致。例外としてループバック（`http://localhost` / `http://127.0.0.1`）のみ RFC 8252 7.3 に従い port を無視して比較する（Claude Code がセッションごとに ephemeral port を使うため）。同意画面ではループバック宛に警告を表示する
- CIMD 取得は https のみ・プライベートアドレス拒否・64KB 上限・5 分キャッシュ

## テスト

- ユニット: CIMD 検証 / redirect_uri 照合 / PKCE / authorize パラメータ（scope 既定値・未知 scope 拒否を含む）/ メタデータ形状（`internal/oauth`）
- 統合（実 MySQL）: 認可コードフロー一式（承認・拒否・PKCE 失敗・コード再利用・ローテーション・期限切れ）と、go-sdk クライアントによる `/mcp` 経由のツール呼び出し（read 系 + write 系の正常系 / 部分更新の維持・クリア / write scope 不足 / ロール・立替者権限の拒否）（`internal/oauth` / `internal/server/mcp_integration_test.go`）

## 未対応・今後

- write 系ツールの拡充（チケット参加者管理・チケット代の精算状態の更新など。expense 系は対応済み）
- 期限切れ OAuth レコードの定期掃除（`DeleteExpiredOAuthRecords` / `DeleteExpiredOAuthTokens` クエリは用意済みで未配線）
