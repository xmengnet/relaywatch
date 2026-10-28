package check

import (
	"testing"

	"relaywatch/internal/store"
)

func baseMonitor() *store.Monitor {
	return &store.Monitor{
		ID:             1,
		State:          store.StateUnknown,
		CurrentVersion: "",
		LatestVersion:  "",
	}
}

func TestVersionFilter(t *testing.T) {
	cases := []struct {
		version, include, ignore string
		wantSkipped              bool
	}{
		{"1.2.3", "", "", false},
		{"1.2.3", `^1\.`, "", false},
		{"2.0.0", `^1\.`, "", true},            // include 不匹配 → 跳过
		{"1.2.3-nightly", "", "nightly", true}, // ignore 匹配 → 跳过
		{"1.2.3", "", "nightly", false},
	}
	for _, c := range cases {
		got, err := VersionFilter(c.version, c.include, c.ignore)
		if err != nil {
			t.Fatalf("VersionFilter(%q,%q,%q) err: %v", c.version, c.include, c.ignore, err)
		}
		if got != c.wantSkipped {
			t.Errorf("VersionFilter(%q,%q,%q) skipped=%v, want %v", c.version, c.include, c.ignore, got, c.wantSkipped)
		}
	}
	if _, err := VersionFilter("1", "[invalid", ""); err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestDecideFirstCheck(t *testing.T) {
	m := baseMonitor()
	d := Decide(m, "0.1.19", nil)
	if d.State != store.StateUpToDate {
		t.Errorf("state = %s, want up_to_date", d.State)
	}
	if d.CurrentVersion != "0.1.19" {
		t.Errorf("baseline = %s, want 0.1.19", d.CurrentVersion)
	}
	if d.Change != nil {
		t.Error("first check must not notify")
	}
}

func TestDecideUpdate(t *testing.T) {
	m := baseMonitor()
	m.State = store.StateUpToDate
	m.CurrentVersion = "0.1.18"
	m.LatestVersion = "0.1.18"
	d := Decide(m, "0.1.19", nil)
	if d.State != store.StateHasUpdate {
		t.Errorf("state = %s, want has_update", d.State)
	}
	if d.Change == nil || d.Change.Kind != "update" || !d.Change.Notify {
		t.Errorf("change = %+v, want update+notify", d.Change)
	}
}

func TestDecideBumpInOpenCycle(t *testing.T) {
	m := baseMonitor()
	m.State = store.StateHasUpdate
	m.CurrentVersion = "0.1.18"
	m.LatestVersion = "0.1.19"
	open := &store.VersionChange{ID: 7, NewVersion: "0.1.19"}
	d := Decide(m, "0.1.20", open)
	if d.State != store.StateHasUpdate {
		t.Errorf("state = %s", d.State)
	}
	if d.Change == nil || d.Change.Kind != "bump" || d.Change.Notify {
		t.Errorf("change = %+v, want bump without notify", d.Change)
	}
}

func TestDecideRollback(t *testing.T) {
	m := baseMonitor()
	m.State = store.StateUpToDate
	m.CurrentVersion = "1.0"
	m.LatestVersion = "1.2" // 上游曾到 1.2，后来删了 release
	d := Decide(m, "1.1", nil)
	// 基线 1.0 < 1.1：其实是「更高」，不是回退。
	// 回退场景：latest 1.2 → 查到 1.1，但基线 1.0 仍 < 1.1，所以还是 has_update。
	// 真回退：查到的版本低于基线。
	d2 := Decide(m, "0.9", nil)
	if d2.State != store.StateUpToDate {
		t.Errorf("rollback: state = %s, want up_to_date (保持原值)", d2.State)
	}
	if d2.CurrentVersion != "1.0" {
		t.Errorf("rollback: baseline moved to %s", d2.CurrentVersion)
	}
	if d2.LatestVersion != "0.9" {
		t.Errorf("rollback: latest = %s, want 0.9 (跟随上游)", d2.LatestVersion)
	}
	if d2.Change == nil || d2.Change.Kind != "rollback" || d2.Change.Notify {
		t.Errorf("rollback: change = %+v, want rollback without notify", d2.Change)
	}
	_ = d
}

func TestDecideAckedBack(t *testing.T) {
	// ack 后 latest == current，再次检查相同版本 → up_to_date
	m := baseMonitor()
	m.State = store.StateUpToDate
	m.CurrentVersion = "0.1.19"
	m.LatestVersion = "0.1.19"
	d := Decide(m, "0.1.19", nil)
	if d.State != store.StateUpToDate || d.Change != nil {
		t.Errorf("state=%s change=%v", d.State, d.Change)
	}
}

func TestFailureDecision(t *testing.T) {
	m := baseMonitor()
	m.State = store.StateUpToDate
	m.CurrentVersion = "1.0"

	// 前两次失败保持 up_to_date
	d := FailureDecision(m, "timeout", 3, false)
	if d.State != store.StateUpToDate || d.ConsecutiveFailures != 1 {
		t.Errorf("1st failure: %+v", d)
	}
	m.ConsecutiveFailures = 1
	d = FailureDecision(m, "timeout", 3, false)
	if d.State != store.StateUpToDate {
		t.Errorf("2nd failure: state=%s", d.State)
	}
	// 第三次（计数 2+1=3）→ error
	m.ConsecutiveFailures = 2
	d = FailureDecision(m, "timeout", 3, false)
	if d.State != store.StateError {
		t.Errorf("3rd failure: state=%s", d.State)
	}
	// 404 → 立即 missing，失败计数清零
	d = FailureDecision(m, "HTTP 404", 3, true)
	if d.State != store.StateMissing || d.ConsecutiveFailures != 0 {
		t.Errorf("notFound: %+v", d)
	}
}
