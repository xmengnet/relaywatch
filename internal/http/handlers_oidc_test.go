package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

// ---------- OIDC Provider 管理（「网站管理」） ----------

// 管理页曾误用登录页的匿名列表 GET /auth/providers（只回 name/icon_url 且不含停用项），
// 列表项没有 id，editProvider/removeProvider 的 if (!p.id) return 静默失效。
// 回归：admin 全量列表必须含 id/enabled 与停用项；issuer 尾斜杠必须规范化。
func TestOIDCProviderAdminListAndIssuerNormalize(t *testing.T) {
	env := newMultiUserEnvWithOIDC(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	env.mustUser(t, "user1", "user", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	uC, _ := env.loginAs(t, "user1", "password123")

	// 普通用户访问 admin 全量列表 → 403
	if rec := env.do(t, "GET", "/api/v1/auth/providers/all", uC, "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("user GET /auth/providers/all = %d, want 403", rec.Code)
	}
	// 匿名访问 admin 全量列表 → 401
	if rec := env.do(t, "GET", "/api/v1/auth/providers/all", "", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET /auth/providers/all = %d, want 401", rec.Code)
	}

	// 创建两个 provider：issuer 带首尾空格与尾斜杠应被规范化；第二个停用
	rec := env.do(t, "POST", "/api/v1/auth/providers", adminC, adminCSRF, map[string]any{
		"name": "casdoor", "issuer": " https://door.example/ ", "client_id": "cid", "client_secret": "sec",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create provider = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, "POST", "/api/v1/auth/providers", adminC, adminCSRF, map[string]any{
		"name": "off", "issuer": "https://off.example/", "client_id": "cid2", "client_secret": "sec2", "enabled": false,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled provider = %d %s", rec.Code, rec.Body.String())
	}

	// 匿名列表：只回启用项，且不含 id（登录页脱敏语义不变）
	rec = env.do(t, "GET", "/api/v1/auth/providers", "", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public list = %d", rec.Code)
	}
	var pub struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if len(pub.Items) != 1 {
		t.Fatalf("public list items = %d, want 1（仅启用项）", len(pub.Items))
	}
	if _, ok := pub.Items[0]["id"]; ok {
		t.Errorf("public list should not expose id: %v", pub.Items[0])
	}
	if pub.Items[0]["name"] != "casdoor" {
		t.Errorf("public list name = %v, want casdoor", pub.Items[0]["name"])
	}

	// admin 全量列表：含 id/enabled，停用项也在
	rec = env.do(t, "GET", "/api/v1/auth/providers/all", adminC, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list = %d %s", rec.Code, rec.Body.String())
	}
	var all struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("admin list items = %d, want 2（含停用项）", len(all.Items))
	}
	for _, it := range all.Items {
		if _, ok := it["id"].(float64); !ok {
			t.Errorf("admin list item missing numeric id: %v", it)
		}
		if _, ok := it["enabled"].(bool); !ok {
			t.Errorf("admin list item missing enabled: %v", it)
		}
		if it["name"] == "casdoor" && it["issuer"] != "https://door.example" {
			t.Errorf("issuer not normalized, got %v", it["issuer"])
		}
	}

	// PATCH 编辑：尾斜杠同样规范化；client_secret 缺省 = 不修改
	idPath := "/api/v1/auth/providers/" + strconv.FormatInt(created.ID, 10)
	rec = env.do(t, "PATCH", idPath, adminC, adminCSRF, map[string]any{
		"name": "casdoor", "issuer": "https://door.example/", "client_id": "cid",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch provider = %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, "GET", idPath, adminC, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("getOne = %d", rec.Code)
	}
	var one map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one["issuer"] != "https://door.example" {
		t.Errorf("getOne issuer = %v, want trimmed", one["issuer"])
	}
}
