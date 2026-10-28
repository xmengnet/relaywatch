package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// ---------- 测试装置 ----------

type multiUserEnv struct {
	st  store.Store
	svc *auth.Service
	h   http.Handler
}

func newMultiUserEnv(t *testing.T) *multiUserEnv {
	t.Helper()
	return newMultiUserEnvWithOptions(t, nil, nil)
}

func newMultiUserEnvWithBox(t *testing.T, box store.SecretBox) *multiUserEnv {
	t.Helper()
	return newMultiUserEnvWithOptions(t, box, nil)
}

func newMultiUserEnvWithLimiter(t *testing.T, limiter *auth.AttemptLimiter) *multiUserEnv {
	t.Helper()
	return newMultiUserEnvWithOptions(t, nil, limiter)
}

func newMultiUserEnvWithOptions(t *testing.T, box store.SecretBox, limiter *auth.AttemptLimiter) *multiUserEnv {
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
	if box != nil {
		st.SetSecretBox(box)
	}
	svc := auth.NewService(st, time.Hour)
	s := &Server{
		Auth:         svc,
		Store:        st,
		Notifiers:    notifier.NewDefaultRegistry(),
		SecretBox:    box,
		LoginLimiter: limiter,
	}
	return &multiUserEnv{st: st, svc: svc, h: s.Handler()}
}

// loginAs 以真实登录流程获得会话 Cookie，返回 Cookie 值与 CSRF token。
func (e *multiUserEnv) loginAs(t *testing.T, username, password string) (cookie, csrf string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", username, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "relaywatch_session" {
			cookie = c.Value
		}
	}
	var resp struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return cookie, resp.CSRFToken
}

func (e *multiUserEnv) mustUser(t *testing.T, name, role, password string) *store.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u := &store.User{Username: name, Role: role, PasswordHash: hash}
	if err := e.st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

// do 以某用户身份发起 JSON 请求（自动带 CSRF）。
func (e *multiUserEnv) do(t *testing.T, method, path, cookie, csrf string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "relaywatch_session", Value: cookie})
	}
	if csrf != "" && method != "GET" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// ---------- 跨用户隔离（404 语义） ----------

