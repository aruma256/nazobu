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
