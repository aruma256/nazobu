-- 謎部 DB スキーマ（SSOT）
-- ここに DDL を記述すると、起動時に sqldef が現状 DB との差分を計算して適用する。
-- 文字コード: utf8mb4 / エンジン: InnoDB / ID 型: VARCHAR(26) ULID

-- 内部ユーザー。IdP 非依存の anchor。
-- 表示用プロフィール（display_name / avatar_url）はログイン時に
-- 連携元 IdP の値で更新するキャッシュ。IdP に依存しない形で持たせる。
-- display_name は IdP 側で未設定のことがあるので、ログイン時に handle 名等を
-- フォールバックとして必ず埋めて NOT NULL を保つ。
CREATE TABLE users (
  id            VARCHAR(26)  NOT NULL,
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
  user_id     VARCHAR(26)  NOT NULL,
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
  id          VARCHAR(26) NOT NULL,
  user_id     VARCHAR(26) NOT NULL,
  -- token そのものではなく SHA-256 hex (64 chars) を保存する。漏洩時に raw token を復元できないように。
  token_hash  CHAR(64)    NOT NULL,
  expires_at  DATETIME(6) NOT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_sessions_token_hash (token_hash),
  KEY idx_sessions_user_id (user_id),
  CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 謎解きイベント（公演）。長期開催が一般的なので、開催日は event ではなく
-- ticket（実際に参加した日）側に持たせる。
CREATE TABLE events (
  id          VARCHAR(26)  NOT NULL,
  title       VARCHAR(255) NOT NULL,
  url         VARCHAR(512) NOT NULL,
  -- 開場時間が開演時刻（ticket.start_time）の何分前か。0 以上、NULL = 未設定。
  doors_open_minutes_before     INT NULL,
  -- 入場締切が開演時刻の何分前か。これを過ぎると参加できない。0 以上、NULL = 未設定。
  entry_deadline_minutes_before INT NULL,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- チケット 1 枚。グループチケットなら 1 枚で複数人が参加でき、参加者で割り勘する。
-- price_per_person は一人あたりの税込・円（割り勘済み）。purchased_by が立て替え、
-- ticket_participants が割り勘元。
CREATE TABLE tickets (
  id                VARCHAR(26)  NOT NULL,
  event_id          VARCHAR(26)  NOT NULL,
  attended_on       DATE         NOT NULL,
  price_per_person  INT          NOT NULL,
  -- このチケット 1 枚で参加できる最大人数（ticket_participants の最大紐づけ数）。
  max_participants  INT          NOT NULL,
  purchased_by      VARCHAR(26)  NOT NULL,
  -- 集合時刻（attended_on の JST 当日基準）と集合場所。集合時刻が決まっていないときは NULL。
  -- 集合場所は空文字を「未設定」として許容する。
  meeting_time      TIME         NULL,
  meeting_place     VARCHAR(255) NOT NULL,
  -- 公演の開演時刻（attended_on の JST 当日基準）。
  start_time        TIME         NOT NULL,
  created_at        DATETIME(6)  NOT NULL,
  updated_at        DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_tickets_event_id (event_id),
  KEY idx_tickets_attended_on (attended_on),
  KEY idx_tickets_purchased_by_attended_on (purchased_by, attended_on),
  CONSTRAINT fk_tickets_event_id     FOREIGN KEY (event_id)     REFERENCES events(id) ON DELETE CASCADE,
  CONSTRAINT fk_tickets_purchased_by FOREIGN KEY (purchased_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ticket と参加者の M:N。誰がどのチケットで参加したか。
-- settled_at は立て替え分を購入者へ精算した時刻。NULL = 未精算（デフォルト）、
-- 非 NULL = 精算済み。タイムスタンプを兼ねることで「いつ精算したか」も保持する。
CREATE TABLE ticket_participants (
  ticket_id   VARCHAR(26) NOT NULL,
  user_id     VARCHAR(26) NOT NULL,
  settled_at  DATETIME(6) NULL DEFAULT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (ticket_id, user_id),
  KEY idx_ticket_participants_user_id (user_id),
  CONSTRAINT fk_ticket_participants_ticket_id FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_participants_user_id   FOREIGN KEY (user_id)   REFERENCES users(id)   ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
