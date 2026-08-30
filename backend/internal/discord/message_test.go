package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendMessage(t *testing.T) {
	t.Run("bot 認証で content と許可したメンションだけを投稿する", func(t *testing.T) {
		var got createMessageRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/channels/channel-1/messages" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bot test-token" {
				t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
			}
			if r.Header.Get("X-Audit-Log-Reason") != "" {
				t.Errorf("メッセージ投稿に audit log reason が付与された: %q", r.Header.Get("X-Audit-Log-Reason"))
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("request JSON の decode に失敗: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"message-1"}`))
		}))
		defer server.Close()

		client := &Client{httpClient: server.Client(), apiBaseURL: server.URL, botToken: "test-token"}
		if err := client.SendMessage(context.Background(), " channel-1 ", "明日の公演", []string{"111", "222"}); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		if got.Content != "明日の公演" {
			t.Errorf("content = %q", got.Content)
		}
		if got.AllowedMentions.Parse == nil || len(got.AllowedMentions.Parse) != 0 {
			t.Errorf("allowed_mentions.parse = %#v, want 空配列", got.AllowedMentions.Parse)
		}
		if len(got.AllowedMentions.Users) != 2 ||
			got.AllowedMentions.Users[0] != "111" || got.AllowedMentions.Users[1] != "222" {
			t.Errorf("allowed_mentions.users = %v, want [111 222]", got.AllowedMentions.Users)
		}
	})

	t.Run("異常応答は status と本文を含むエラーにする", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer server.Close()

		client := &Client{httpClient: server.Client(), apiBaseURL: server.URL, botToken: "test-token"}
		err := client.SendMessage(context.Background(), "channel-1", "x", nil)
		if err == nil {
			t.Fatal("err = nil, want error")
		}
		if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
			t.Errorf("エラーに status と本文が含まれていない: %v", err)
		}
	})

	t.Run("成功応答に message id が無ければエラーにする", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		client := &Client{httpClient: server.Client(), apiBaseURL: server.URL, botToken: "test-token"}
		err := client.SendMessage(context.Background(), "channel-1", "x", nil)
		if err == nil || !strings.Contains(err.Error(), "id が無い") {
			t.Errorf("err = %v, want id 欠落エラー", err)
		}
	})
}

func TestMessageConfigured(t *testing.T) {
	configured := NewClient(http.DefaultClient, "token", "", "")
	if !configured.MessageConfigured("channel") {
		t.Error("bot token と channel ID があれば true にならない")
	}
	if configured.MessageConfigured("") {
		t.Error("channel ID が空でも true になった")
	}
	unconfigured := NewClient(http.DefaultClient, "", "guild", "category")
	if unconfigured.MessageConfigured("channel") {
		t.Error("bot token が空でも true になった")
	}
}
