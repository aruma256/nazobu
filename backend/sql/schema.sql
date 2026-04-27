-- 謎部 DB スキーマ（SSOT）
-- ここに DDL を記述すると、起動時に sqldef が現状 DB との差分を計算して適用する。
-- 文字コード: utf8mb4 / エンジン: InnoDB / ID 型: VARCHAR(26) ULID

-- 内部ユーザー。IdP 非依存の anchor。
-- IdP ごとの identity は *_identities テーブルに分離する（将来 Discord 以外の
-- IdP に対応する場合、google_identities 等を横並びで追加する想定）。
CREATE TABLE users (
  id          VARCHAR(26) NOT NULL,
  created_at  DATETIME(6) NOT NULL,
  updated_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- Discord IdP の identity。1 user につき 0 or 1 件。
CREATE TABLE discord_identities (
  user_id         VARCHAR(26)  NOT NULL,
  discord_user_id VARCHAR(32)  NOT NULL,
  username        VARCHAR(255) NOT NULL,
  global_name     VARCHAR(255) NULL,
  avatar          VARCHAR(64)  NULL,
  created_at      DATETIME(6)  NOT NULL,
  updated_at      DATETIME(6)  NOT NULL,
  PRIMARY KEY (user_id),
  UNIQUE KEY uq_discord_identities_discord_user_id (discord_user_id),
  CONSTRAINT fk_discord_identities_user_id FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
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
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- チケット 1 枚。グループチケットなら 1 枚で複数人が参加でき、price を割り勘する。
-- price は税込・円。purchased_by が立て替え、ticket_participants が割り勘元。
CREATE TABLE tickets (
  id            VARCHAR(26) NOT NULL,
  event_id      VARCHAR(26) NOT NULL,
  attended_on   DATE        NOT NULL,
  price         INT         NOT NULL,
  purchased_by  VARCHAR(26) NOT NULL,
  created_at    DATETIME(6) NOT NULL,
  updated_at    DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  KEY idx_tickets_event_id (event_id),
  KEY idx_tickets_attended_on (attended_on),
  KEY idx_tickets_purchased_by (purchased_by),
  CONSTRAINT fk_tickets_event_id     FOREIGN KEY (event_id)     REFERENCES events(id) ON DELETE CASCADE,
  CONSTRAINT fk_tickets_purchased_by FOREIGN KEY (purchased_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ticket と参加者の M:N。誰がどのチケットで参加したか。
CREATE TABLE ticket_participants (
  ticket_id   VARCHAR(26) NOT NULL,
  user_id     VARCHAR(26) NOT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (ticket_id, user_id),
  KEY idx_ticket_participants_user_id (user_id),
  CONSTRAINT fk_ticket_participants_ticket_id FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CONSTRAINT fk_ticket_participants_user_id   FOREIGN KEY (user_id)   REFERENCES users(id)   ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
