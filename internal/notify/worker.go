package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// Worker 投递 worker：轮询 due 投递，渲染模板、发送、按退避重试。
// deferred 记录补发前做四项校验，失败即作废。
type Worker struct {
	Store     store.Store
	Notifiers *notifier.Registry
	Interval  time.Duration // 轮询间隔，默认 2s
	// 重试策略（来自 notifications.retry 配置；0 值回退默认：5 次 / 2s 起 / 10min 封顶）
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	// ResendLatest 前端手动重发时调用；true 表示重发时版本号取发送时刻 latest_version
	OnDelivery func(status store.DeliveryStatus)

	cancelFn context.CancelFunc // Start 时记录，Stop 时触发（与作废投递的 cancel 方法无关）
	wg       sync.WaitGroup     // 跟踪 loop；Stop 等待其在跑轮次结束
}

// ForceFlag 在投递记录上标记手动重发（运行时字段，不落库）。

// ProcessOne 暴露给调度轮询（同步执行一条投递，含周期校验）。
func (w *Worker) ProcessOne(ctx context.Context, d *store.Delivery) {
	w.processOne(ctx, d)
}

// ProcessForced 同步执行一条手动重发投递（跳过周期校验）。
func (w *Worker) ProcessForced(ctx context.Context, d *store.Delivery) {
	d.Force = true
	w.processOne(ctx, d)
}

// Start 启动 worker 循环。
func (w *Worker) Start(ctx context.Context) {
	ctx, w.cancelFn = context.WithCancel(ctx)
	w.wg.Add(1)
	go w.loop(ctx)
}

// Stop 停止轮询并等待当前轮次结束。
func (w *Worker) Stop() {
	if w.cancelFn != nil {
		w.cancelFn()
	}
	w.wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	defer w.wg.Done()
	t := time.NewTicker(w.interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.round(ctx)
		}
	}
}

func (w *Worker) round(ctx context.Context) {
	due, err := w.Store.DueDeliveries(ctx, time.Now(), 50)
	if err != nil {
		slog.Error("worker due deliveries", "err", err)
		return
	}
	for _, d := range due {
		w.processOne(ctx, d)
	}
}

func (w *Worker) processOne(ctx context.Context, d *store.Delivery) {
	// 取回监控与渠道；先做 deferred 补发校验
	m, err := w.Store.GetMonitor(ctx, d.MonitorID)
	if err == store.ErrNotFound {
		w.cancel(ctx, d, "monitor deleted")
		return
	}
	if err != nil {
		slog.Error("worker get monitor", "err", err)
		return
	}
	ch, err := w.Store.GetChannel(ctx, d.ChannelID)
	if err == store.ErrNotFound {
		w.cancel(ctx, d, "channel deleted")
		return
	}
	if err != nil {
		slog.Error("worker get channel", "err", err)
		return
	}

	// 补发前校验（deferred 记录四项校验，pending 立即投递也校验前两项）
	if !m.Enabled {
		w.cancel(ctx, d, "monitor disabled")
		return
	}
	if !ch.Enabled {
		w.cancel(ctx, d, "channel disabled")
		return
	}

	var msg *notifier.Message
	if d.Kind == "compare" {
		// 双轨对比校验
		if m.ReferenceMonitorID == nil {
			w.cancel(ctx, d, "reference removed")
			return
		}
		msg = w.buildCompareMessage(ctx, m, ch, d, d.Force)
		if msg == nil {
			w.cancel(ctx, d, "compare condition no longer holds")
			return
		}
	} else {
		if d.Status == store.DeliveryDeferred {
			if m.State != store.StateHasUpdate {
				w.cancel(ctx, d, "monitor no longer has_update")
				return
			}
			if d.ChangeID != nil {
				open, err := w.Store.OpenVersionChange(ctx, m.ID)
				if err != nil || open == nil || open.ID != *d.ChangeID {
					w.cancel(ctx, d, "change cycle superseded")
					return
				}
			}
		}
		msg = w.buildMessage(ctx, m, ch, d, d.Force)
		if msg == nil {
			w.cancel(ctx, d, "no open change to deliver")
			return
		}
	}

	// 发送
	n, ok := w.Notifiers.Get(ch.Type)
	if !ok {
		w.cancel(ctx, d, "unknown channel type "+ch.Type)
		return
	}
	cfg, err := cfgFromJSON(ch.Config)
	if err != nil {
		w.cancel(ctx, d, "channel config invalid")
		return
	}
	w.injectOwnerEmail(ctx, ch, cfg)
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sendErr := n.Send(sendCtx, cfg, msg)

	now := time.Now()
	if sendErr == nil {
		d.Status = store.DeliverySent
		d.SentAt = &now
		d.LastError = ""
		// 投递成功：更新监控的 last_notified_at（静默期基准）
		m.LastNotifiedAt = &now
		if err := w.Store.UpdateMonitorCheckResult(ctx, m.ID, store.CheckResult{
			State:               m.State,
			CurrentVersion:      m.CurrentVersion,
			LatestVersion:       m.LatestVersion,
			LastCheckedAt:       nowOr(m.LastCheckedAt),
			LastSuccessAt:       m.LastSuccessAt,
			LastChangeAt:        m.LastChangeAt,
			LastError:           m.LastError,
			ConsecutiveFailures: m.ConsecutiveFailures,
			LastNotifiedAt:      &now,
		}); err != nil {
			slog.Error("touch last_notified_at", "err", err)
		}
	} else {
		d.Attempts++
		d.LastError = sendErr.Error()
		var pe *notifier.PermanentError
		if _, isPerm := sendErr.(*notifier.PermanentError); isPerm || asPermanent(sendErr, &pe) {
			d.Status = store.DeliveryFailed
		} else if d.Attempts >= w.maxAttempts() {
			d.Status = store.DeliveryFailed
		} else {
			next := now.Add(w.backoff(d.Attempts))
			d.NextAttemptAt = &next
		}
	}
	if err := w.Store.UpdateDelivery(ctx, d); err != nil {
		slog.Error("update delivery", "err", err)
	}
	w.emit(d.Status)
	slog.Debug("delivery processed", "id", d.ID, "status", d.Status, "err", d.LastError)
}

