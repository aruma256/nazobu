package config

import "os"

type Config struct {
	HTTPAddr     string
	FrontendURL  string
	SchemaPath   string
	CookieSecure bool
	DB           DBConfig
	Discord      DiscordConfig
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type DiscordConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func Load() Config {
	return Config{
		HTTPAddr:     env("HTTP_ADDR", ":8080"),
		FrontendURL:  env("FRONTEND_URL", "http://localhost:3000"),
		SchemaPath:   env("SCHEMA_PATH", "sql/schema.sql"),
		CookieSecure: env("COOKIE_SECURE", "false") == "true",
		DB: DBConfig{
			Host:     env("DB_HOST", "mysql"),
			Port:     env("DB_PORT", "3306"),
			User:     env("DB_USER", "nazobu"),
			Password: env("DB_PASSWORD", ""),
			Name:     env("DB_NAME", "nazobu"),
		},
		Discord: DiscordConfig{
			ClientID:     env("DISCORD_CLIENT_ID", ""),
			ClientSecret: env("DISCORD_CLIENT_SECRET", ""),
			RedirectURL:  env("DISCORD_REDIRECT_URL", "http://localhost:3000/auth/discord/callback"),
		},
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
