package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"relaywatch/internal/store"
)

// viewer（只读角色）不得增删改既有资源（docs/design.md §19.3/§19.6），
// 即使资源归属 viewer 本人——用户由 user 降级为 viewer 时归属保留。
func TestViewerWriteDenied(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	bob := env.mustUser(t, "bob-viewer", "user", "password123")

	bobC, bobCSRF := env.loginAs(t, "bob-viewer", "password123")
	// bob 以 user 身份建一条监控与一个渠道（归属自己）
	rec := env.do(t, "POST", "/api/v1/monitors", bobC, bobCSRF, map[string]any{
		"name": "bob-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob create monitor: %d %s", rec.Code, rec.Body.String())
	}
	var mon struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mon)

	rec = env.do(t, "POST", "/api/v1/channels", bobC, bobCSRF, map[string]any{
		"name": "bob-ch", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bob create channel: %d %s", rec.Code, rec.Body.String())
	}
	var ch struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)

	// 造一条已失败的投递记录，供 resend 校验
	dl := &store.Delivery{
		MonitorID: mon.ID, ChannelID: ch.ID,
		Status: store.DeliveryFailed, NextAttemptAt: ptrTime(time.Now()),
	}
	if err := env.st.AddDelivery(context.Background(), dl); err != nil {
		t.Fatal(err)
	}

	// admin 把 bob 降级为 viewer
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(bob.ID), adminC, adminCSRF, map[string]any{"role": "viewer"})
	if rec.Code != http.StatusOK {
		t.Fatalf("downgrade bob: %d %s", rec.Code, rec.Body.String())
	}

	// 读仍然可用
	for _, path := range []string{
		"/api/v1/monitors",
		"/api/v1/monitors/" + itoa(mon.ID),
		"/api/v1/monitors/" + itoa(mon.ID) + "/history",
		"/api/v1/monitors/" + itoa(mon.ID) + "/changes",
		"/api/v1/channels",
		"/api/v1/channels/" + itoa(ch.ID),
		"/api/v1/deliveries",
	} {
		if rec := env.do(t, "GET", path, bobC, bobCSRF, nil); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
		}
	}

	// 写全部 403
	writes := []struct {
		method, path string
		body         any
	}{
		{"PATCH", "/api/v1/monitors/" + itoa(mon.ID), map[string]any{
			"name": "renamed", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
		}},
		{"DELETE", "/api/v1/monitors/" + itoa(mon.ID), nil},
		{"POST", "/api/v1/monitors/batch", map[string]any{"action": "disable", "ids": []int64{mon.ID}}},
		{"POST", "/api/v1/monitors/" + itoa(mon.ID) + "/check", nil},
		{"POST", "/api/v1/monitors/" + itoa(mon.ID) + "/ack", nil},
		{"PATCH", "/api/v1/channels/" + itoa(ch.ID), map[string]any{
			"name": "renamed", "type": "webhook", "config": map[string]any{"url": "http://example.com/hook2"},
		}},
		{"DELETE", "/api/v1/channels/" + itoa(ch.ID), nil},
		{"POST", "/api/v1/channels/" + itoa(ch.ID) + "/test", nil},
		{"POST", "/api/v1/deliveries/" + itoa(dl.ID) + "/resend", nil},
	}
	for _, w := range writes {
		rec := env.do(t, w.method, w.path, bobC, bobCSRF, w.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403 (%s)", w.method, w.path, rec.Code, rec.Body.String())
		}
	}

	// 被拒的写没有落库：名称与启用状态不变
	got, err := env.st.GetMonitor(context.Background(), mon.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "bob-mon" || !got.Enabled {
		t.Errorf("monitor mutated by denied writes: name=%q enabled=%v", got.Name, got.Enabled)
	}

	// viewer 仍可管理自己的资料与 Token（§19.3「仅自己的资料与 Token」）
	if rec := env.do(t, "PATCH", "/api/v1/profile", bobC, bobCSRF, map[string]any{"display_name": "只读用户"}); rec.Code != http.StatusOK {
		t.Errorf("viewer update own profile = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, "POST", "/api/v1/tokens", bobC, bobCSRF, map[string]any{"name": "readonly-token"}); rec.Code != http.StatusCreated {
		t.Errorf("viewer create own token = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	// 升回 user 后写恢复：证明确实按角色判定
	rec = env.do(t, "PATCH", "/api/v1/users/"+itoa(bob.ID), adminC, adminCSRF, map[string]any{"role": "user"})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-promote bob: %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, "PATCH", "/api/v1/monitors/"+itoa(mon.ID), bobC, bobCSRF, map[string]any{
		"name": "renamed-by-user", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusOK {
		t.Errorf("user PATCH own monitor = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

// viewer 被显式授予附加操作权限（如 monitor.create）后仍不得改动既有资源：
// Can 管附加操作（可被显式授予），CanWrite 管既有资源的增删改（viewer 恒拒）。
func TestViewerGrantedCreateStillReadOnly(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	viewer := env.mustUser(t, "viewer-grant", "viewer", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	vC, vCSRF := env.loginAs(t, "viewer-grant", "password123")

	rec := env.do(t, "PATCH", "/api/v1/users/"+itoa(viewer.ID), adminC, adminCSRF, map[string]any{
		"permissions": []string{"monitor.create"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("grant perm: %d %s", rec.Code, rec.Body.String())
	}

	// 授权后可以创建（§19.5 显式配置优先）
	rec = env.do(t, "POST", "/api/v1/monitors", vC, vCSRF, map[string]any{
		"name": "granted-mon", "source_type": "manual", "source_config": map[string]any{"version": "1.0.0"}, "interval_sec": 300,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("granted viewer create = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	var mon struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &mon)

	// 但既有资源的增删改仍拒绝
	if rec := env.do(t, "DELETE", "/api/v1/monitors/"+itoa(mon.ID), vC, vCSRF, nil); rec.Code != http.StatusForbidden {
		t.Errorf("granted viewer DELETE = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, "POST", "/api/v1/monitors/"+itoa(mon.ID)+"/check", vC, vCSRF, nil); rec.Code != http.StatusForbidden {
		t.Errorf("granted viewer check-now = %d, want 403 (%s)", rec.Code, rec.Body.String())
	}
}
