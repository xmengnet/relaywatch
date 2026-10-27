// Package scheduler 实现「每条记录自己的间隔」的 ticker 调度。
package scheduler

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math"
	"sync"
	"time"

	"relaywatch/internal/check"
	"relaywatch/internal/store"
)

// HostLimiter per-host 令牌桶注册表。
type HostLimiter struct {
	mu       sync.Mutex
	default_ float64 // 每分钟请求数
	overrides map[string]float64
	buckets  map[string]*hostBucket
}

type hostBucket struct {
	tokens  float64
	last    time.Time
	perSec  float64
	frozenUntil time.Time // Retry-After 冻结
}

// NewHostLimiter 创建 per-host 限速器。perMinute 是默认每分钟请求数。
func NewHostLimiter(perMinute int, overrides map[string]int) *HostLimiter {
	h := &HostLimiter{
		default_:  float64(perMinute),
		overrides: map[string]float64{},
		buckets:   map[string]*hostBucket{},
	}
	for host, n := range overrides {
		h.overrides[host] = float64(n)
	}
	return h
}

// Allow 尝试为 host 取一个令牌。false 表示需等待（任务延迟，不算失败）。
func (h *HostLimiter) Allow(host string, now time.Time) bool {
	if host == "" {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.buckets[host]
	if !ok {
		perMin := h.default_
		if v, ok := h.overrides[host]; ok {
			perMin = v
		}
		b = &hostBucket{tokens: perMin, last: now, perSec: perMin / 60.0}
		h.buckets[host] = b
	}
	if now.Before(b.frozenUntil) {
		return false
	}
	// 补充令牌
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens = math.Min(b.tokens+elapsed*b.perSec, b.perSec*60) // 容量 = 一分钟配额
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Freeze 因 429/Retry-After 冻结 host 到指定时间。
func (h *HostLimiter) Freeze(host string, until time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if b, ok := h.buckets[host]; ok {
		if until.After(b.frozenUntil) {
			b.frozenUntil = until
		}
	}
}

// JitterFactor 由监控 ID 推导确定性抖动因子，范围 [-0.1, +0.1)。
// 同一 ID 在任何进程生命周期中恒定，重启不会导致到期时间跳变。
func JitterFactor(id int64) float64 {
	h := fnv.New64a()
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(id >> (8 * i))
	}
	h.Write(b[:])
	// %2000 / 9999 映射到 [0,1)，再线性映射到 [-0.1, +0.1)
	frac := float64(h.Sum64()%2000) / 2000.0
	return frac*0.2 - 0.1
}

// Scheduler 调度器。
type Scheduler struct {
	Store     store.Store
	Checker   *check.Checker
	Tick      time.Duration        // 调度粒度
	MaxConc   int                  // 最大并发检查数
	Hosts     *HostLimiter
	MinInterval int                // 秒；低于该值的 interval 按该值处理

	sem      chan struct{}
	running  map[int64]bool
	mu       sync.Mutex
	wg       sync.WaitGroup
	cancel   context.CancelFunc
}

// Start 启动调度循环。
func (s *Scheduler) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.sem = make(chan struct{}, s.maxConc())
	s.running = map[int64]bool{}
	s.wg.Add(1)
	go s.loop(ctx)
}

// Stop 等待在跑的检查结束。
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	tick := s.tick()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.round(ctx)
		}
	}
}

func (s *Scheduler) round(ctx context.Context) {
	monitors, err := s.Store.ListEnabledMonitors(ctx)
	if err != nil {
		slog.Error("scheduler list monitors", "err", err)
		return
	}
	now := time.Now()
	for _, m := range monitors {
		interval := m.IntervalSec
		if interval < s.minInterval() {
			interval = s.minInterval()
		}
		due := nextDue(m.LastCheckedAt, interval, JitterFactor(m.ID))
		if now.Before(due) {
			continue
		}
		if s.markRunning(m.ID) {
			go s.run(ctx, m)
		}
	}
}

// nextDue 计算下次到期时间。
func nextDue(lastChecked *time.Time, intervalSec int, jitter float64) time.Time {
	if lastChecked == nil {
		return time.Time{} // 从未检查过：立即到期
	}
	d := time.Duration(float64(intervalSec) * (1 + jitter) * float64(time.Second))
	return lastChecked.Add(d)
}

func (s *Scheduler) run(ctx context.Context, m *store.Monitor) {
	defer func() {
		s.mu.Lock()
		delete(s.running, m.ID)
		s.mu.Unlock()
		<-s.sem
	}()
	s.sem <- struct{}{}

	// per-host 限速：令牌不足则等待最多 3 轮，仍不足则本轮跳过（不算检查）
	if host := s.hostOf(m); host != "" && s.Hosts != nil {
		if !s.waitAllow(ctx, host) {
			slog.Debug("host limited, skip this round", "monitor", m.ID, "host", host)
			return
		}
	}
	out := s.Checker.Check(ctx, m)
	slog.Debug("checked", "monitor", m.ID, "ok", out.OK, "state", out.State, "ms", out.DurationMS)
}

func (s *Scheduler) waitAllow(ctx context.Context, host string) bool {
	for i := 0; i < 3; i++ {
		if s.Hosts.Allow(host, time.Now()) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
	return false
}

// hostOf 从源配置里提取目标主机名（http_json/http_regex 有 url 字段）。
func (s *Scheduler) hostOf(m *store.Monitor) string {
	switch m.SourceType {
	case "github_release", "github_tag":
		return "api.github.com"
	case "aur":
		return "aur.archlinux.org"
	case "arch_official":
		return "archlinux.org"
	default:
		return ""
	}
}

func (s *Scheduler) markRunning(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		return false
	}
	s.running[id] = true
	return true
}

func (s *Scheduler) tick() time.Duration {
	if s.Tick > 0 {
		return s.Tick
	}
	return 10 * time.Second
}

func (s *Scheduler) maxConc() int {
	if s.MaxConc > 0 {
		return s.MaxConc
	}
	return 10
}

func (s *Scheduler) minInterval() int {
	if s.MinInterval > 0 {
		return s.MinInterval
	}
	return 60
}
