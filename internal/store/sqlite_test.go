package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreMonitorPatterns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	admin := &User{Username: "admin", PasswordHash: []byte("x"), Role: "admin"}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	m := &Monitor{
		OwnerID:             admin.ID,
		Name:                "test-monitor",
		SourceType:          "github_release",
		SourceConfig:        `{"repo":"owner/repo"}`,
		VersionPrefix:       "v",
		FromPattern:         `(\d{4})(\d{2})(\d{2})`,
		ToPattern:           `\1.\2.\3`,
		IntervalSec:         300,
		Enabled:             true,
		State:               StateUnknown,
	}

	if err := st.CreateMonitor(ctx, m); err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	if m.ID == 0 {
		t.Fatalf("expected ID > 0")
	}

	got, err := st.GetMonitor(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.FromPattern != `(\d{4})(\d{2})(\d{2})` {
		t.Errorf("FromPattern = %q", got.FromPattern)
	}
	if got.ToPattern != `\1.\2.\3` {
		t.Errorf("ToPattern = %q", got.ToPattern)
	}

	// Update
	got.ToPattern = `$1.$2.$3`
	if err := st.UpdateMonitor(ctx, got); err != nil {
		t.Fatalf("UpdateMonitor: %v", err)
	}

	updated, err := st.GetMonitor(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMonitor updated: %v", err)
	}
	if updated.ToPattern != `$1.$2.$3` {
		t.Errorf("updated ToPattern = %q", updated.ToPattern)
	}

	// 参照关联测试
	refID := m.ID
	follower := &Monitor{
		OwnerID:            admin.ID,
		Name:               "aur-follower",
		SourceType:         "aur",
		SourceConfig:       `{"pkgname":"test"}`,
		IntervalSec:        300,
		Enabled:            true,
		State:              StateUnknown,
		ReferenceMonitorID: &refID,
	}
	if err := st.CreateMonitor(ctx, follower); err != nil {
		t.Fatalf("Create follower: %v", err)
	}

	followers, err := st.ListFollowerMonitors(ctx, refID)
	if err != nil {
		t.Fatalf("ListFollowerMonitors: %v", err)
	}
	if len(followers) != 1 || followers[0].ID != follower.ID {
		t.Fatalf("followers = %v, want [%d]", followers, follower.ID)
	}

	// UpdateMonitorCompare 测试
	now := time.Now()
	if err := st.UpdateMonitorCompare(ctx, follower.ID, "lagging", "1.5.0", &now, "1.5.0"); err != nil {
		t.Fatalf("UpdateMonitorCompare: %v", err)
	}
	gotF, err := st.GetMonitor(ctx, follower.ID)
	if err != nil {
		t.Fatalf("Get follower: %v", err)
	}
	if gotF.CompareState != "lagging" || gotF.CompareRefVersion != "1.5.0" {
		t.Errorf("compare fields mismatch: %+v", gotF)
	}

	// ListMonitors 筛选 compare_state
	filtered, total, err := st.ListMonitors(ctx, MonitorFilter{CompareState: "lagging"})
	if err != nil || total != 1 || len(filtered) != 1 {
		t.Errorf("ListMonitors by compare_state failed: total=%d, err=%v", total, err)
	}

	// 清空参照
	if err := st.ClearFollowerReferences(ctx, refID); err != nil {
		t.Fatalf("ClearFollowerReferences: %v", err)
	}
	clearedF, _ := st.GetMonitor(ctx, follower.ID)
	if clearedF.ReferenceMonitorID != nil || clearedF.CompareState != "" {
		t.Errorf("expected reference cleared, got: %+v", clearedF)
	}
}

