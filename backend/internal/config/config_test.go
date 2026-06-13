package config

import "testing"

func TestEnv(t *testing.T) {
	// 未設定 / 空文字は os.Getenv からは区別できず、どちらもデフォルト値に倒れる。
	t.Run("空文字（= 未設定相当）ならデフォルト値を返す", func(t *testing.T) {
		t.Setenv("NAZOBU_TEST_UNSET", "")
		if got := env("NAZOBU_TEST_UNSET", "default"); got != "default" {
			t.Errorf("env = %q, want %q", got, "default")
		}
	})

	t.Run("環境変数が設定済みならその値を返す", func(t *testing.T) {
		t.Setenv("NAZOBU_TEST_SET", "actual")
		if got := env("NAZOBU_TEST_SET", "default"); got != "actual" {
			t.Errorf("env = %q, want %q", got, "actual")
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	// 各 key を空にして、デフォルト値が反映されることを確認する。
	keys := []string{
		"HTTP_ADDR", "FRONTEND_URL", "SCHEMA_PATH", "COOKIE_SECURE",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"DISCORD_CLIENT_ID", "DISCORD_CLIENT_SECRET", "DISCORD_REDIRECT_URL",
		"DISCORD_WEBHOOK_URL",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	cfg := Load()
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.FrontendURL != "http://localhost:3000" {
		t.Errorf("FrontendURL = %q", cfg.FrontendURL)
	}
	if cfg.SchemaPath != "sql/schema.sql" {
		t.Errorf("SchemaPath = %q", cfg.SchemaPath)
	}
	if cfg.CookieSecure {
		t.Errorf("CookieSecure はデフォルト false")
	}
	if cfg.DB.Host != "mysql" {
		t.Errorf("DB.Host = %q", cfg.DB.Host)
	}
	if cfg.DB.Port != "3306" {
		t.Errorf("DB.Port = %q", cfg.DB.Port)
	}
	if cfg.DB.User != "nazobu" {
		t.Errorf("DB.User = %q", cfg.DB.User)
	}
	if cfg.DB.Password != "" {
		t.Errorf("DB.Password はデフォルト空")
	}
	if cfg.DB.Name != "nazobu" {
		t.Errorf("DB.Name = %q", cfg.DB.Name)
	}
	if cfg.Discord.ClientID != "" {
		t.Errorf("Discord.ClientID はデフォルト空")
	}
	if cfg.Discord.ClientSecret != "" {
		t.Errorf("Discord.ClientSecret はデフォルト空")
	}
	if cfg.Discord.RedirectURL != "http://localhost:3000/auth/discord/callback" {
		t.Errorf("Discord.RedirectURL = %q", cfg.Discord.RedirectURL)
	}
	if cfg.Discord.WebhookURL != "" {
		t.Errorf("Discord.WebhookURL はデフォルト空")
	}
}

func TestLoadOverridden(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("FRONTEND_URL", "https://example.com")
	t.Setenv("SCHEMA_PATH", "/etc/schema.sql")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_PORT", "13306")
	t.Setenv("DB_USER", "alice")
	t.Setenv("DB_PASSWORD", "s3cret")
	t.Setenv("DB_NAME", "prod")
	t.Setenv("DISCORD_CLIENT_ID", "cid")
	t.Setenv("DISCORD_CLIENT_SECRET", "csecret")
	t.Setenv("DISCORD_REDIRECT_URL", "https://example.com/cb")
	t.Setenv("DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/123/abc")

	cfg := Load()
	if cfg.HTTPAddr != ":9000" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.FrontendURL != "https://example.com" {
		t.Errorf("FrontendURL = %q", cfg.FrontendURL)
	}
	if cfg.SchemaPath != "/etc/schema.sql" {
		t.Errorf("SchemaPath = %q", cfg.SchemaPath)
	}
	if !cfg.CookieSecure {
		t.Errorf("CookieSecure = false, want true")
	}
	if cfg.DB.Host != "db.internal" || cfg.DB.Port != "13306" || cfg.DB.User != "alice" ||
		cfg.DB.Password != "s3cret" || cfg.DB.Name != "prod" {
		t.Errorf("DB = %+v", cfg.DB)
	}
	if cfg.Discord.ClientID != "cid" || cfg.Discord.ClientSecret != "csecret" ||
		cfg.Discord.RedirectURL != "https://example.com/cb" ||
		cfg.Discord.WebhookURL != "https://discord.com/api/webhooks/123/abc" {
		t.Errorf("Discord = %+v", cfg.Discord)
	}
}

func TestCookieSecureOnlyExactlyTrue(t *testing.T) {
	// "true" 以外（"1", "TRUE", "yes" 等）は false に倒れる仕様の固定。
	cases := []struct {
		in   string
		want bool
	}{
		{"true", true},
		{"TRUE", false},
		{"1", false},
		{"yes", false},
		{"false", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Setenv("COOKIE_SECURE", c.in)
			if got := Load().CookieSecure; got != c.want {
				t.Errorf("CookieSecure=%v, want %v", got, c.want)
			}
		})
	}
}
