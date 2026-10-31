package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"relaywatch/internal/store"
)

// 回归两个问题：
//  1. 共享渠道解密后的 config（bot_token/密码等）原样下发所有普通用户，
//     /export?redact=0 还可批量明文带走；
//  2. canAccessChannel 把共享渠道判为不可访问：列表可见但详情/测试 404，语义打架。
func TestSharedChannelMaskingAndAccess(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "admin-ac", "admin", "password123")
	env.mustUser(t, "owner1", "user", "password123")
	env.mustUser(t, "other1", "user", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	ownerC, ownerCSRF := env.loginAs(t, "owner1", "password123")
	otherC, otherCSRF := env.loginAs(t, "other1", "password123")

	// admin 建共享 telegram 渠道（bot_token 为机密字段）
	rec := env.do(t, "POST", "/api/v1/channels", adminC, adminCSRF, map[string]any{
		"name": "tg-shared", "type": "telegram",
		"config": map[string]any{"bot_token": "SUPER-SECRET-TOKEN", "chat_id": "123"}, "shared": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create shared channel = %d %s", rec.Code, rec.Body.String())
	}
	var ch struct {
		ID     int64  `json:"id"`
		Config string `json:"config"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if !strings.Contains(ch.Config, "SUPER-SECRET-TOKEN") {
		t.Errorf("admin create response should contain secret, got %s", ch.Config)
	}
	sharedPath := fmt.Sprintf("/api/v1/channels/%d", ch.ID)

	// 普通用户详情：200（此前 404 与列表矛盾），且机密掩码
	rec = env.do(t, "GET", sharedPath, otherC, otherCSRF, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("other user GET shared channel = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "SUPER-SECRET-TOKEN") {
		t.Errorf("secret leaked to non-owner: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), channelMask) {
		t.Errorf("config should be masked, got %s", rec.Body.String())
	}

	// 列表同样可见且掩码
	rec = env.do(t, "GET", "/api/v1/channels", otherC, otherCSRF, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "tg-shared") {
		t.Fatalf("other user list = %d %s, want shared channel visible", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SUPER-SECRET-TOKEN") {
		t.Errorf("secret leaked in list: %s", rec.Body.String())
	}

	// 无修改权 → PATCH/DELETE 拒绝
	rec = env.do(t, "PATCH", sharedPath, otherC, otherCSRF, map[string]any{"name": "hijack", "type": "telegram"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("other user PATCH shared = %d, want 404", rec.Code)
	}
	rec = env.do(t, "DELETE", sharedPath, otherC, otherCSRF, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("other user DELETE shared = %d, want 404", rec.Code)
	}

	// admin 明文导出；普通用户 redact=0 → 403（默认脱敏导出仍可用）
	if rec := env.do(t, "GET", "/api/v1/export?redact=0", otherC, otherCSRF, nil); rec.Code != http.StatusForbidden {
		t.Errorf("user redact=0 export = %d, want 403", rec.Code)
	}
	if rec := env.do(t, "GET", "/api/v1/export?redact=0", adminC, adminCSRF, nil); rec.Code != http.StatusOK {
		t.Errorf("admin redact=0 export = %d, want 200", rec.Code)
	}

	// owner 自己的渠道不掩码
	rec = env.do(t, "POST", "/api/v1/channels", ownerC, ownerCSRF, map[string]any{
		"name": "own-tg", "type": "telegram",
		"config": map[string]any{"bot_token": "OWNER-SECRET", "chat_id": "456"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("owner create = %d %s", rec.Code, rec.Body.String())
	}
	var own struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &own)
	rec = env.do(t, "GET", fmt.Sprintf("/api/v1/channels/%d", own.ID), ownerC, ownerCSRF, nil)
	if !strings.Contains(rec.Body.String(), "OWNER-SECRET") {
		t.Errorf("owner should see own secret, got %s", rec.Body.String())
	}
}

// 回归：监控 create/update 的 channel_ids 全链路无归属校验，
// 用户可把他人私有渠道绑到自己监控（横向越权）。
func TestMonitorChannelOwnershipValidation(t *testing.T) {
	env := newMultiUserEnv(t)
	env.mustUser(t, "alice-mc", "user", "password123")
	bob := env.mustUser(t, "bob-mc", "user", "password123")
	env.mustUser(t, "admin-ac", "admin", "password123")
	adminC, adminCSRF := env.loginAs(t, "admin-ac", "password123")
	aliceC, aliceCSRF := env.loginAs(t, "alice-mc", "password123")
	bobC, bobCSRF := env.loginAs(t, "bob-mc", "password123")

	// alice 的私有渠道
	rec := env.do(t, "POST", "/api/v1/channels", aliceC, aliceCSRF, map[string]any{
		"name": "alice-private", "type": "telegram",
		"config": map[string]any{"bot_token": "A", "chat_id": "1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("alice channel = %d %s", rec.Code, rec.Body.String())
	}
	var aliceCh struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceCh)

	// admin 共享渠道（bob 可用）
	rec = env.do(t, "POST", "/api/v1/channels", adminC, adminCSRF, map[string]any{
		"name": "shared-ok", "type": "telegram",
		"config": map[string]any{"bot_token": "S", "chat_id": "2"}, "shared": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("shared channel = %d %s", rec.Code, rec.Body.String())
	}
	var sharedCh struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sharedCh)

	// bob 的监控（直插 store）
	m := &store.Monitor{Name: "bob-mon", SourceType: "manual", SourceConfig: `{"version":"1.0.0"}`,
		IntervalSec: 300, Enabled: true, State: store.StateUnknown, OwnerID: bob.ID}
	if err := env.st.CreateMonitor(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	monPath := fmt.Sprintf("/api/v1/monitors/%d", m.ID)

	// 绑定他人私有渠道 → 400
	rec = env.do(t, "PATCH", monPath, bobC, bobCSRF, map[string]any{
		"name": "bob-mon", "source_type": "manual",
		"source_config": map[string]any{"version": "1.0.0"}, "channel_ids": []int64{aliceCh.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bind other's private channel = %d %s, want 400", rec.Code, rec.Body.String())
	}
	// 创建时同样拒绝
	rec = env.do(t, "POST", "/api/v1/monitors/", bobC, bobCSRF, map[string]any{
		"name": "bob-mon2", "source_type": "manual",
		"source_config": map[string]any{"version": "1.0.0"}, "channel_ids": []int64{aliceCh.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create with other's private channel = %d %s, want 400", rec.Code, rec.Body.String())
	}
	// 绑定共享渠道 → 允许
	rec = env.do(t, "PATCH", monPath, bobC, bobCSRF, map[string]any{
		"name": "bob-mon", "source_type": "manual",
		"source_config": map[string]any{"version": "1.0.0"}, "channel_ids": []int64{sharedCh.ID},
	})
	if rec.Code != http.StatusOK {
		t.Errorf("bind shared channel = %d %s, want 200", rec.Code, rec.Body.String())
	}
	got, _ := env.st.GetMonitor(context.Background(), m.ID)
	if len(got.ChannelIDs) != 1 || got.ChannelIDs[0] != sharedCh.ID {
		t.Errorf("monitor channels = %v, want [%d]", got.ChannelIDs, sharedCh.ID)
	}
}