# MCP 連携（Claude connector）

謎部を Claude のカスタムコネクタ（remote MCP server）として接続し、Claude から自分の参加予定などを参照できるようにする仕組み。

## 接続方法（ユーザー向け）

1. Claude.ai の Settings → Connectors → カスタムコネクタを追加
2. URL に `https://nazobu.aruma256.dev/mcp` を入力（Client Secret は空欄のまま）
3. ブラウザで謎部の Discord ログイン → 同意画面が開くので「許可する」

## 提供ツール

| ツール | 内容 |
|---|---|
| `list_my_upcoming_tickets` | 自分の今後参加予定のチケット一覧（開演日時・集合時刻・場所・同行者・参加費） |

書き込み系（公演・チケット登録）は今後追加予定。

## アーキテクチャ

- MCP エンドポイント: `/mcp`（公式 [go-sdk](https://github.com/modelcontextprotocol/go-sdk) の Streamable HTTP、stateless + JSON 応答）
- 認可: backend 自身が OAuth 2.1 認可サーバを兼ねる（`backend/internal/oauth`）。クライアント登録は **CIMD**（Client ID Metadata Document）方式のみ対応で、client_id は Claude がホストする HTTPS URL。事前登録（DCR）は無い
- ツール実装は既存の Connect RPC ハンドラを in-process 呼び出しで再利用する。Bearer 認証済みの user を `auth.WithUser` で context に注入し、`lookupSessionUser` が cookie より context を優先する

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

- ユニット: CIMD 検証 / redirect_uri 照合 / PKCE / authorize パラメータ / メタデータ形状（`internal/oauth`）
- 統合（実 MySQL）: 認可コードフロー一式（承認・拒否・PKCE 失敗・コード再利用・ローテーション・期限切れ）と、go-sdk クライアントによる `/mcp` 経由のツール呼び出し（`internal/oauth` / `internal/server/mcp_integration_test.go`）

## 未対応・今後

- write 系ツール（公演・チケット登録）。権限モデルの見直し（web / MCP とも role ベースで同等に）とセットで行う
- 期限切れ OAuth レコードの定期掃除（`DeleteExpiredOAuthRecords` / `DeleteExpiredOAuthTokens` クエリは用意済みで未配線）
- scope は現状 `read` のみ。write ツール追加時に拡張する
