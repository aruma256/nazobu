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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/guilds/guild-1/channels" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bot test-token" {
			t.Errorf("Authorization header が不正")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("request JSON の decode に失敗: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"channel-1"}`))
	}))
	defer server.Close()

	client := &Client{
		httpClient: server.Client(),
		apiBaseURL: server.URL,
		botToken:   "test-token",
		guildID:    "guild-1",
		categoryID: "category-1",
	}
	channelID, err := client.CreateSpoilerChannel(context.Background(), "20260830-テスト公演", "event event-1", []string{"user-2", "user-1", "user-1"})
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
		{ID: "user-1", Type: 1, Allow: "1024", Deny: "0"},
		{ID: "user-2", Type: 1, Allow: "1024", Deny: "0"},
	}
	if !slices.Equal(got.PermissionOverwrites, wantOverwrites) {
		t.Errorf("permission_overwrites = %+v, want %+v", got.PermissionOverwrites, wantOverwrites)
	}
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
