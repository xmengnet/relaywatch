package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// newTestStore 打开临时 SQLite。
func newTestStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func ptrOf(t time.Time) *time.Time { return &t }

func setupFixture(t *testing.T, st store.Store, cooldownSec int) (*store.Monitor, *store.Channel) {
	t.Helper()
	ctx := context.Background()
	u := &store.User{Username: "admin", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	ch := &store.Channel{Name: "hook", Type: "webhook", Config: `{}`, Enabled: true, OwnerID: &u.ID}
	if err := st.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	m := &store.Monitor{
		OwnerID:           u.ID,
		Name:              "mygo",
		SourceType:        "github_release",
		SourceConfig:      `{}`,
		IntervalSec:       300,
		Enabled:           true,
		State:             store.StateHasUpdate,
		CurrentVersion:    "0.1.0",
		LatestVersion:     "0.1.20",
		NotifyCooldownSec: cooldownSec,
	}
	if err := st.CreateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMonitorChannels(ctx, m.ID, []int64{ch.ID}); err != nil {
		t.Fatal(err)
	}
	chg := &store.VersionChange{MonitorID: m.ID, OldVersion: "0.1.0", NewVersion: "0.1.20", DetectedAt: time.Now()}
	if err := st.AddVersionChange(ctx, chg); err != nil {
		t.Fatal(err)
	}
	return m, ch
}

func TestDispatcherCooldown(t *testing.T) {
	st := newTestStore(t)
	// 冷静期 1 小时：上次投递就在刚才 → deferred
	m, _ := setupFixture(t, st, 3600)
	if err := st.UpdateMonitorCheckResult(context.Background(), m.ID, store.CheckResult{
		State: m.State, CurrentVersion: m.CurrentVersion, LatestVersion: m.LatestVersion,
		LastCheckedAt: time.Now(), LastNotifiedAt: ptrOf(time.Now()),
	}); err != nil {
		t.Fatal(err)
	}

	d := &Dispatcher{Store: st, Notifiers: notifier.NewRegistry()}
	open, err := st.OpenVersionChange(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.enqueue(context.Background(), m, open); err != nil {
		t.Fatal(err)
	}
	// deferred 记录的 next_attempt 在冷静期结束（1 小时后），不在 due 范围内；
	// 直接查该监控的投递列表验证状态与排期
	dl, _, err := st.ListDeliveries(context.Background(), m.ID, 0, "", 0, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dl) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(dl))
	}
	now := time.Now()
	if dl[0].Status != store.DeliveryDeferred {
		t.Errorf("status = %s, want deferred", dl[0].Status)
	}
	if dl[0].NextAttemptAt.Before(now.Add(59 * time.Minute)) {
		t.Errorf("next_attempt = %v, want ~1h later", dl[0].NextAttemptAt)
	}

	// deferred 期间用户 ack → worker 补发校验应作废
	if _, err := st.CancelOpenDeliveries(context.Background(), m.ID, "acknowledged by user"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetDelivery(context.Background(), dl[0].ID)
	if got.Status != store.DeliveryCanceled {
		t.Errorf("status = %s, want canceled", got.Status)
	}
}

func TestDispatcherNoCooldown(t *testing.T) {
	st := newTestStore(t)
	m, _ := setupFixture(t, st, 0)

	d := &Dispatcher{Store: st, Notifiers: notifier.NewRegistry()}
	open, _ := st.OpenVersionChange(context.Background(), m.ID)
	if err := d.enqueue(context.Background(), m, open); err != nil {
		t.Fatal(err)
	}
	due, _ := st.DueDeliveries(context.Background(), time.Now(), 10)
	if len(due) != 1 || due[0].Status != store.DeliveryPending {
		t.Fatalf("due = %+v, want 1 pending", due)
	}
}

func TestWorkerSendsAndRetries(t *testing.T) {
	st := newTestStore(t)
	m, ch := setupFixture(t, st, 0)

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(500) // 第一次失败（可重试）
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ch.Config = `{"url":"` + srv.URL + `"}`
	if err := st.UpdateChannel(context.Background(), ch); err != nil {
		t.Fatal(err)
	}

	reg := notifier.NewRegistry()
	reg.Register(&notifier.WebhookNotifier{})
	w := &Worker{Store: st, Notifiers: reg, Interval: 10 * time.Millisecond}

	// 入队 pending
	d := &Dispatcher{Store: st, Notifiers: reg}
	open, _ := st.OpenVersionChange(context.Background(), m.ID)
	if err := d.enqueue(context.Background(), m, open); err != nil {
		t.Fatal(err)
	}
	due, _ := st.DueDeliveries(context.Background(), time.Now(), 10)
	if len(due) != 1 {
		t.Fatalf("due = %d", len(due))
	}

	// 第一次处理：500 → 重试排期
	w.ProcessOne(context.Background(), due[0])
	got, _ := st.GetDelivery(context.Background(), due[0].ID)
	if got.Status != store.DeliveryPending {
		t.Fatalf("after 1st: status = %s err=%s, want pending(retry)", got.Status, got.LastError)
	}
	if got.NextAttemptAt == nil || got.NextAttemptAt.Before(time.Now().Add(time.Second)) {
		t.Errorf("next_attempt = %v, want backoff scheduled", got.NextAttemptAt)
	}

	// 把 next_attempt 拨到过去模拟等待，再处理：200 → sent
	past := time.Now().Add(-time.Second)
	got.NextAttemptAt = &past
	_ = st.UpdateDelivery(context.Background(), got)
	w.ProcessOne(context.Background(), got)
	got, _ = st.GetDelivery(context.Background(), due[0].ID)
	if got.Status != store.DeliverySent {
		t.Fatalf("after 2nd: status = %s err=%s, want sent", got.Status, got.LastError)
	}

	// last_notified_at 已更新（静默期基准）
	m2, _ := st.GetMonitor(context.Background(), m.ID)
	if m2.LastNotifiedAt == nil {
		t.Error("last_notified_at not set after sent")
	}
}

func TestWorkerCancelOnAcked(t *testing.T) {
	st := newTestStore(t)
	m, _ := setupFixture(t, st, 0)
	reg := notifier.NewRegistry()
	reg.Register(&notifier.WebhookNotifier{})
	w := &Worker{Store: st, Notifiers: reg}

	// 入队后 ack 掉变更 → deferred 补发校验失败应作废
	d := &Dispatcher{Store: st, Notifiers: reg}
	open, _ := st.OpenVersionChange(context.Background(), m.ID)
	_ = d.enqueue(context.Background(), m, open)
	due, _ := st.DueDeliveries(context.Background(), time.Now(), 10)

	now := time.Now()
	uid := int64(1)
	open.AcknowledgedAt = &now
	open.AcknowledgedBy = &uid
	_ = st.UpdateVersionChange(context.Background(), open)

	w.ProcessOne(context.Background(), due[0])
	got, _ := st.GetDelivery(context.Background(), due[0].ID)
	if got.Status != store.DeliveryCanceled {
		t.Errorf("status = %s, want canceled (acked)", got.Status)
	}
}

func TestBackoff(t *testing.T) {
	w := &Worker{BaseDelay: 2 * time.Second, MaxDelay: time.Minute}
	cases := []struct{ attempts, wantSec int }{
		{1, 2}, {2, 4}, {3, 8}, {6, 60},
	}
	for _, c := range cases {
		got := w.backoff(c.attempts)
		if got != time.Duration(c.wantSec)*time.Second {
			t.Errorf("backoff(%d) = %v, want %ds", c.attempts, got, c.wantSec)
		}
	}
}

// TestRetryConfigHonored 锁住 notifications.retry 配置接线：非零值生效，零值回退默认。
func TestRetryConfigHonored(t *testing.T) {
	w := &Worker{MaxAttempts: 7, BaseDelay: 3 * time.Second, MaxDelay: 30 * time.Second}
	if got := w.maxAttempts(); got != 7 {
		t.Errorf("maxAttempts = %d, want 7", got)
	}
	if got := w.backoff(1); got != 3*time.Second {
		t.Errorf("backoff(1) = %v, want 3s", got)
	}
	if got := w.backoff(99); got != 30*time.Second {
		t.Errorf("backoff(99) = %v, want clamp 30s", got)
	}

	def := &Worker{}
	if got := def.maxAttempts(); got != 5 {
		t.Errorf("default maxAttempts = %d, want 5", got)
	}
	if got := def.backoff(1); got != 2*time.Second {
		t.Errorf("default backoff(1) = %v, want 2s", got)
	}
}
