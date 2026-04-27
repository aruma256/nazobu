package config

import "os"

type Config struct {
	HTTPAddr   string
	SchemaPath string
	DB         DBConfig
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

func Load() Config {
	return Config{
		HTTPAddr:   env("HTTP_ADDR", ":8080"),
		SchemaPath: env("SCHEMA_PATH", "sql/schema.sql"),
		DB: DBConfig{
			Host:     env("DB_HOST", "mysql"),
			Port:     env("DB_PORT", "3306"),
			User:     env("DB_USER", "nazobu"),
			Password: env("DB_PASSWORD", ""),
			Name:     env("DB_NAME", "nazobu"),
		},
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
