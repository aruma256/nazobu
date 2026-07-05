package reminder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordWebhookPost(t *testing.T) {
	t.Run("content と allowed_mentions を JSON で POST する", func(t *testing.T) {
		var gotMethod, gotContentType string
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotContentType = r.Header.Get("Content-Type")
			gotBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		d := &discordWebhook{url: srv.URL, client: srv.Client()}
		if err := d.post(context.Background(), "明日の公演", []string{"111", "222"}); err != nil {
			t.Fatalf("post: %v", err)
		}

		if gotMethod != http.MethodPost {
			t.Errorf("method = %q, want POST", gotMethod)
		}
		if gotContentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", gotContentType)
		}

		var payload struct {
			Content         string `json:"content"`
			AllowedMentions struct {
				Parse []string `json:"parse"`
				Users []string `json:"users"`
			} `json:"allowed_mentions"`
		}
		if err := json.Unmarshal(gotBody, &payload); err != nil {
			t.Fatalf("payload の JSON decode に失敗: %v (%s)", err, gotBody)
		}
		if payload.Content != "明日の公演" {
			t.Errorf("content = %q", payload.Content)
		}
		if len(payload.AllowedMentions.Users) != 2 ||
			payload.AllowedMentions.Users[0] != "111" || payload.AllowedMentions.Users[1] != "222" {
			t.Errorf("users = %v, want [111 222]", payload.AllowedMentions.Users)
		}
		// parse は null ではなく空配列 [] で送る必要がある（@everyone / ロールメンション抑止の要）。
		if !strings.Contains(string(gotBody), `"parse":[]`) {
			t.Errorf("parse が空配列で送られていない: %s", gotBody)
		}
	})

	t.Run("2xx 以外の応答はエラーにし status とエラー本文を含める", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer srv.Close()

		d := &discordWebhook{url: srv.URL, client: srv.Client()}
		err := d.post(context.Background(), "x", nil)
		if err == nil {
			t.Fatal("err = nil, want error")
		}
		if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
			t.Errorf("エラーに status と本文が含まれていない: %v", err)
		}
	})

	t.Run("接続失敗はエラー", func(t *testing.T) {
		// 閉じた server の URL を使い、接続エラーを起こす。
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()

		d := &discordWebhook{url: url, client: http.DefaultClient}
		if err := d.post(context.Background(), "x", nil); err == nil {
			t.Fatal("err = nil, want error")
		}
	})
}
