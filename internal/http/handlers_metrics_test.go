package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

func newMetricsTestServer(t *testing.T, cfg MetricsConfig) (*Server, store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st, time.Hour)
	srv := &Server{
		Auth:       svc,
		Store:      st,
		MetricsCfg: cfg,
	}
	return srv, st
}

func TestMetricsBasicAuth_Success(t *testing.T) {
	srv, _ := newMetricsTestServer(t, MetricsConfig{
		Enabled:  true,
		Username: "prom",
		Password: "secret-password",
	})
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("prom", "secret-password")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "relaywatch_monitors_total") {
		t.Errorf("expected metrics body to contain relaywatch_monitors_total, got: %s", body)
	}
}

func TestMetricsBasicAuth_Unauthorized(t *testing.T) {
	srv, _ := newMetricsTestServer(t, MetricsConfig{
		Enabled:  true,
		Username: "prom",
		Password: "secret-password",
	})
	h := srv.Handler()

	// 1. 无认证
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "Basic") {
		t.Errorf("expected WWW-Authenticate header, got: %q", rec.Header().Get("WWW-Authenticate"))
	}

	// 2. 错误密码
	req2 := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req2.SetBasicAuth("prom", "wrong-password")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec2.Code)
	}
}

func TestMetricsBasicAuth_WebSessionIgnored(t *testing.T) {
	srv, st := newMetricsTestServer(t, MetricsConfig{
		Enabled:  true,
		Username: "prom",
		Password: "secret-password",
	})
	h := srv.Handler()

	// 创建已登录用户
	ctx := context.Background()
	hash, _ := auth.HashPassword("user-pass")
	user := &store.User{Username: "viewer", PasswordHash: hash, Role: "viewer"}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	_, sess, err := srv.Auth.Login(ctx, "viewer", "user-pass", "ua", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	// 请求时仅携带普通 Web 登录 Cookie，未带 Basic Auth
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.AddCookie(&http.Cookie{Name: "relaywatch_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// 应该被拦截，不再放行 Web 登录会话
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 because web cookie is not accepted for metrics, got %d", rec.Code)
	}
}

func TestMetricsBasicAuth_Disabled(t *testing.T) {
	srv, _ := newMetricsTestServer(t, MetricsConfig{
		Enabled:  false,
		Username: "prom",
		Password: "secret-password",
	})
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("prom", "secret-password")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when metrics disabled, got %d", rec.Code)
	}
}

func TestMetricsBasicAuth_NoPasswordConfigured(t *testing.T) {
	srv, _ := newMetricsTestServer(t, MetricsConfig{
		Enabled:  true,
		Username: "prom",
		Password: "", // 未配置密码
	})
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.SetBasicAuth("prom", "")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when password not configured, got %d", rec.Code)
	}
}
