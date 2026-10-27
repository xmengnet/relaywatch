// Package notify 通知编排：检查结果 → 投递任务入队（含静默期），投递 worker 重试与补发。
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"relaywatch/internal/compare"
	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// Dispatcher 把检查结果转成投递任务。Checker 在每次成功检查后调用 OnChange。
type Dispatcher struct {
	Store     store.Store
	Notifiers *notifier.Registry

	// Retry 配置
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	// OnDelivery 事件钩子（SSE 用，可空）
	OnDelivery func(status store.DeliveryStatus)
}

// 事件里带出的上下文：Checker 只需要告诉 Dispatcher「某监控出现/更新了变更」。
type changeEvent struct {
	Monitor *store.Monitor
	Change  *store.VersionChange
	Notify  bool // false = bump/rollback，不新建投递
}

// OnChange 由 Checker 调用（异步安全，内部串行化落库）。
func (d *Dispatcher) OnChange(ctx context.Context, monitor *store.Monitor, change *store.VersionChange, notify bool) {
	if change == nil || !notify {
		return
	}
	if err := d.enqueue(ctx, monitor, change); err != nil {
		slog.Error("enqueue deliveries", "monitor", monitor.ID, "err", err)
	}
}

// enqueue 为监控绑定的每个启用渠道创建投递记录。
// 静默期：距 last_notified_at 不足 cooldown → deferred 到冷静期结束；否则 pending 立即投。
func (d *Dispatcher) enqueue(ctx context.Context, monitor *store.Monitor, change *store.VersionChange) error {
	// 渠道绑定实时查，避免用快照
	m, err := d.Store.GetMonitor(ctx, monitor.ID)
	if err != nil {
		return fmt.Errorf("reload monitor: %w", err)
	}
	if len(m.ChannelIDs) == 0 {
		return nil
	}
	channels, err := d.Store.ListChannelsAll(ctx)
	if err != nil {
		return err
	}
	byID := map[int64]*store.Channel{}
	for _, c := range channels {
		byID[c.ID] = c
	}

	now := time.Now()
	var lastNotified time.Time
	if m.LastNotifiedAt != nil {
		lastNotified = *m.LastNotifiedAt
	}
	cooldown := time.Duration(m.NotifyCooldownSec) * time.Second
	changeID := change.ID

	for _, cid := range m.ChannelIDs {
		ch := byID[cid]
		if ch == nil || !ch.Enabled {
			continue
		}
		status := store.DeliveryPending
		var nextAt time.Time
		switch {
		case cooldown > 0 && !lastNotified.IsZero() && now.Sub(lastNotified) < cooldown:
			// 静默期中：推迟到冷静期结束
			status = store.DeliveryDeferred
			nextAt = lastNotified.Add(cooldown)
		default:
			nextAt = now
		}
		d1 := &store.Delivery{
			MonitorID:     m.ID,
			ChannelID:     cid,
			ChangeID:      &changeID,
			Status:        status,
			NextAttemptAt: &nextAt,
		}
		if err := d.Store.AddDelivery(ctx, d1); err != nil {
			return err
		}
		d.emit(status)
		slog.Debug("delivery enqueued", "monitor", m.ID, "channel", cid, "status", status)
	}
	return nil
}

// CompareContext 保存双轨对比通知的事件上下文。
type CompareContext struct {
	FollowerID      int64  `json:"follower_id"`
	FollowerName    string `json:"follower_name"`
	FollowerVersion string `json:"follower_version"`
	RefID           int64  `json:"ref_id"`
	RefName         string `json:"ref_name"`
	RefVersion      string `json:"ref_version"`
	Event           string `json:"event"` // entered_lagging | gap_grew | caught_up
}

// EnqueueCompare 为双轨对比事件创建投递记录。
func (d *Dispatcher) EnqueueCompare(ctx context.Context, follower, ref *store.Monitor, event compare.EventType) error {
	m, err := d.Store.GetMonitor(ctx, follower.ID)
	if err != nil {
		return fmt.Errorf("reload follower monitor: %w", err)
	}
	if len(m.ChannelIDs) == 0 {
		return nil
	}
	channels, err := d.Store.ListChannelsAll(ctx)
	if err != nil {
		return err
	}
	byID := map[int64]*store.Channel{}
	for _, c := range channels {
		byID[c.ID] = c
	}

	cmpCtx := CompareContext{
		FollowerID:      follower.ID,
		FollowerName:    follower.Name,
		FollowerVersion: follower.LatestVersion,
		RefID:           ref.ID,
		RefName:         ref.Name,
		RefVersion:      ref.LatestVersion,
		Event:           string(event),
	}
	ctxJSON, _ := json.Marshal(cmpCtx)

	now := time.Now()
	var lastNotified time.Time
	if m.LastNotifiedAt != nil {
		lastNotified = *m.LastNotifiedAt
	}
	cooldown := time.Duration(m.NotifyCooldownSec) * time.Second

	for _, cid := range m.ChannelIDs {
		ch := byID[cid]
		if ch == nil || !ch.Enabled {
			continue
		}
		status := store.DeliveryPending
		var nextAt time.Time
		switch {
		case cooldown > 0 && !lastNotified.IsZero() && now.Sub(lastNotified) < cooldown:
			status = store.DeliveryDeferred
			nextAt = lastNotified.Add(cooldown)
		default:
			nextAt = now
		}
		d1 := &store.Delivery{
			MonitorID:     m.ID,
			ChannelID:     cid,
			Status:        status,
			Kind:          "compare",
			Context:       string(ctxJSON),
			NextAttemptAt: &nextAt,
		}
		if err := d.Store.AddDelivery(ctx, d1); err != nil {
			return err
		}
		d.emit(status)
		slog.Debug("compare delivery enqueued", "follower", m.ID, "channel", cid, "event", event, "status", status)
	}
	return nil
}

func (d *Dispatcher) emit(status store.DeliveryStatus) {
	if d.OnDelivery != nil {
		d.OnDelivery(status)
	}
}

// cfgFromJSON 解析渠道配置。
func cfgFromJSON(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (d *Dispatcher) maxAttempts() int {
	if d.MaxAttempts > 0 {
		return d.MaxAttempts
	}
	return 5
}

func (d *Dispatcher) baseDelay() time.Duration {
	if d.BaseDelay > 0 {
		return d.BaseDelay
	}
	return 2 * time.Second
}

func (d *Dispatcher) maxDelay() time.Duration {
	if d.MaxDelay > 0 {
		return d.MaxDelay
	}
	return 10 * time.Minute
}

// backoff 指数退避：base * 2^attempts，封顶 max。
func (d *Dispatcher) backoff(attempts int) time.Duration {
	delay := d.baseDelay()
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= d.maxDelay() {
			return d.maxDelay()
		}
	}
	if delay > d.maxDelay() {
		return d.maxDelay()
	}
	return delay
}
