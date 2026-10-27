package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newMultiUserStore(t *testing.T) (*sqliteStore, context.Context) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, ok := st.(*sqliteStore)
	if !ok {
		t.Fatalf("expected *sqliteStore")
	}
	return s, ctx
}

func mustUser(t *testing.T, st *sqliteStore, ctx context.Context, name, role string, disabled bool) *User {
	t.Helper()
	u := &User{Username: name, Role: role, Disabled: disabled, PasswordHash: []byte("x")}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser %s: %v", name, err)
	}
	return u
}

func mustMonitor(t *testing.T, st *sqliteStore, ctx context.Context, name string, ownerID int64) *Monitor {
	t.Helper()
	m := &Monitor{Name: name, SourceType: "manual", SourceConfig: `{"version":"1.0.0"}`, IntervalSec: 300, Enabled: true, State: StateUnknown, OwnerID: ownerID}
	if err := st.CreateMonitor(ctx, m); err != nil {
		t.Fatalf("CreateMonitor %s: %v", name, err)
	}
	return m
}

// 迁移回填：存量 owner_id=0 的资源归到首个可用 admin 名下；禁用 admin 不参与回填。
// FK 约束禁止 owner_id=0 直插，因此模拟旧库：先跑 0001-0005，插数据，再手工执行 0006 的回填语句验证语义。
func TestMigrationOwnerBackfill(t *testing.T) {
	s, ctx := newMultiUserStore(t)

	// 模拟旧库资源：owner_id 列在 0006 已存在但值由我们控制。
	// 直接建一个无主监控（owner=0 会违反 FK），改为插入后用 0006 的原始语句验证。
	admin := mustUser(t, s, ctx, "admin", "admin", false)
	m := mustMonitor(t, s, ctx, "m1", admin.ID)
	_ = m

	// 用 pragma 关闭 FK 后构造 owner=0 行，等价于从旧库升级时的存量数据
	if _, err := s.db.raw.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("pragma off: %v", err)
	}
	if _, err := s.db.exec(ctx, `INSERT INTO monitors (owner_id, name, source_type, source_config, interval_sec, enabled, state, created_at, updated_at)
		VALUES (0, 'legacy', 'manual', '{}', 300, 1, 'unknown', ?, ?)`, formatTime(time.Now()), formatTime(time.Now())); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if _, err := s.db.raw.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("pragma on: %v", err)
	}

	// 执行 0006 中的回填语句（原样）
	backfill := `UPDATE monitors SET owner_id = (SELECT id FROM users WHERE role = 'admin' AND disabled = 0 ORDER BY id LIMIT 1) WHERE owner_id = 0`
	if _, err := s.db.exec(ctx, backfill); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	var ownerID int64
	if err := s.db.queryRow(ctx, `SELECT owner_id FROM monitors WHERE name = 'legacy'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != admin.ID {
		t.Errorf("legacy backfilled to %d, want %d", ownerID, admin.ID)
	}

	// 禁用 admin 不参与回填：再加一个可用 admin 后重置并回填，应归可用者
	admin2 := mustUser(t, s, ctx, "admin2", "admin", false)
	if err := s.SetUserDisabled(ctx, admin.ID, true); err != nil {
		t.Fatalf("disable admin: %v", err)
	}
	if _, err := s.db.raw.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.exec(ctx, `UPDATE monitors SET owner_id = 0 WHERE name = 'legacy'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.raw.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.exec(ctx, backfill); err != nil {
		t.Fatal(err)
	}
	if err := s.db.queryRow(ctx, `SELECT owner_id FROM monitors WHERE name = 'legacy'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != admin2.ID {
		t.Errorf("backfill after disable = %d, want %d", ownerID, admin2.ID)
	}
}

// owner 过滤：user 只看到自己的；admin 不过滤；无权限查询结果为空。
func TestMonitorOwnerFilter(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	alice := mustUser(t, s, ctx, "alice", "user", false)
	bob := mustUser(t, s, ctx, "bob", "user", false)
	admin := mustUser(t, s, ctx, "admin", "admin", false)

	ma := mustMonitor(t, s, ctx, "alice-mon", alice.ID)
	_ = mustMonitor(t, s, ctx, "bob-mon", bob.ID)
	_ = mustMonitor(t, s, ctx, "admin-mon", admin.ID)

	// alice 视角：只有自己 1 条
	items, total, err := s.ListMonitors(ctx, MonitorFilter{OwnerID: alice.ID, PerPage: 100})
	if err != nil {
		t.Fatalf("ListMonitors: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != ma.ID {
		t.Fatalf("alice view: total=%d len=%d, want 1/1 of %d", total, len(items), ma.ID)
	}

	// bob 视角看不到 alice 的
	items, total, _ = s.ListMonitors(ctx, MonitorFilter{OwnerID: bob.ID, PerPage: 100})
	if total != 1 {
		t.Fatalf("bob view total=%d, want 1", total)
	}

	// admin 视角：全部 3 条
	_, total, _ = s.ListMonitors(ctx, MonitorFilter{PerPage: 100})
	if total != 3 {
		t.Fatalf("admin view total=%d, want 3", total)
	}
}

// 共享渠道可见性：普通用户可见自己的+全局共享；不可见他人私有渠道；admin 全部。
func TestChannelVisibility(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	alice := mustUser(t, s, ctx, "alice", "user", false)
	bob := mustUser(t, s, ctx, "bob", "user", false)
	_ = mustUser(t, s, ctx, "admin", "admin", false)

	shared := &Channel{Name: "shared", Type: "webhook", Config: `{}`, Enabled: true} // OwnerID nil = 全局
	if err := s.CreateChannel(ctx, shared); err != nil {
		t.Fatal(err)
	}
	aliceCh := &Channel{Name: "alice-ch", Type: "webhook", Config: `{}`, Enabled: true, OwnerID: &alice.ID}
	if err := s.CreateChannel(ctx, aliceCh); err != nil {
		t.Fatal(err)
	}
	bobCh := &Channel{Name: "bob-ch", Type: "webhook", Config: `{}`, Enabled: true, OwnerID: &bob.ID}
	if err := s.CreateChannel(ctx, bobCh); err != nil {
		t.Fatal(err)
	}

	// alice 可见：自己的 + 共享
	vis, err := s.ListChannelsVisible(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(vis) != 2 {
		t.Fatalf("alice visible = %d, want 2 (own + shared), got %+v", len(vis), vis)
	}
	// bob 不可见 alice 的
	vis, _ = s.ListChannelsVisible(ctx, bob.ID)
	if len(vis) != 2 {
		t.Fatalf("bob visible = %d, want 2", len(vis))
	}
	// admin 全部
	all, _ := s.ListChannelsAll(ctx)
	if len(all) != 3 {
		t.Fatalf("admin all = %d, want 3", len(all))
	}
}

// 用户删除转移：监控与渠道转移给目标用户，会话与 Token 级联删除。
func TestDeleteUserTransferResources(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	alice := mustUser(t, s, ctx, "alice", "user", false)
	admin := mustUser(t, s, ctx, "admin", "admin", false)

	ma := mustMonitor(t, s, ctx, "alice-mon", alice.ID)
	aliceID := alice.ID
	ch := &Channel{Name: "alice-ch", Type: "webhook", Config: `{}`, Enabled: true, OwnerID: &aliceID}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	sess := &Session{ID: "sess-1", UserID: alice.ID, CSRFToken: "c", ExpiresAt: time.Now().Add(24 * time.Hour)}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	tok := &APIToken{UserID: alice.ID, Name: "t", TokenHash: "hash-1"}
	if err := s.CreateAPIToken(ctx, tok); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteUserTransferResources(ctx, alice.ID, admin.ID); err != nil {
		t.Fatalf("DeleteUserTransferResources: %v", err)
	}

	// 用户没了
	if _, err := s.UserByID(ctx, alice.ID); err != ErrNotFound {
		t.Errorf("user should be deleted, err = %v", err)
	}
	// 监控转移
	m, _ := s.GetMonitor(ctx, ma.ID)
	if m.OwnerID != admin.ID {
		t.Errorf("monitor owner = %d, want %d", m.OwnerID, admin.ID)
	}
	// 渠道转移
	gc, err := s.GetChannel(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gc.OwnerID == nil || *gc.OwnerID != admin.ID {
		t.Errorf("channel owner = %v, want %d", gc.OwnerID, admin.ID)
	}
	// 会话与 Token 清空
	if _, err := s.SessionByID(ctx, "sess-1"); err != ErrNotFound {
		t.Errorf("session should be gone, err = %v", err)
	}
	if _, err := s.APITokenByHash(ctx, "hash-1"); err != ErrNotFound {
		t.Errorf("token should be gone, err = %v", err)
	}
}

// 防自锁：最后一个可用 admin 的判定。
func TestLastAdminGuard(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	a1 := mustUser(t, s, ctx, "a1", "admin", false)
	a2 := mustUser(t, s, ctx, "a2", "admin", false)

	n, _ := s.HasEnabledAdmin(ctx)
	if n != 2 {
		t.Fatalf("enabled admins = %d, want 2", n)
	}
	if err := s.SetUserDisabled(ctx, a1.ID, true); err != nil {
		t.Fatal(err)
	}
	n, _ = s.HasEnabledAdmin(ctx)
	if n != 1 {
		t.Fatalf("after disable = %d, want 1", n)
	}
	// 禁用最后一个 → 0，上层应拒绝该操作
	if err := s.SetUserDisabled(ctx, a2.ID, true); err != nil {
		t.Fatal(err)
	}
	n, _ = s.HasEnabledAdmin(ctx)
	if n != 0 {
		t.Fatalf("all disabled = %d, want 0（上层须先建新 admin）", n)
	}
	// user/viewer 不计入
	mustUser(t, s, ctx, "u1", "user", false)
	mustUser(t, s, ctx, "v1", "viewer", false)
	n, _ = s.HasEnabledAdmin(ctx)
	if n != 0 {
		t.Fatalf("user/viewer counted as admin = %d", n)
	}
}

// 用户资料更新：display_name / role / email；PasswordHash nil 不动哈希。
func TestUpdateUserProfile(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	u := mustUser(t, s, ctx, "alice", "user", false)
	origHash := append([]byte(nil), u.PasswordHash...)

	u.Email = "a@x.io"
	u.DisplayName = "Alice"
	if err := s.UpdateUserProfile(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, _ := s.UserByID(ctx, u.ID)
	if got.Email != "a@x.io" || got.DisplayName != "Alice" {
		t.Errorf("profile = %q/%q", got.Email, got.DisplayName)
	}
	if string(got.PasswordHash) != string(origHash) {
		t.Errorf("password hash changed unexpectedly")
	}

	// 带哈希则更新
	u.PasswordHash = []byte("newhash")
	if err := s.UpdateUserProfile(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, _ = s.UserByID(ctx, u.ID)
	if string(got.PasswordHash) != "newhash" {
		t.Errorf("password not updated")
	}
}

// ListUsers 过滤与分页。
func TestListUsers(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	for _, n := range []string{"alice", "bob", "carol"} {
		mustUser(t, s, ctx, n, "user", false)
	}
	mustUser(t, s, ctx, "root", "admin", false)
	items, total, err := s.ListUsers(ctx, UserFilter{Role: "user", Search: "a", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	// 搜索 "a" 命中 alice/carol（含 admin 表？role=user 过滤后）→ 2
	if total != 2 || len(items) != 2 {
		t.Fatalf("search a role user: total=%d len=%d, want 2", total, len(items))
	}
	for _, u := range items {
		if u.PasswordHash != nil {
			t.Errorf("list should not leak password hash for %s", u.Username)
		}
	}
}

// 监控归属变更（SetMonitorOwner）。
func TestSetMonitorOwner(t *testing.T) {
	s, ctx := newMultiUserStore(t)
	alice := mustUser(t, s, ctx, "alice", "user", false)
	admin := mustUser(t, s, ctx, "admin", "admin", false)
	m := mustMonitor(t, s, ctx, "m", alice.ID)

	if err := s.SetMonitorOwner(ctx, m.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMonitor(ctx, m.ID)
	if got.OwnerID != admin.ID {
		t.Errorf("owner = %d, want %d", got.OwnerID, admin.ID)
	}
	// 注：monitors.owner_id 无数据库级外键（SQLite 不允许 REFERENCES + 非 NULL 默认值的
	// ADD COLUMN），归属完整性由应用层保证（创建强制当前用户、删除时事务内转移），
	// 因此对不存在用户的 SetMonitorOwner 不再期望报错——不专门测试该路径。
}
