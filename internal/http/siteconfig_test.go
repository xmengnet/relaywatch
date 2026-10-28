package httpapi

import (
	"context"
	"net/http"
	"testing"

	"relaywatch/internal/source"
)

// ---------- 站点配置（「网站管理」） ----------

func TestSiteConfigAuthAndPersistence(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	env.mustUser(t, "user1", "user", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	uC, uCSRF := env.loginAs(t, "user1", "password123")

	// 普通用户访问 → 403
	rec := env.do(t, "GET", "/api/v1/site-config", uC, uCSRF, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("user GET site-config = %d, want 403", rec.Code)
	}
	rec = env.do(t, "PUT", "/api/v1/site-config", uC, uCSRF, map[string]any{"mirrors": []any{}, "host_defaults": []any{}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("user PUT site-config = %d, want 403", rec.Code)
	}

	// admin 读默认（空）
	rec = env.do(t, "GET", "/api/v1/site-config", adminC, adminCSRF, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET site-config = %d", rec.Code)
	}

	// 写入镜像 + 站点默认值
	rec = env.do(t, "PUT", "/api/v1/site-config", adminC, adminCSRF, map[string]any{
		"mirrors": []any{map[string]any{"from": "https://api.github.com", "to": "https://gh-proxy.example/api.github.com"}},
		"host_defaults": []any{
			map[string]any{"source_type": "gitea_release", "host": "https://gitea.internal.example"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin PUT site-config: %d %s", rec.Code, rec.Body.String())
	}

	// 立即生效：RewriteURL 与 HostDefaultFor
	if got := source.RewriteURL("https://api.github.com/repos/o/r/releases"); got != "https://gh-proxy.example/api.github.com/repos/o/r/releases" {
		t.Errorf("RewriteURL = %q", got)
	}
	if got := source.RewriteURL("https://gitlab.com/api/v4/x"); got != "https://gitlab.com/api/v4/x" {
		t.Errorf("non-matching URL should be untouched, got %q", got)
	}
	if got := source.HostDefaultFor("gitea_release"); got != "https://gitea.internal.example" {
		t.Errorf("HostDefaultFor = %q", got)
	}

	// 重启恢复：重新 LoadSiteConfig 应从 settings 加载
	source.SetSiteConfig(nil, nil)
	LoadSiteConfig(context.Background(), env.st)
	if got := source.HostDefaultFor("gitea_release"); got != "https://gitea.internal.example" {
		t.Errorf("after reload HostDefaultFor = %q", got)
	}

	// 非法镜像 → 400
	rec = env.do(t, "PUT", "/api/v1/site-config", adminC, adminCSRF, map[string]any{
		"mirrors":       []any{map[string]any{"from": "not-a-url", "to": "https://x.example"}},
		"host_defaults": []any{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid mirror = %d, want 400", rec.Code)
	}
}

// ApplyHostDefaults 回填行为：host 空才填，已有值不动。
func TestApplyHostDefaults(t *testing.T) {
	source.SetSiteConfig(nil, []source.HostDefault{{SourceType: "gitea_release", Host: "https://gitea.def.example/"}})
	defer source.SetSiteConfig(nil, nil)

	cfg := map[string]any{"repo": "o/r"}
	got := source.ApplyHostDefaults("gitea_release", cfg)
	if got["host"] != "https://gitea.def.example" {
		t.Errorf("empty host not filled: %v", got["host"])
	}

	cfg2 := map[string]any{"repo": "o/r", "host": "https://explicit.example"}
	got2 := source.ApplyHostDefaults("gitea_release", cfg2)
	if got2["host"] != "https://explicit.example" {
		t.Errorf("explicit host overwritten: %v", got2["host"])
	}

	// 无默认值的源类型不动
	cfg3 := map[string]any{"host": ""}
	got3 := source.ApplyHostDefaults("gitlab_release", cfg3)
	if got3["host"] != "" {
		t.Errorf("unexpected fill: %v", got3["host"])
	}
}
