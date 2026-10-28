package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteInternalErrorHidesDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, errors.New("sqlite: no such table: secret_internal"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret_internal") {
		t.Fatalf("response leaked internal error: %s", body)
	}
	if !strings.Contains(body, `"code":"internal"`) {
		t.Fatalf("response missing internal code: %s", body)
	}
}

// 请求体超过上限直接 413，防止超大 body 占内存。
func TestBodyLimit(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-body", "admin", "TestPassw0rd!")
	cookie, csrf := env.loginAs(t, "admin-body", "TestPassw0rd!")

	// Content-Length 超限：直接拒绝
	big := strings.NewReader(`{"name":"` + strings.Repeat("a", 9<<20) + `"}`)
	req := httptest.NewRequest("POST", "/api/v1/monitors", big)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "relaywatch_session", Value: cookie})
	req.Header.Set("X-CSRF-Token", csrf)
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", rec.Code)
	}
}
