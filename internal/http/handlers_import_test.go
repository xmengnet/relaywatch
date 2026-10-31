package httpapi

import (
	"context"
	"net/http"
	"testing"

	"relaywatch/internal/store"
)

// 回归：导入构造 Monitor 时漏设 OwnerID，落库 owner_id=0，
// 非 admin 导入者自己看不到导入的监控（404/列表为空），仅 admin 可见。
func TestImportSetsOwnerToImporter(t *testing.T) {
	env := newMultiUserEnv(t)
	// 普通用户需 import 权限（user 角色默认含）
	u := env.mustUser(t, "importer1", "user", "password123")
	uC, uCSRF := env.loginAs(t, "importer1", "password123")

	rec := env.do(t, "POST", "/api/v1/import", uC, uCSRF, map[string]any{
		"text": `
[aria2-config]
source = "aur"
aur = "aria2-config"
`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d %s", rec.Code, rec.Body.String())
	}

	items, total, err := env.st.ListMonitors(context.Background(), store.MonitorFilter{OwnerID: u.ID, Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("importer visible monitors = %d (total %d), want 1", len(items), total)
	}
	if items[0].OwnerID != u.ID {
		t.Errorf("monitor owner = %d, want importer %d", items[0].OwnerID, u.ID)
	}
}
