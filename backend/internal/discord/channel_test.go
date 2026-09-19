package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestCreateSpoilerChannel(t *testing.T) {
	var got createChannelRequest
	botUserRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot test-token" {
			t.Errorf("Authorization header が不正")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/@me":
			botUserRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"bot-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/guilds/guild-1/channels":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("request JSON の decode に失敗: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"channel-1"}`))
		default:
			t.Errorf("予期しない request = %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		apiBaseURL: server.URL,
		botToken:   "test-token",
		guildID:    "guild-1",
		categoryID: "category-1",
	}
	channelID, err := client.CreateSpoilerChannel(context.Background(), "20260830-テスト公演", "event event-1", []string{"user-2", "bot-1", "user-1", "user-1"})
	if err != nil {
		t.Fatalf("CreateSpoilerChannel に失敗: %v", err)
	}
	if channelID != "channel-1" {
		t.Errorf("channelID = %q, want channel-1", channelID)
	}
	if got.Name != "20260830-テスト公演" || got.ParentID != "category-1" || got.Type != 0 {
		t.Errorf("create payload = %+v", got)
	}
	wantOverwrites := []permissionOverwrite{
		{ID: "guild-1", Type: 0, Allow: "0", Deny: "1024"},
		{ID: "bot-1", Type: 1, Allow: "1024", Deny: "0"},
		{ID: "user-1", Type: 1, Allow: "1024", Deny: "0"},
		{ID: "user-2", Type: 1, Allow: "1024", Deny: "0"},
	}
	if !slices.Equal(got.PermissionOverwrites, wantOverwrites) {
		t.Errorf("permission_overwrites = %+v, want %+v", got.PermissionOverwrites, wantOverwrites)
	}
	if botUserRequests != 1 {
		t.Errorf("bot user 取得回数 = %d, want 1", botUserRequests)
	}
}

func TestCurrentBotUserID(t *testing.T) {
	t.Run("成功した id をキャッシュする", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.Method != http.MethodGet || r.URL.Path != "/users/@me" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":" bot-1 "}`))
		}))
		defer server.Close()

		client := &Client{httpClient: server.Client(), apiBaseURL: server.URL, botToken: "test-token"}
		for range 2 {
			got, err := client.currentBotUserID(context.Background())
			if err != nil {
				t.Fatalf("currentBotUserID に失敗: %v", err)
			}
			if got != "bot-1" {
				t.Errorf("bot user id = %q, want bot-1", got)
			}
		}
		if requests != 1 {
			t.Errorf("request 数 = %d, want 1", requests)
		}
	})

	t.Run("id 欠落はキャッシュせず再試行する", func(t *testing.T) {
		requests := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.Header().Set("Content-Type", "application/json")
			if requests == 1 {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"bot-1"}`))
		}))
		defer server.Close()

		client := &Client{httpClient: server.Client(), apiBaseURL: server.URL, botToken: "test-token"}
		if _, err := client.currentBotUserID(context.Background()); err == nil {
			t.Fatal("id 欠落で err = nil")
		}
		got, err := client.currentBotUserID(context.Background())
		if err != nil {
			t.Fatalf("再試行に失敗: %v", err)
		}
		if got != "bot-1" || requests != 2 {
			t.Errorf("bot user id = %q, requests = %d", got, requests)
		}
	})
}

func TestGrantMembersViewPreservesExistingPermissions(t *testing.T) {
	putPayloads := map[string]permissionOverwrite{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/channels/channel-1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
              "id":"channel-1",
              "permission_overwrites":[
                {"id":"already","type":1,"allow":"1024","deny":"0"},
                {"id":"merge","type":1,"allow":"2048","deny":"1024"}
              ]
            }`))
		case r.Method == http.MethodPut:
			var payload permissionOverwrite
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("request JSON の decode に失敗: %v", err)
			}
			putPayloads[r.URL.Path] = payload
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("予期しない request = %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		apiBaseURL: server.URL,
		botToken:   "test-token",
		guildID:    "guild-1",
		categoryID: "category-1",
	}
	err := client.GrantMembersView(context.Background(), "channel-1", []string{"new", "merge", "already"})
	if err != nil {
		t.Fatalf("GrantMembersView に失敗: %v", err)
	}
	if len(putPayloads) != 2 {
		t.Fatalf("PUT 数 = %d, want 2: %+v", len(putPayloads), putPayloads)
	}
	if got := putPayloads["/channels/channel-1/permissions/merge"]; got.Allow != "3072" || got.Deny != "0" || got.Type != 1 {
		t.Errorf("merge payload = %+v", got)
	}
	if got := putPayloads["/channels/channel-1/permissions/new"]; got.Allow != "1024" || got.Deny != "0" || got.Type != 1 {
		t.Errorf("new payload = %+v", got)
	}
	if _, ok := putPayloads["/channels/channel-1/permissions/already"]; ok {
		t.Error("既に VIEW_CHANNEL を持つメンバーへ PUT している")
	}
}

func TestChannelURLRequiresConfiguration(t *testing.T) {
	configured := NewClient(http.DefaultClient, "token", "guild", "category")
	if got := configured.ChannelURL("channel"); got != "https://discord.com/channels/guild/channel" {
		t.Errorf("ChannelURL = %q", got)
	}
	unconfigured := NewClient(http.DefaultClient, "", "guild", "category")
	if got := unconfigured.ChannelURL("channel"); got != "" {
		t.Errorf("未設定時の ChannelURL = %q", got)
	}
}

func TestValidateSpoilerChannel(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"対象サーバーのテキスト", `{"id":"456","guild_id":"123","type":0}`, 200, false},
		{"別サーバー", `{"id":"456","guild_id":"999","type":0}`, 200, true},
		{"音声", `{"id":"456","guild_id":"123","type":2}`, 200, true},
		{"別チャンネル", `{"id":"789","guild_id":"123","type":0}`, 200, true},
		{"存在しない", `{}`, 404, true},
		{"権限なし", `{}`, 403, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/channels/456" {
					t.Errorf("予期しない操作: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()
			client := NewClient(ts.Client(), "token", "123", "category")
			client.apiBaseURL = ts.URL
			if err := client.ValidateSpoilerChannel(context.Background(), "456"); (err != nil) != tt.wantError {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