func TestMonitorCrossUser404(t *testing.T) {
	env := newMultiUserEnv(t)
	bob := env.mustUser(t, "bob", "user", "password123")
	env.mustUser(t, "alice", "user", "password123")
	env.mustUser(t, "admin-ac", "admin", "password123")

	// admin 建一个监控
	adminLoginC, adminToken := env.loginAs(t, "admin-ac", "password123")
	rec := env.do(t, "POST", "/api/v1/monitors", adminLoginC, adminToken, map[string]any{
		"name": "admin-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// bob 登录，访问 admin 的监控 → 404（非 403）
	bobC, bobCSRF := env.loginAs(t, "bob", "password123")
	rec = env.do(t, "GET", "/api/v1/monitors/"+itoa(created.ID), bobC, bobCSRF, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob GET admin monitor = %d, want 404", rec.Code)
	}
	rec = env.do(t, "DELETE", "/api/v1/monitors/"+itoa(created.ID), bobC, bobCSRF, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("bob DELETE admin monitor = %d, want 404", rec.Code)
	}

	// admin 可访问
	rec = env.do(t, "GET", "/api/v1/monitors/"+itoa(created.ID), adminLoginC, adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("admin GET own monitor = %d, want 200", rec.Code)
	}

	// bob 列表看不到 admin 的
	rec = env.do(t, "GET", "/api/v1/monitors", bobC, bobCSRF, nil)
	var listResp struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	if listResp.Total != 0 {
		t.Errorf("bob list total = %d, want 0", listResp.Total)
	}

	// bob 建监控归属 bob
	rec = env.do(t, "POST", "/api/v1/monitors", bobC, bobCSRF, map[string]any{
		"name": "bob-mon", "source_type": "manual", "source_config": map[string]any{"version": "2.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob create: %d %s", rec.Code, rec.Body.String())
	}
	var bobMon struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &bobMon)
	m, _ := env.st.GetMonitor(context.Background(), bobMon.ID)
	if m.OwnerID != bob.ID {
		t.Errorf("created monitor owner = %d, want bob(%d)", m.OwnerID, bob.ID)
	}
}

func TestChannelOwnership(t *testing.T) {
	env := newMultiUserEnv(t)
	_ = env.mustUser(t, "alice", "user", "password123")
	bob := env.mustUser(t, "bob", "user", "password123")
	_ = env.mustUser(t, "admin-ac", "admin", "password123")

	bobC, bobCSRF := env.loginAs(t, "bob", "password123")
	// bob 建渠道归属 bob
	rec := env.do(t, "POST", "/api/v1/channels", bobC, bobCSRF, map[string]any{
		"name": "bob-ch", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob create channel: %d %s", rec.Code, rec.Body.String())
	}
	var ch struct {
		ID      int64  `json:"id"`
		OwnerID *int64 `json:"owner_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if ch.OwnerID == nil || *ch.OwnerID != bob.ID {
		t.Errorf("channel owner = %v, want bob(%d)", ch.OwnerID, bob.ID)
	}
	// bob 请求共享 → 普通用户仍归自己
	rec = env.do(t, "POST", "/api/v1/channels", bobC, bobCSRF, map[string]any{
		"name": "bob-shared", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook"}, "shared": true,
	})
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if ch.OwnerID == nil || *ch.OwnerID != bob.ID {
		t.Errorf("user shared channel owner = %v, want bob(%d)", ch.OwnerID, bob.ID)
	}
}

// ---------- 注册开关 ----------

func TestRegisterGate(t *testing.T) {
	env := newMultiUserEnv(t)

	// 默认关闭：404
	rec := env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("register while closed = %d, want 404", rec.Code)
	}

	// admin 开启注册（默认角色 user）
	env.mustUser(t, "admin-ac", "admin", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	rec = env.do(t, "PUT", "/api/v1/settings", adminC, adminCSRF, map[string]any{
		"registration_enabled": true, "registration_default_role": "user",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable registration: %d %s", rec.Code, rec.Body.String())
	}

	// 匿名注册成功并自动登录，角色为 user
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		User struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.User.Role != "user" {
		t.Errorf("registered role = %s, want user", resp.User.Role)
	}

	// 用户名冲突
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie", "password": "password123",
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate register = %d, want 409", rec.Code)
	}

	// 关闭后再注册 → 404
	rec = env.do(t, "PUT", "/api/v1/settings", adminC, adminCSRF, map[string]any{"registration_enabled": false})
	if rec.Code != http.StatusOK {
		t.Fatal("disable registration failed")
	}
	rec = env.do(t, "POST", "/api/v1/auth/register", "", "", map[string]any{
		"username": "newbie2", "password": "password123",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("register after disable = %d, want 404", rec.Code)
	}
}

// ---------- 用户管理：防自锁与权限 ----------

func TestUserManagementGuards(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	admin2 := env.mustUser(t, "admin2", "admin", "password123")
	user1 := env.mustUser(t, "user1", "user", "password123")

	// user 访问用户管理 → 403
	uC, uCSRF := env.loginAs(t, "user1", "password123")
	rec := env.do(t, "GET", "/api/v1/users", uC, uCSRF, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("user GET /users = %d, want 403", rec.Code)
	}

	// 禁用最后一个可用 admin → 409（admin2 先禁用，只剩自己）
	rec = env.do(t, "POST", "/api/v1/users/"+itoa(admin2.ID)+"/disable", adminC, adminCSRF, map[string]any{"disabled": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable admin2: %d %s", rec.Code, rec.Body.String())
	}
	// 禁用 admin2（两个 admin，允许）
	rec = env.do(t, "POST", "/api/v1/users/"+itoa(admin2.ID)+"/disable", adminC, adminCSRF, map[string]any{"disabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable admin2: %d %s", rec.Code, rec.Body.String())
	}
	// 现在只剩自己 → 禁用自己被拒
	rec = env.do(t, "POST", "/api/v1/users/"+itoa(userSelfID(t, env, "admin-ac"))+"/disable", adminC, adminCSRF, map[string]any{"disabled": true})
	if rec.Code != http.StatusConflict {
		t.Errorf("disable last admin = %d, want 409", rec.Code)
	}

	// 修改自己角色 → 400
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(userSelfID(t, env, "admin-ac")), adminC, adminCSRF, map[string]any{"role": "user"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("change own role = %d, want 400", rec.Code)
	}

	// 删除自己 → 400
	rec = env.do(t, "DELETE", "/api/v1/users/"+itoa(userSelfID(t, env, "admin-ac")), adminC, adminCSRF, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("delete self = %d, want 400", rec.Code)
	}

	// 禁用 user1 后其会话立即失效
	rec = env.do(t, "POST", "/api/v1/users/"+itoa(user1.ID)+"/disable", adminC, adminCSRF, map[string]any{"disabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("disable user1: %d", rec.Code)
	}
	rec = env.do(t, "GET", "/api/v1/monitors", uC, uCSRF, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("disabled user session should be revoked, got %d, want 401", rec.Code)
	}
}

// userSelfID 查用户 id。
func userSelfID(t *testing.T, env *multiUserEnv, name string) int64 {
	t.Helper()
	u, err := env.st.UserByUsername(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// ---------- 细粒度权限 ----------

func TestGranularPermissions(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	// viewer 默认无任何创建权限
	viewer := env.mustUser(t, "viewer1", "viewer", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")

	vC, vCSRF := env.loginAs(t, "viewer1", "password123")

	// viewer 建监控 → 403（细粒度，而非角色）
	rec := env.do(t, "POST", "/api/v1/monitors", vC, vCSRF, map[string]any{
		"name": "v-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer create monitor = %d, want 403", rec.Code)
	}

	// viewer 建渠道 → 403
	rec = env.do(t, "POST", "/api/v1/channels", vC, vCSRF, map[string]any{
		"name": "v-ch", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook"},
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer create channel = %d, want 403", rec.Code)
	}

	// 授予 monitor.create（显式权限数组）→ 建监控成功，建渠道仍 403
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(viewer.ID), adminC, adminCSRF, map[string]any{
		"permissions": []string{"monitor.create"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("grant perm: %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, "POST", "/api/v1/monitors", vC, vCSRF, map[string]any{
		"name": "v-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Errorf("granted create monitor = %d, want 201", rec.Code)
	}
	rec = env.do(t, "POST", "/api/v1/channels", vC, vCSRF, map[string]any{
		"name": "v-ch", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook"},
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("ungranted create channel = %d, want 403", rec.Code)
	}

	// 空数组 = 全部禁用：给 user 显式空权限后建监控 403
	env.mustUser(t, "locked", "user", "password123")
	lC, lCSRF := env.loginAs(t, "locked", "password123")
	rec = env.do(t, "POST", "/api/v1/monitors", lC, lCSRF, map[string]any{
		"name": "l-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("default user create should pass, got %d", rec.Code)
	}
	lockedID := userSelfID(t, env, "locked")
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(lockedID), adminC, adminCSRF, map[string]any{
		"permissions": []string{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set empty perms: %d", rec.Code)
	}
	rec = env.do(t, "POST", "/api/v1/monitors", lC, lCSRF, map[string]any{
		"name": "l-mon2", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("empty-perms create monitor = %d, want 403", rec.Code)
	}

	// 角色变更为 viewer 且未显式传 permissions → 重置为按角色默认（nil），建监控回 403
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(lockedID), adminC, adminCSRF, map[string]any{"role": "viewer"})
	if rec.Code != http.StatusOK {
		t.Fatalf("role change: %d", rec.Code)
	}
	rec = env.do(t, "POST", "/api/v1/monitors", lC, lCSRF, map[string]any{
		"name": "l-mon3", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("role-reset create monitor = %d, want 403", rec.Code)
	}

	// 导出权限：默认 user 可导出；显式空权限后 403
	rec = env.do(t, "GET", "/api/v1/export", lC, lCSRF, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("empty-perms export = %d, want 403", rec.Code)
	}
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(lockedID), adminC, adminCSRF, map[string]any{
		"permissions": []string{"export"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("grant export: %d", rec.Code)
	}
	rec = env.do(t, "GET", "/api/v1/export", lC, lCSRF, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("granted export = %d, want 200", rec.Code)
	}
	_ = viewer
}

func TestProfileSessions(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "alice", "user", "password123")

	// alice 登录两次（两个会话）
	c1, csrf1 := env.loginAs(t, "alice", "password123")
	c2, csrf2 := env.loginAs(t, "alice", "password123")
	if c1 == c2 {
		t.Fatal("two logins should create distinct sessions")
	}

	// 列表应有 2 条，其中一条标记 current
	rec := env.do(t, "GET", "/api/v1/profile/sessions", c1, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sessions list: %d", rec.Code)
	}
	var resp struct {
		Items []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 2 {
		t.Fatalf("sessions = %d, want 2", len(resp.Items))
	}
	currentCount := 0
	var otherID string
	for _, it := range resp.Items {
		if it.Current {
			currentCount++
		} else {
			otherID = it.ID
		}
	}
	if currentCount != 1 || otherID == "" {
		t.Fatalf("current count = %d, other = %q", currentCount, otherID)
	}

	// 用会话 1 吊销会话 2
	rec = env.do(t, "DELETE", "/api/v1/profile/sessions/"+otherID, c1, csrf1, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke other session: %d %s", rec.Code, rec.Body.String())
	}
	// 会话 2 立即失效
	rec = env.do(t, "GET", "/api/v1/monitors", c2, csrf2, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked session should be 401, got %d", rec.Code)
	}

	// 不能吊销不属于自己的会话（404 语义）
	env.mustUser(t, "bob", "user", "password123")
	c3, csrf3 := env.loginAs(t, "bob", "password123")
	rec = env.do(t, "DELETE", "/api/v1/profile/sessions/"+c1, c3, csrf3, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("revoke foreign session = %d, want 404", rec.Code)
	}
}

func itoa(n int64) string {
	return json.Number(fmtInt(n)).String()
}

func fmtInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
