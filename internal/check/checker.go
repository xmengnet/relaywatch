package check

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// Checker 单条监控的检查编排。
type Checker struct {
	Registry  *source.Registry
	Store     store.Store
	Threshold int           // 连续失败多少次进入 error
	Timeout   time.Duration // 单源请求超时
	UserAgent string
	// Notifier 可选：状态变化时的钩子（M3 接投递）。
	OnEvent func(Event)
	// OnChange 可选：检测到需要投递的版本变更时回调（notify.Dispatcher.OnChange）。
	OnChange func(ctx context.Context, monitor *store.Monitor, change *store.VersionChange, notify bool)
	// OnChecked 每次检查成功落库后调用（双轨对比评估用）。
	OnChecked func(ctx context.Context, monitor *store.Monitor)
}

// Event 检查完成事件（SSE 推送用）。
type Event struct {
	MonitorID  int64  `json:"monitor_id"`
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Outcome 是一次检查的详细结果（手动检查同步返回用）。
type Outcome struct {
	OK         bool
	Version    string
	State      store.MonitorState
	Error      string
	DurationMS int64
	Skipped    bool // 版本被过滤
}

// Check 执行一次检查：拉源、规范化、过滤、状态机、落库。
func (c *Checker) Check(ctx context.Context, m *store.Monitor) Outcome {
	start := time.Now()
	out := Outcome{State: m.State}

	src, ok := c.Registry.Get(m.SourceType)
	if !ok {
		return c.fail(ctx, m, start, fmt.Sprintf("未知源类型 %s", m.SourceType), false)
	}

	var cfg map[string]any
	if err := json.Unmarshal([]byte(m.SourceConfig), &cfg); err != nil {
		return c.fail(ctx, m, start, "source_config 不是合法 JSON: "+err.Error(), false)
	}
	cfg = source.ApplyHostDefaults(m.SourceType, cfg) // 「网站管理」站点默认值回填

	fetchCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	res, err := src.Fetch(fetchCtx, cfg)
	if err != nil {
		var nf *source.NotFoundError
		return c.fail(ctx, m, start, err.Error(), asNotFound(err, &nf))
	}
	out.OK = true
	out.DurationMS = time.Since(start).Milliseconds()

	raw := res.Version
	version := source.Normalize(raw, m.VersionPrefix, m.VersionSuffix)
	if m.FromPattern != "" {
		rewritten, err := source.RewriteVersion(version, m.FromPattern, m.ToPattern)
		if err != nil {
			return c.fail(ctx, m, start, "from_pattern 正则重写失败: "+err.Error(), false)
		}
		version = rewritten
	}
	if version == "" {
		return c.fail(ctx, m, start, fmt.Sprintf("源返回空版本（原始 %q）", raw), false)
	}
	out.Version = version

	// 版本过滤：被过滤的版本记历史但不改变状态
	skipped, err := VersionFilter(version, m.VersionIncludeRegex, m.VersionIgnoreRegex)
	if err != nil {
		return c.fail(ctx, m, start, "版本过滤正则无效: "+err.Error(), false)
	}
	out.Skipped = skipped

	// 查开放变更（决定 bump vs update）
	var openChange *store.VersionChange
	if oc, err := c.Store.OpenVersionChange(ctx, m.ID); err == nil {
		openChange = oc
	}

	var decision Decision
	if skipped {
		// 状态不变，只记历史
		decision = Decision{
			State:               m.State,
			CurrentVersion:      m.CurrentVersion,
			LatestVersion:       m.LatestVersion,
			ConsecutiveFailures: 0,
		}
	} else {
		decision = Decide(m, version, openChange)
	}
	out.State = decision.State

	now := time.Now()
	cr := store.CheckResult{
		State:               decision.State,
		CurrentVersion:      decision.CurrentVersion,
		LatestVersion:       decision.LatestVersion,
		LastCheckedAt:       now,
		LastSuccessAt:       &now,
		LastError:           decision.LastError,
		ConsecutiveFailures: decision.ConsecutiveFailures,
	}
	if decision.Change != nil {
		t := now
		cr.LastChangeAt = &t
	}

	// 记录版本变更
	if decision.Change != nil {
		chg := &store.VersionChange{
			MonitorID:  m.ID,
			OldVersion: decision.Change.OldVersion,
			NewVersion: decision.Change.NewVersion,
			DetectedAt: now,
		}
		switch decision.Change.Kind {
		case "bump":
			if openChange != nil {
				openChange.NewVersion = decision.Change.NewVersion
				if err := c.Store.UpdateVersionChange(ctx, openChange); err != nil {
					slog.Error("update version change", "monitor", m.ID, "err", err)
				}
			}
		case "update":
			if err := c.Store.AddVersionChange(ctx, chg); err != nil {
				slog.Error("add version change", "monitor", m.ID, "err", err)
			}
			// 通知编排：新建变更周期 → 入队投递
			if c.OnChange != nil {
				c.OnChange(ctx, m, chg, true)
			}
		case "rollback":
			if err := c.Store.AddVersionChange(ctx, chg); err != nil {
				slog.Error("add version change (rollback)", "monitor", m.ID, "err", err)
			}
		}
	}

	if err := c.Store.UpdateMonitorCheckResult(ctx, m.ID, cr); err != nil {
		slog.Error("update monitor result", "monitor", m.ID, "err", err)
	}
	_ = c.Store.AddCheckHistory(ctx, &store.CheckHistory{
		MonitorID:  m.ID,
		CheckedAt:  now,
		DurationMS: out.DurationMS,
		OK:         true,
		Version:    version,
	})

	if c.OnChecked != nil {
		c.OnChecked(ctx, m)
	}

	c.emit(m.ID, out)
	return out
}

func (c *Checker) fail(ctx context.Context, m *store.Monitor, start time.Time, msg string, notFound bool) Outcome {
	out := Outcome{OK: false, Error: msg, DurationMS: time.Since(start).Milliseconds()}
	d := FailureDecision(m, msg, c.threshold(), notFound)
	out.State = d.State

	now := time.Now()
	cr := store.CheckResult{
		State:               d.State,
		CurrentVersion:      m.CurrentVersion,
		LatestVersion:       m.LatestVersion,
		LastCheckedAt:       now,
		LastError:           msg,
		ConsecutiveFailures: d.ConsecutiveFailures,
	}
	if notFound {
		cr.ConsecutiveFailures = 0
	}
	if err := c.Store.UpdateMonitorCheckResult(ctx, m.ID, cr); err != nil {
		slog.Error("update monitor result (fail)", "monitor", m.ID, "err", err)
	}
	_ = c.Store.AddCheckHistory(ctx, &store.CheckHistory{
		MonitorID:  m.ID,
		CheckedAt:  now,
		DurationMS: out.DurationMS,
		OK:         false,
		Error:      msg,
	})

	c.emit(m.ID, out)
	return out
}

func (c *Checker) emit(monitorID int64, out Outcome) {
	if c.OnEvent != nil {
		c.OnEvent(Event{
			MonitorID:  monitorID,
			OK:         out.OK,
			Version:    out.Version,
			State:      string(out.State),
			Error:      out.Error,
			DurationMS: out.DurationMS,
		})
	}
}

func (c *Checker) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 20 * time.Second
}

func (c *Checker) threshold() int {
	if c.Threshold > 0 {
		return c.Threshold
	}
	return 3
}

func asNotFound(err error, target **source.NotFoundError) bool {
	_, ok := err.(*source.NotFoundError)
	return ok
}