// buildMessage 渲染消息。payload 存渲染结果，重发时重渲染保证版本新鲜。
// force=true（手动重发）时无开放变更也照样发送（用监控当前版本）。
func (w *Worker) buildMessage(ctx context.Context, m *store.Monitor, ch *store.Channel, d *store.Delivery, force bool) *notifier.Message {
	// 找当前开放变更
	open, err := w.Store.OpenVersionChange(ctx, m.ID)
	if err != nil {
		if !force {
			// 无开放变更（可能已 ack）——常规投递作废
			return nil
		}
		// 手动重发：用监控当前的 latest 作为新版本
		if m.LatestVersion == "" {
			return nil
		}
		open = &store.VersionChange{
			MonitorID:  m.ID,
			OldVersion: m.CurrentVersion,
			NewVersion: m.LatestVersion,
			DetectedAt: time.Now(),
		}
	}
	newVersion := open.NewVersion
	// 有基线时显示 old→new，否则只显示新版本
	oldVersion := open.OldVersion
	// 补发语义：新版本号取发送时刻监控的 latest_version
	if m.LatestVersion != "" && m.LatestVersion != newVersion {
		// latest 更新了但变更记录还是旧的（bump 未同步的边缘情况），以 latest 为准
		newVersion = m.LatestVersion
	}

	data := notifier.TemplateData{
		Monitor: notifier.MonitorInfo{
			Name:       m.Name,
			SourceType: m.SourceType,
			Tags:       m.Tags,
		},
		Change: notifier.ChangeInfo{
			OldVersion: oldVersion,
			NewVersion: newVersion,
			DetectedAt: open.DetectedAt,
		},
	}
	title, body, err := notifier.Render("", ch.Template, data, true)
	if err != nil {
		slog.Error("render template", "err", err)
		body = fmt.Sprintf("%s: %s → %s", m.Name, oldVersion, newVersion)
	}
	return &notifier.Message{
		Title:    title,
		Body:     body,
		Markdown: true,
		Monitor:  data.Monitor,
		Change:   data.Change,
	}
}

