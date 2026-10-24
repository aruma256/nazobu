package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLog(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/nazobu.v1.TicketService/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	h := withAccessLog(mux)

	serve := func(target string) map[string]any {
		t.Helper()
		output.Reset()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
		if output.Len() == 0 {
			return nil
		}
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatalf("invalid log: %v", err)
		}
		return record
	}

	if record := serve("/healthz"); record != nil {
		t.Fatalf("healthz should not be logged: %v", record)
	}

	record := serve("/oauth/authorize?code=secret&state=secret")
	if strings.Contains(output.String(), "secret") {
		t.Fatal("query leaked")
	}
	if record["route"] != "GET /oauth/authorize" || record["path"] != "/oauth/authorize" || record["status"] != float64(http.StatusFound) {
		t.Fatalf("unexpected record: %v", record)
	}

	// 未知のプロシージャや未登録パスは任意文字列なのでパスを出さない。
	for _, target := range []string{"/nazobu.v1.TicketService/alice", "/alice"} {
		record := serve(target)
		if strings.Contains(output.String(), "alice") {
			t.Fatalf("path leaked: %s", target)
		}
		if record["status"] != float64(http.StatusNotFound) {
			t.Fatalf("unexpected record: %v", record)
		}
	}
}
