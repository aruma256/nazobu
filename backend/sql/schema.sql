-- 謎部 DB スキーマ（SSOT）
-- ここに DDL を記述すると、起動時に sqldef が現状 DB との差分を計算して適用する。
-- 文字コード: utf8mb4 / エンジン: InnoDB / ID 型: CHAR(36) UUIDv7

-- 内部ユーザー。IdP 非依存の anchor。
-- 表示用プロフィール（display_name / avatar_url）はログイン時に
-- 連携元 IdP の値で更新するキャッシュ。IdP に依存しない形で持たせる。
-- display_name は IdP 側で未設定のことがあるので、ログイン時に handle 名等を
-- フォールバックとして必ず埋めて NOT NULL を保つ。
CREATE TABLE users (
  id            CHAR(36)  NOT NULL,
  display_name  VARCHAR(255) NOT NULL,
  avatar_url    VARCHAR(512) NULL,
  notifications_enabled TINYINT(1) NOT NULL DEFAULT 1,
  -- 'admin' = 管理者 / 'member' = 一般メンバー。新規ユーザーは member スタート、admin は手動昇格。
  role          VARCHAR(16)  NOT NULL DEFAULT 'member',
  created_at    DATETIME(6)  NOT NULL,
  updated_at    DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT chk_users_role CHECK (role IN ('admin', 'member'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- IdP の identity。(provider, subject) でログイン時に user を引く。
-- provider は 'discord' / 'google' 等の識別子、subject は IdP 内のユーザー ID
-- （OIDC の sub 相当）。1 user に複数 IdP を紐付けられる前提の N:1。
CREATE TABLE user_identities (
  user_id     CHAR(36)  NOT NULL,
  provider    VARCHAR(32)  NOT NULL,
  subject     VARCHAR(255) NOT NULL,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (provider, subject),
  KEY idx_user_identities_user_id (user_id),
  CONSTRAINT fk_user_identities_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  -- 当面 Discord のみで運用するため provider を固定する。新規 IdP を増やす際にここを更新する。
  -- ※ sqldef + MySQL では 1 要素の IN (...) に既知のバグがあるため = で書く。
  CONSTRAINT chk_user_identities_provider CHECK (provider = 'discord')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sessions (
  id          CHAR(36) NOT NULL,
  user_id     CHAR(36) NOT NULL,
  -- token そのものではなく SHA-256 hex (64 chars) を保存する。漏洩時に raw token を復元できないように。
  token_hash  CHAR(64)    NOT NULL,
  expires_at  DATETIME(6) NOT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_sessions_token_hash (token_hash),
  KEY idx_sessions_user_id (user_id),
  CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- MCP 連携（Claude connector）用 OAuth 2.1 の認可コード。
-- client_id は CIMD（Client ID Metadata Document）方式のため、事前登録した ID ではなく
-- クライアントがホストする HTTPS URL をそのまま保存する。
-- コードは単回使用（token 交換時に削除）なので used フラグは持たない。
CREATE TABLE oauth_authorization_codes (
  id              CHAR(36)     NOT NULL,
  -- 認可コードの SHA-256 hex。sessions.token_hash と同じく raw 値は保存しない。
  code_hash       CHAR(64)     NOT NULL,
  user_id         CHAR(36)     NOT NULL,
  client_id       VARCHAR(512) NOT NULL,
  redirect_uri    VARCHAR(512) NOT NULL,
  scope           VARCHAR(255) NOT NULL,
  -- PKCE の code_challenge。method は S256 のみ受け付けるためカラムでは持たない。
  code_challenge  VARCHAR(128) NOT NULL,
  expires_at      DATETIME(6)  NOT NULL,
  created_at      DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_oauth_authorization_codes_code_hash (code_hash),
  CONSTRAINT fk_oauth_authorization_codes_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- MCP 連携用のアクセストークン / リフレッシュトークンのペア。
-- refresh 時はこの行を UPDATE してローテーションする（OAuth 2.1 の public client 要件）。
CREATE TABLE oauth_tokens (
  id                        CHAR(36)     NOT NULL,
  user_id                   CHAR(36)     NOT NULL,
  client_id                 VARCHAR(512) NOT NULL,
  scope                     VARCHAR(255) NOT NULL,
  access_token_hash         CHAR(64)     NOT NULL,
  access_token_expires_at   DATETIME(6)  NOT NULL,
  refresh_token_hash        CHAR(64)     NOT NULL,
  refresh_token_expires_at  DATETIME(6)  NOT NULL,
  created_at                DATETIME(6)  NOT NULL,
  updated_at                DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_oauth_tokens_access_token_hash (access_token_hash),
  UNIQUE KEY uq_oauth_tokens_refresh_token_hash (refresh_token_hash),
  KEY idx_oauth_tokens_user_id (user_id),
  CONSTRAINT fk_oauth_tokens_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 謎解きイベント（公演）。長期開催が一般的なので、開催日は event ではなく
-- ticket（実際に参加した日）側に持たせる。
CREATE TABLE events (
  id          CHAR(36)  NOT NULL,
  title       VARCHAR(255) NOT NULL,
  url         VARCHAR(512) NOT NULL,
  -- 公演のキャッチコピー（手動入力）。空文字を「未設定」として許容する。
  catchphrase VARCHAR(255) NOT NULL DEFAULT '',
  -- 公演 URL から取得した OG 画像の URL。allowlist ドメインのみ取得し、
  -- 取得失敗時は NULL のまま。表示は外部画像 URL を直接埋め込む方針。
  image_url   VARCHAR(2048) NULL,
  -- 開場時間が開演時刻（ticket.start_at）の何分前か。0 以上、NULL = 未設定。
  doors_open_minutes_before     INT NULL,
  -- 入場締切が開演時刻の何分前か。これを過ぎると参加できない。0 以上、NULL = 未設定。
  entry_deadline_minutes_before INT NULL,
  -- 想定所要時間（分）。カレンダー連携で終了時刻を算出するために使う。1 以上。デフォルトは 120 分。
  expected_duration_minutes     INT NOT NULL DEFAULT 120,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- チケット 1 枚。グループチケットなら 1 枚で複数人が参加でき、参加者で割り勘する。
-- price_per_person は一人あたりの税込・円（割り勘済み）。purchased_by が立て替え、
-- ticket_participants が割り勘元。
--
-- 開演日時 / 集合時刻はいずれも JST naive な DATETIME として保持する
-- （driver は loc=Asia/Tokyo で動かしているため Go の time.Time は JST で出入りする）。
CREATE TABLE tickets (
  id                CHAR(36)  NOT NULL,
  event_id          CHAR(36)  NOT NULL,
  -- 公演の開演日時（JST）。日付 / 時刻はここから派生する。
  start_at          DATETIME(6)  NOT NULL,
  -- 集合日時（JST）。集合時刻が決まっていないときは NULL。日跨ぎ集合にも対応できる。
  meeting_at        DATETIME(6)  NULL,
  price_per_person  INT          NOT NULL,
  -- このチケット 1 枚で参加できる最大人数。ticket_participants の紐づけ数と
  -- unregistered_participants_count の合計がこの値以下になるようアプリ層で担保する。
  max_participants  INT          NOT NULL,
  -- 本サービスに未登録の同行者の人数。0 以上。登録ユーザーと同様に max_participants の枠を消費する。
  -- 未登録者は個人を特定できないため、精算管理の対象外（人数の記録・表示のみ）。
  unregistered_participants_count INT NOT NULL DEFAULT 0,
  purchased_by      CHAR(36)  NOT NULL,
  -- 集合場所。空文字を「未設定」として許容する。
  meeting_place     VARCHAR(255) NOT NULL,
  -- 前日リマインド通知にこのチケットを含めて送信した時刻（JST）。NULL = 未送信。
  -- 同日に複数チケットがある場合は 1 通にまとめて送り、含めた全チケットにこの時刻を立てる。
  day_before_notified_at  DATETIME(6) NULL DEFAULT NULL,
  -- 集合 2 時間前リマインド通知を送信した時刻（JST）。NULL = 未送信。
  meeting_notified_at     DATETIME(6) NULL DEFAULT NULL,
  created_at        DATETIME(6)  NOT NULL,
  updated_at        DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_tickets_event_id (event_id),
  KEY idx_tickets_start_at (start_at),
  KEY idx_tickets_purchased_by_start_at (purchased_by, start_at),
  CONSTRAINT fk_tickets_event_id     FOREIGN KEY (event_id)     REFERENCES events(id) ON DELETE CASCADE,
  CONSTRAINT fk_tickets_purchased_by FOREIGN KEY (purchased_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ticket と参加者の M:N。誰がどのチケットで参加したか。
-- settled_at は立て替え分を購入者へ精算した時刻。NULL = 未精算（デフォルト）、
-- 非 NULL = 精算済み。タイムスタンプを兼ねることで「いつ精算したか」も保持する。
CREATE TABLE ticket_participants (
  ticket_id   CHAR(36) NOT NULL,
  user_id     CHAR(36) NOT NULL,
  settled_at  DATETIME(6) NULL DEFAULT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (ticket_id, user_id),
  KEY idx_ticket_participants_user_id (user_id),
  CONSTRAINT fk_ticket_participants_ticket_id FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_participants_user_id   FOREIGN KEY (user_id)   REFERENCES users(id)   ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ticket にぶら下がる追加精算（打ち上げ飲み会など）。1 ticket に複数持てる。
-- チケット代と同じく「立替者へ各自が支払う」モデルだが、立替者（paid_by）は
-- チケット購入者と別人でも構わない。金額は参加者ごとに持つため本体には置かない。
CREATE TABLE ticket_expenses (
  id          CHAR(36)     NOT NULL,
  ticket_id   CHAR(36)     NOT NULL,
  -- 費目名。例: '打ち上げ飲み会'
  title       VARCHAR(255) NOT NULL,
  paid_by     CHAR(36)     NOT NULL,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_ticket_expenses_ticket_id (ticket_id),
  KEY idx_ticket_expenses_paid_by (paid_by),
  CONSTRAINT fk_ticket_expenses_ticket_id FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_expenses_paid_by   FOREIGN KEY (paid_by)   REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 追加精算の対象者。amount は「この人が立替者に支払う金額（税込・円）」で、
-- 人によって負担額を変えられる（不参加者はそもそも行を作らない）。
-- 登録時点ではチケット参加者から選ぶが、後からチケット参加者を外しても
-- 精算記録として行は残す（FK はチケット参加者ではなく users を参照）。
-- settled_at は ticket_participants と同じ流儀（NULL = 未精算）。
CREATE TABLE ticket_expense_participants (
  expense_id   CHAR(36)    NOT NULL,
  user_id     CHAR(36)    NOT NULL,
  amount      INT         NOT NULL,
  settled_at  DATETIME(6) NULL DEFAULT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (expense_id, user_id),
  KEY idx_ticket_expense_participants_user_id (user_id),
  CONSTRAINT fk_ticket_expense_participants_expense_id FOREIGN KEY (expense_id) REFERENCES ticket_expenses(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_expense_participants_user_id   FOREIGN KEY (user_id)   REFERENCES users(id)   ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
