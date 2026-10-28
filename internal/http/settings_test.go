package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// PUT /settings 是局部更新：只改传入字段，不能把其他设置重置为默认值。
func TestSettingsPartialUpdate(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-set", "admin", "TestPassw0rd!")
	cookie, csrf := env.loginAs(t, "admin-set", "TestPassw0rd!")

	read := func() settingsResponse {
		t.Helper()
		rec := env.do(t, "GET", "/api/v1/settings", cookie, csrf, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET settings = %d %s", rec.Code, rec.Body.String())
		}
		var got settingsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode settings: %v", err)
		}
		return got
	}

	// 只保存告警渠道
	rec := env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{
		"alert_channel_ids": []int64{3, 5},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT alert = %d %s", rec.Code, rec.Body.String())
	}
	got := read()
	if len(got.AlertChannelIDs) != 2 || got.AlertChannelIDs[0] != 3 || got.AlertChannelIDs[1] != 5 {
		t.Fatalf("alert ids = %v, want [3 5]", got.AlertChannelIDs)
	}

	// 只保存注册设置，不能清空刚保存的告警渠道
	rec = env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{
		"registration_enabled":       true,
		"registration_default_role":  "viewer",
		"registration_require_email": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT registration = %d %s", rec.Code, rec.Body.String())
	}
	got = read()
	if !got.RegistrationEnabled || got.RegistrationDefaultRole != "viewer" || !got.RegistrationRequireEmail {
		t.Fatalf("registration = %+v, want enabled/viewer/require_email", got)
	}
	if len(got.AlertChannelIDs) != 2 {
		t.Fatalf("alert ids after registration save = %v, want [3 5]", got.AlertChannelIDs)
	}

	// 只清空告警渠道，注册设置保持不变
	rec = env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{
		"alert_channel_ids": []int64{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT alert empty = %d %s", rec.Code, rec.Body.String())
	}
	got = read()
	if len(got.AlertChannelIDs) != 0 {
		t.Fatalf("alert ids = %v, want empty", got.AlertChannelIDs)
	}
	if !got.RegistrationEnabled || got.RegistrationDefaultRole != "viewer" || !got.RegistrationRequireEmail {
		t.Fatalf("registration reset by alert save: %+v", got)
	}

	// 非法角色被拒，且不改动现有设置
	rec = env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{
		"registration_default_role": "admin",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT invalid role = %d, want 400", rec.Code)
	}
	got = read()
	if got.RegistrationDefaultRole != "viewer" {
		t.Fatalf("role changed after invalid update: %q", got.RegistrationDefaultRole)
	}
}

// 登录页需要知道注册是否强制邮箱验证码，才能决定是否渲染验证码输入框。
func TestAuthStatusExposesRequireEmail(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-status", "admin", "TestPassw0rd!")
	cookie, csrf := env.loginAs(t, "admin-status", "TestPassw0rd!")

	read := func() bool {
		t.Helper()
		rec := env.do(t, "GET", "/api/v1/auth/status", "", "", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET auth/status = %d", rec.Code)
		}
		var got struct {
			RequireEmail bool `json:"registration_require_email"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.RequireEmail
	}

	if read() {
		t.Fatal("require_email should default to false")
	}
	rec := env.do(t, "PUT", "/api/v1/settings", cookie, csrf, map[string]any{"registration_require_email": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable require_email = %d %s", rec.Code, rec.Body.String())
	}
	if !read() {
		t.Fatal("require_email should be true after update")
	}
}