func (w *Worker) buildCompareMessage(ctx context.Context, m *store.Monitor, ch *store.Channel, d *store.Delivery, force bool) *notifier.Message {
	if m.ReferenceMonitorID == nil {
		return nil
	}
	ref, err := w.Store.GetMonitor(ctx, *m.ReferenceMonitorID)
	if err != nil {
		return nil
	}

	var cmpCtx CompareContext
	if d.Context != "" {
		_ = json.Unmarshal([]byte(d.Context), &cmpCtx)
	}

	// 常规投递校验事件是否仍成立（Force 手动重发跳过）
	if !force {
		switch cmpCtx.Event {
		case "entered_lagging", "gap_grew":
			if m.CompareState != "lagging" {
				return nil
			}
		case "caught_up":
			if m.CompareState != "in_sync" {
				return nil
			}
		}
	}

	data := notifier.TemplateData{
		Monitor: notifier.MonitorInfo{
			Name:       m.Name,
			SourceType: m.SourceType,
			Tags:       m.Tags,
		},
		Change: notifier.ChangeInfo{
			DetectedAt: time.Now(),
		},
		Compare: notifier.CompareInfo{
			RefMonitorName:  ref.Name,
			RefVersion:      ref.LatestVersion,
			FollowerVersion: m.LatestVersion,
			State:           m.CompareState,
			Event:           cmpCtx.Event,
		},
	}
	title, body, err := notifier.RenderCompare("", ch.Template, data, true)
	if err != nil {
		slog.Error("render compare template", "err", err)
		body = fmt.Sprintf("%s（%s）对比 %s（%s）：%s", m.Name, m.LatestVersion, ref.Name, ref.LatestVersion, m.CompareState)
	}
	return &notifier.Message{
		Title:    title,
		Body:     body,
		Markdown: true,
		Monitor:  data.Monitor,
	}
}

func (w *Worker) cancel(ctx context.Context, d *store.Delivery, reason string) {
	d.Status = store.DeliveryCanceled
	d.LastError = "canceled: " + reason
	if err := w.Store.UpdateDelivery(ctx, d); err != nil {
		slog.Error("cancel delivery", "err", err)
	}
	w.emit(d.Status)
}

// injectOwnerEmail 邮件渠道收件人缺省时回填渠道所有者的账号邮箱。
// 仅在 to 为空时写入 owner_email，发送方按其兜底；共享渠道无所有者则不填。
func (w *Worker) injectOwnerEmail(ctx context.Context, ch *store.Channel, cfg map[string]any) {
	if ch.Type != "email_smtp" {
		return
	}
	if to, _ := cfg["to"].(string); to != "" {
		return
	}
	if ch.OwnerID == nil || *ch.OwnerID == 0 {
		return
	}
	u, err := w.Store.UserByID(ctx, *ch.OwnerID)
	if err != nil || u == nil || u.Email == "" {
		return
	}
	cfg["owner_email"] = u.Email
}

func (w *Worker) interval() time.Duration {
	if w.Interval > 0 {
		return w.Interval
	}
	return 2 * time.Second
}

func (w *Worker) maxAttempts() int {
	if w.MaxAttempts > 0 {
		return w.MaxAttempts
	}
	return 5
}

// backoff 指数退避：base * 2^(attempts-1)，封顶 max。
func (w *Worker) backoff(attempts int) time.Duration {
	delay := w.baseDelay()
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= w.maxDelay() {
			return w.maxDelay()
		}
	}
	if delay > w.maxDelay() {
		return w.maxDelay()
	}
	return delay
}

func (w *Worker) baseDelay() time.Duration {
	if w.BaseDelay > 0 {
		return w.BaseDelay
	}
	return 2 * time.Second
}

func (w *Worker) maxDelay() time.Duration {
	if w.MaxDelay > 0 {
		return w.MaxDelay
	}
	return 10 * time.Minute
}

func (w *Worker) emit(status store.DeliveryStatus) {
	if w.OnDelivery != nil {
		w.OnDelivery(status)
	}
}

func nowOr(t *time.Time) time.Time {
	if t != nil {
		return *t
	}
	return time.Now()
}

func asPermanent(err error, target **notifier.PermanentError) bool {
	_, ok := err.(*notifier.PermanentError)
	return ok
}

// jsonMarshalHelper 保留 import 引用（payload 序列化在 resend 处使用）。
var _ = json.Marshal
