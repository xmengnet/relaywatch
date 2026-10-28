package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"relaywatch/internal/auth"
)

func TestLoginRateLimit(t *testing.T) {
	lim := auth.NewAttemptLimiter(2, time.Minute, time.Minute)
	env := newMultiUserEnvWithLimiter(t, lim)
	env.mustUser(t, "alice-rl", "user", "TestPassw0rd!")

	attempt := func(password string) int {
		t.Helper()
		rec := env.do(t, "POST", "/api/v1/auth/login", "", "", map[string]any{
			"username": "alice-rl", "password": password,
		})
		return rec.Code
	}

	if code := attempt("wrong-1"); code != http.StatusUnauthorized {
		t.Fatalf("first failure = %d, want 401", code)
	}
	if code := attempt("wrong-2"); code != http.StatusUnauthorized {
		t.Fatalf("second failure = %d, want 401", code)
	}
	if code := attempt("TestPassw0rd!"); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt = %d, want 429", code)
	}
}

func TestLoginSuccessResetsLimiter(t *testing.T) {
	lim := auth.NewAttemptLimiter(2, time.Minute, time.Minute)
	env := newMultiUserEnvWithLimiter(t, lim)
	env.mustUser(t, "bob-rl", "user", "TestPassw0rd!")

	attempt := func(password string) int {
		t.Helper()
		rec := env.do(t, "POST", "/api/v1/auth/login", "", "", map[string]any{
			"username": "bob-rl", "password": password,
		})
		return rec.Code
	}

	if code := attempt("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("failure = %d, want 401", code)
	}
	if code := attempt("TestPassw0rd!"); code != http.StatusOK {
		t.Fatalf("successful login = %d, want 200", code)
	}
	// 成功登录应清空失败计数：还能再失败两次
	if code := attempt("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("failure after reset = %d, want 401", code)
	}
	if code := attempt("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("second failure after reset = %d, want 401", code)
	}
	if code := attempt("wrong"); code != http.StatusTooManyRequests {
		t.Fatalf("blocked after reset = %d, want 429", code)
	}
}

func TestRegisterSendCodeRateLimit(t *testing.T) {
	lim := auth.NewAttemptLimiter(1, time.Minute, time.Minute)
	env := newMultiUserEnvWithLimiter(t, lim)
	env.mustUser(t, "admin-rl", "admin", "TestPassw0rd!")
	cookie, csrf := env.loginAs(t, "admin-rl", "TestPassw0rd!")

	rec := env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{
		"registration_enabled":       true,
		"registration_require_email": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable registration = %d %s", rec.Code, rec.Body.String())
	}

	send := func() int {
		t.Helper()
		rec := env.do(t, "POST", "/api/v1/auth/register/send-code", "", "", map[string]any{"email": "new@example.com"})
		return rec.Code
	}
	// 未配置全局邮件 → 第一次 503，但请求已计数
	if code := send(); code != http.StatusServiceUnavailable {
		t.Fatalf("first send = %d, want 503", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("second send = %d, want 429", code)
	}
}

func TestClientIPUsesRemoteAddrByDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.9:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := clientIP(r); got != "192.0.2.9" {
		t.Fatalf("clientIP = %q, want socket address 192.0.2.9", got)
	}
}

// Docker/部署侧的 Secure cookie 逻辑：base_url 为 https 时强制 Secure。
func TestSecureCookieFromBaseURL(t *testing.T) {
	secure := httptest.NewRequest("GET", "/", nil)
	if secureCookie(secure, true) != true {
		t.Fatal("forced secure should be true")
	}
	plain := httptest.NewRequest("GET", "/", nil)
	if secureCookie(plain, false) != false {
		t.Fatal("plain http without force should not be secure")
	}
	tlsReq := httptest.NewRequest("GET", "/", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if secureCookie(tlsReq, false) != true {
		t.Fatal("direct TLS should be secure")
	}
}
