package auth

import (
	"sync"
	"time"
)

// AttemptLimiter 是进程内的失败次数限制器：同一 key 在 window 内累计到 limit 次后，
// 锁定 lockout 时长。用于登录失败与注册验证码发送等匿名端点。
type AttemptLimiter struct {
	mu      sync.Mutex
	entries map[string]*attemptEntry
	limit   int
	window  time.Duration
	lockout time.Duration
	now     func() time.Time
}

type attemptEntry struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

// NewAttemptLimiter 创建限制器；limit <= 0 表示不限制。
func NewAttemptLimiter(limit int, window, lockout time.Duration) *AttemptLimiter {
	if window <= 0 {
		window = time.Minute
	}
	if lockout <= 0 {
		lockout = window
	}
	return &AttemptLimiter{
		entries: make(map[string]*attemptEntry),
		limit:   limit,
		window:  window,
		lockout: lockout,
		now:     time.Now,
	}
}

// Allow 判断当前是否仍可尝试。nil 或 limit<=0 时恒为 true。
func (l *AttemptLimiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	e, ok := l.entries[key]
	if !ok {
		return true
	}
	if now.Before(e.blockedUntil) {
		return false
	}
	if now.Sub(e.windowStart) >= l.window {
		delete(l.entries, key)
		return true
	}
	return e.count < l.limit
}

// Hit 记录一次失败/请求；达到 limit 后进入锁定。
func (l *AttemptLimiter) Hit(key string) {
	if l == nil || l.limit <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowStart) >= l.window {
		l.entries[key] = &attemptEntry{count: 1, windowStart: now, lastSeen: now}
		return
	}
	e.count++
	e.lastSeen = now
	if e.count >= l.limit {
		e.blockedUntil = now.Add(l.lockout)
	}
}

// Reset 清除某个 key 的记录（登录成功后调用）。
func (l *AttemptLimiter) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// sweepLocked 防止匿名请求把 map 撑大；只在条目较多时做一次全量清理。
func (l *AttemptLimiter) sweepLocked(now time.Time) {
	if len(l.entries) < 1024 {
		return
	}
	for k, e := range l.entries {
		if now.Sub(e.lastSeen) > l.window+l.lockout {
			delete(l.entries, k)
		}
	}
}
