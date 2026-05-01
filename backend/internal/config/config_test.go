package config

import "testing"

func TestEnv(t *testing.T) {
	const key = "NAZOBU_TEST_ENV_VALUE"

	t.Run("環境変数が空なら default が返る", func(t *testing.T) {
		t.Setenv(key, "")
		if got := env(key, "default"); got != "default" {
			t.Errorf("env = %q, want %q", got, "default")
		}
	})

	t.Run("環境変数に値があればその値が返る", func(t *testing.T) {
		t.Setenv(key, "value")
		if got := env(key, "default"); got != "value" {
			t.Errorf("env = %q, want %q", got, "value")
		}
	})
}
