package compare

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"relaywatch/internal/store"
)

func TestAlign(t *testing.T) {
	cases := []struct {
		ver, src, want string
	}{
		{"0.1.19-1", "aur", "0.1.19"},
		{"1:2.0.0-3", "aur", "2.0.0"},
		{"0.1.19-1", "github_release", "0.1.19-1"},
		{"v1.0", "aur", "v1.0"},
	}
	for _, c := range cases {
		got := Align(c.ver, c.src)
		if got != c.want {
			t.Errorf("Align(%q, %q) = %q, want %q", c.ver, c.src, got, c.want)
		}
	}
}

func TestComputeState(t *testing.T) {
	cases := []struct {
		fVer, rVer, fType, rType, want string
	}{
		{"0.1.19-1", "0.1.19", "aur", "github_release", StateInSync},
		{"0.1.18-1", "0.1.19", "aur", "github_release", StateLagging},
		{"0.1.20-1", "0.1.19", "aur", "github_release", StateAhead},
		{"0.1.19", "0.1.19", "github_release", "github_release", StateInSync},
	}
	for _, c := range cases {
		got, err := ComputeState(c.fVer, c.rVer, c.fType, c.rType)
		if err != nil {
			t.Fatalf("ComputeState err: %v", err)
		}
		if got != c.want {
			t.Errorf("ComputeState(%q, %q) = %q, want %q", c.fVer, c.rVer, got, c.want)
		}
	}
}

func TestDecideEvent(t *testing.T) {
	cases := []struct {
		oldSt, newSt, refVer, lastNotified string
		wantEvent                          EventType
		wantNotify                         bool
	}{
		{"", StateLagging, "1.0.0", "", EventEnteredLagging, true},
		{StateInSync, StateLagging, "1.0.0", "", EventEnteredLagging, true},
		{StateLagging, StateLagging, "1.0.0", "1.0.0", "", false},
		{StateLagging, StateLagging, "1.1.0", "1.0.0", EventGapGrew, true},
		{StateLagging, StateInSync, "1.1.0", "1.0.0", EventCaughtUp, true},
		{StateInSync, StateAhead, "1.0.0", "", "", false},
	}
	for _, c := range cases {
		ev, notify := DecideEvent(c.oldSt, c.newSt, c.refVer, c.lastNotified)
		if ev != c.wantEvent || notify != c.wantNotify {
			t.Errorf("DecideEvent(%q, %q, %q, %q) = (%q, %v), want (%q, %v)",
				c.oldSt, c.newSt, c.refVer, c.lastNotified, ev, notify, c.wantEvent, c.wantNotify)
		}
	}
}

func TestEvaluator(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	_ = st.Migrate(ctx)

	admin := &store.User{Username: "admin", Role: "admin"}
	if err := st.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}

	// 1. 创建上游参照监控（GitHub）
	ref := &store.Monitor{
		OwnerID:       admin.ID,
		Name:          "upstream-repo",
		SourceType:    "github_release",
		SourceConfig:  `{"repo":"owner/repo"}`,
		IntervalSec:   300,
		Enabled:       true,
		State:         store.StateUpToDate,
		LatestVersion: "1.2.0",
	}
	if err := st.CreateMonitor(ctx, ref); err != nil {
		t.Fatal(err)
	}

	// 2. 创建跟随监控（AUR），初始版本为 1.1.0-1
	refID := ref.ID
	follower := &store.Monitor{
		OwnerID:            admin.ID,
		Name:               "aur-pkg",
		SourceType:         "aur",
		SourceConfig:       `{"pkgname":"aur-pkg"}`,
		IntervalSec:        300,
		Enabled:            true,
		State:              store.StateUpToDate,
		LatestVersion:      "1.1.0-1",
		ReferenceMonitorID: &refID,
	}
	if err := st.CreateMonitor(ctx, follower); err != nil {
		t.Fatal(err)
	}

	var events []EventType
	eval := NewEvaluator(st, func(ctx context.Context, f, r *store.Monitor, ev EventType) error {
		events = append(events, ev)
		return nil
	})

	// 首次评估：应进入落后 (1.1.0 vs 1.2.0)
	if err := eval.EvaluatePair(ctx, follower.ID); err != nil {
		t.Fatalf("EvaluatePair err: %v", err)
	}

	f1, _ := st.GetMonitor(ctx, follower.ID)
	if f1.CompareState != StateLagging {
		t.Errorf("CompareState = %q, want %q", f1.CompareState, StateLagging)
	}
	if len(events) != 1 || events[0] != EventEnteredLagging {
		t.Fatalf("events = %v, want [%q]", events, EventEnteredLagging)
	}

	// 上游版本变大到 1.3.0（经检查结果通道落库，latest_version 不走 UpdateMonitor），触发 EvaluateOnChecked
	ref.LatestVersion = "1.3.0"
	_ = st.UpdateMonitorCheckResult(ctx, ref.ID, store.CheckResult{
		State: ref.State, LatestVersion: "1.3.0", LastCheckedAt: time.Now(),
	})
	eval.EvaluateOnChecked(ctx, ref)

	if len(events) != 2 || events[1] != EventGapGrew {
		t.Fatalf("expected gap_grew, got: %v", events)
	}

	// 跟随监控更新追平到 1.3.0-1
	follower.LatestVersion = "1.3.0-1"
	_ = st.UpdateMonitorCheckResult(ctx, follower.ID, store.CheckResult{
		State: follower.State, LatestVersion: "1.3.0-1", LastCheckedAt: time.Now(),
	})
	eval.EvaluateOnChecked(ctx, follower)

	f2, _ := st.GetMonitor(ctx, follower.ID)
	if f2.CompareState != StateInSync {
		t.Errorf("expected in_sync, got: %q", f2.CompareState)
	}
	if len(events) != 3 || events[2] != EventCaughtUp {
		t.Fatalf("expected caught_up, got: %v", events)
	}
}
