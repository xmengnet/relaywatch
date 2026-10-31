package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"relaywatch/internal/store"
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

// ---------- OIDC 绑定流程（「个人资料」） ----------

// 假 IdP：只提供发现文档（BeginBind 仅需 discovery；换 token 发生在 CompleteBind，需真实 IdP，不在单测覆盖内）。
// issuer 动态取 r.Host，保证与 go-oidc 的严格比对一致。
func newFakeIdP(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 "http://" + r.Host,
			"authorization_endpoint": "http://" + r.Host + "/login/oauth/authorize",
			"token_endpoint":         "http://" + r.Host + "/api/login/oauth/access_token",
			"jwks_uri":               "http://" + r.Host + "/jwks",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOIDCBindRoutesAndUnbind(t *testing.T) {
	env := newMultiUserEnvWithOIDC(t)
	admin := env.mustUser(t, "admin-ac", "admin", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	idp := newFakeIdP(t)

	// 创建启用的 provider（issuer 指向假 IdP）
	rec := env.do(t, "POST", "/api/v1/auth/providers", adminC, adminCSRF, map[string]any{
		"name": "casdoor", "issuer": idp.URL, "client_id": "cid", "client_secret": "sec",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create provider = %d %s", rec.Code, rec.Body.String())
	}

	// 匿名发起绑定 → 302 回登录页
	rec = env.do(t, "GET", "/auth/oidc/casdoor/bind", "", "", nil)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Fatalf("anonymous bind = %d %q, want 302 /login", rec.Code, rec.Header().Get("Location"))
	}

	// 不存在的 provider → 400
	rec = env.do(t, "GET", "/auth/oidc/nope/bind", adminC, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bind unknown provider = %d, want 400", rec.Code)
	}

	// 已登录发起绑定 → 302 到 IdP 授权页，带 state 与 PKCE challenge
	rec = env.do(t, "GET", "/auth/oidc/casdoor/bind", adminC, "", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("bind begin = %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, idp.URL+"/login/oauth/authorize") ||
		!strings.Contains(loc, "state=") || !strings.Contains(loc, "code_challenge=") {
		t.Fatalf("bind redirect = %q, want IdP authorize URL with state+challenge", loc)
	}

	// profile 响应字段与解绑保护
	rec = env.do(t, "GET", "/api/v1/profile", adminC, "", nil)
	var prof struct {
		Oidc         bool      `json:"oidc"`
		OidcProvider string    `json:"oidc_provider"`
		HasPassword  bool      `json:"has_password"`
		User         *struct{} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
		t.Fatal(err)
	}
	if prof.Oidc || prof.OidcProvider != "" || !prof.HasPassword {
		t.Fatalf("profile = %+v, want unbound with password", prof)
	}
	if rec := env.do(t, "DELETE", "/api/v1/profile/oidc", adminC, adminCSRF, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unbind when unbound = %d, want 400", rec.Code)
	}

	// 直写 store 模拟绑定成功后的状态；解绑后回到未绑定
	if err := env.st.SetUserOIDC(context.Background(), admin.ID, "casdoor", "sub-admin"); err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, "GET", "/api/v1/profile", adminC, "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
		t.Fatal(err)
	}
	if !prof.Oidc || prof.OidcProvider != "casdoor" {
		t.Fatalf("profile after bind = %+v", prof)
	}
	if rec := env.do(t, "DELETE", "/api/v1/profile/oidc", adminC, adminCSRF, nil); rec.Code != http.StatusOK {
		t.Fatalf("unbind = %d %s, want 200", rec.Code, rec.Body.String())
	}
	rec = env.do(t, "GET", "/api/v1/profile", adminC, "", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
		t.Fatal(err)
	}
	if prof.Oidc || prof.OidcProvider != "" {
		t.Fatalf("profile after unbind = %+v", prof)
	}
}

// 纯 OIDC 账户（无本地密码）拒绝解绑，避免失去唯一登录方式。
func TestOIDCUnbindRequiresPassword(t *testing.T) {
	env := newMultiUserEnvWithOIDC(t)
	pure := &store.User{Username: "pureoidc", Role: "user", CreatedAt: time.Now()}
	if err := env.st.CreateUser(context.Background(), pure); err != nil {
		t.Fatal(err)
	}
	if err := env.st.SetUserOIDC(context.Background(), pure.ID, "casdoor", "sub-pure"); err != nil {
		t.Fatal(err)
	}
	// 直接造会话（该账户无密码，无法走登录流程）
	sess := &store.Session{ID: "pure-sess", UserID: pure.ID, CSRFToken: "pure-csrf", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := env.st.CreateSession(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if rec := env.do(t, "DELETE", "/api/v1/profile/oidc", "pure-sess", "pure-csrf", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("pure-oidc unbind = %d %s, want 400", rec.Code, rec.Body.String())
	}
}
