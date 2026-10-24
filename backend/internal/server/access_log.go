package server

import (
	"log/slog"
	"net/http"
	"time"
)

// withAccessLog はリクエストごとにメソッド・ルート・ステータス・所要時間を記録する。
// クエリ文字列・ヘッダ・ボディには code / state / token などが含まれ得るため出力しない。
// パスも任意文字列になり得るので、mux のルートに一致し 404 でなかったときだけ出す。
func withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// liveness probe は頻度が高くノイズになるので記録しない。
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// ServeMux は一致したパターンを r.Pattern に書き込む。
		attrs := []any{
			"method", r.Method,
			"route", r.Pattern,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if r.Pattern != "" && rec.status != http.StatusNotFound {
			attrs = append(attrs, "path", r.URL.Path)
		}
		slog.InfoContext(r.Context(), "http request", attrs...)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// MCP の Streamable HTTP（SSE）が http.Flusher を型アサーションで使うため明示的に委譲する。
func (r *statusRecorder) Flush() {
	r.wroteHeader = true
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// http.ResponseController が元の ResponseWriter に辿り着けるようにする。
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
