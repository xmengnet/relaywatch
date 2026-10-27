package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// captureLog 临时把 slog 默认输出重定向到缓冲区（改动全局状态，测试须串行）。
func captureLog(t *testing.T, run func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	run()
	return buf.String()
}

func serveWithLog(t *testing.T, h http.Handler, r *http.Request) string {
	t.Helper()
	return captureLog(t, func() {
		h.ServeHTTP(httptest.NewRecorder(), r)
	})
}

func TestAccessLogLevels(t *testing.T) {
	cases := []struct {
		name   string
		status int
		level  string
	}{
		{"2xx info", 200, "INFO"},
		{"4xx warn", 404, "WARN"},
		{"5xx error", 500, "ERROR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := serveWithLog(t, accessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
			})), httptest.NewRequest("GET", "/api/v1/monitors", nil))
			if !strings.Contains(out, "level="+c.level) || !strings.Contains(out, "status="+strconv.Itoa(c.status)) {
				t.Fatalf("want level=%s status=%d, got: %s", c.level, c.status, out)
			}
		})
	}
}

func TestAccessLogSkipsNoise(t *testing.T) {
	for _, path := range []string{"/healthz", "/metrics", "/assets/app.js"} {
		out := serveWithLog(t, accessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
		})), httptest.NewRequest("GET", path, nil))
		if out != "" {
			t.Errorf("%s should not be logged, got: %s", path, out)
		}
	}
}

func TestAccessLogSSEOpensOnce(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/events", nil)
	r.Header.Set("Accept", "text/event-stream")
	h := accessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Millisecond) // 模拟长连接期间
	}))
	out := serveWithLog(t, h, r)
	if !strings.Contains(out, "msg=\"sse open\"") {
		t.Fatalf("want sse open, got: %s", out)
	}
}

func TestAccessLogRecordsUser(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/monitors", nil)
	u := &store.User{ID: 1, Username: "alice", Role: "admin"}
	r = r.WithContext(auth.WithUser(r.Context(), u, nil))
	out := serveWithLog(t, accessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})), r)
	if !strings.Contains(out, "user=alice") {
		t.Fatalf("want user=alice, got: %s", out)
	}
}
