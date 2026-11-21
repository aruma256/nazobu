CREATE TABLE users (
  id            CHAR(36)  NOT NULL,
  display_name  VARCHAR(255) NOT NULL,
  avatar_url    VARCHAR(512) NULL,
  notifications_enabled TINYINT(1) NOT NULL DEFAULT 1,
  role          VARCHAR(16)  NOT NULL DEFAULT 'member',
  created_at    DATETIME(6)  NOT NULL,
  updated_at    DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT chk_users_role CHECK (role IN ('admin', 'member'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_identities (
  user_id     CHAR(36)  NOT NULL,
  provider    VARCHAR(32)  NOT NULL,
  subject     VARCHAR(255) NOT NULL,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (provider, subject),
  KEY idx_user_identities_user_id (user_id),
  CONSTRAINT fk_user_identities_user_id FOREIGN KEY (user_id) REFERENCES users(id),
  -- ※ sqldef + MySQL では 1 要素の IN (...) に既知のバグがあるため = で書く。
  CONSTRAINT chk_user_identities_provider CHECK (provider = 'discord')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sessions (
  id          CHAR(36) NOT NULL,
  user_id     CHAR(36) NOT NULL,
  token_hash  CHAR(64)    NOT NULL,
  expires_at  DATETIME(6) NOT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_sessions_token_hash (token_hash),
  KEY idx_sessions_user_id (user_id),
  CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE oauth_authorization_codes (
  id              CHAR(36)     NOT NULL,
  code_hash       CHAR(64)     NOT NULL,
  user_id         CHAR(36)     NOT NULL,
  client_id       VARCHAR(512) NOT NULL,
  redirect_uri    VARCHAR(512) NOT NULL,
  scope           VARCHAR(255) NOT NULL,
  code_challenge  VARCHAR(128) NOT NULL,
  expires_at      DATETIME(6)  NOT NULL,
  created_at      DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_oauth_authorization_codes_code_hash (code_hash),
  CONSTRAINT fk_oauth_authorization_codes_user_id FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

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
  CONSTRAINT fk_oauth_tokens_user_id FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE events (
  id          CHAR(36)  NOT NULL,
  title       VARCHAR(255) NOT NULL,
  url         VARCHAR(512) NOT NULL,
  catchphrase VARCHAR(255) NOT NULL DEFAULT '',
  image_url   VARCHAR(2048) NULL,
  doors_open_minutes_before     INT NULL,
  entry_deadline_minutes_before INT NULL,
  expected_duration_minutes     INT NOT NULL DEFAULT 120,
  discord_spoiler_channel_id VARCHAR(32) NULL,
  created_at  DATETIME(6)  NOT NULL,
  updated_at  DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_events_url (url),
  UNIQUE KEY uq_events_discord_spoiler_channel_id (discord_spoiler_channel_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tickets (
  id                CHAR(36)  NOT NULL,
  event_id          CHAR(36)  NOT NULL,
  start_at          DATETIME(6)  NOT NULL,
  meeting_at        DATETIME(6)  NULL,
  price_per_person  INT          NOT NULL,
  max_participants  INT          NOT NULL,
  unregistered_participants_count INT NOT NULL DEFAULT 0,
  purchased_by      CHAR(36)  NOT NULL,
  meeting_place     VARCHAR(255) NOT NULL,
  memo              VARCHAR(2000) NOT NULL DEFAULT '',
  day_before_notified_at  DATETIME(6) NULL DEFAULT NULL,
  meeting_notified_at     DATETIME(6) NULL DEFAULT NULL,
  created_at        DATETIME(6)  NOT NULL,
  updated_at        DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_tickets_event_id (event_id),
  KEY idx_tickets_start_at (start_at),
  KEY idx_tickets_purchased_by_start_at (purchased_by, start_at),
  CONSTRAINT fk_tickets_event_id     FOREIGN KEY (event_id)     REFERENCES events(id),
  CONSTRAINT fk_tickets_purchased_by FOREIGN KEY (purchased_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE ticket_participants (
  ticket_id   CHAR(36) NOT NULL,
  user_id     CHAR(36) NOT NULL,
  settled_at  DATETIME(6) NULL DEFAULT NULL,
  created_at  DATETIME(6) NOT NULL,
  PRIMARY KEY (ticket_id, user_id),
  KEY idx_ticket_participants_user_id (user_id),
  CONSTRAINT fk_ticket_participants_ticket_id FOREIGN KEY (ticket_id) REFERENCES tickets(id),
  CONSTRAINT fk_ticket_participants_user_id   FOREIGN KEY (user_id)   REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
