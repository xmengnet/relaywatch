package auth

import (
	"testing"
	"time"
)

func TestAttemptLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := NewAttemptLimiter(2, time.Minute, 10*time.Minute)
	l.now = func() time.Time { return now }

	key := "login:127.0.0.1|alice"
	if !l.Allow(key) {
		t.Fatal("first attempt should be allowed")
	}
	l.Hit(key)
	if !l.Allow(key) {
		t.Fatal("second attempt should be allowed")
	}
	l.Hit(key)
	if l.Allow(key) {
		t.Fatal("third attempt should be blocked")
	}

	// 锁定 10 分钟；窗口 1 分钟已过，解锁后允许重新尝试
	now = now.Add(11 * time.Minute)
	if !l.Allow(key) {
		t.Fatal("after lockout should be allowed")
	}

	// 成功登录后 Reset 立即清除
	l.Hit(key)
	l.Hit(key)
	if l.Allow(key) {
		t.Fatal("should be blocked again")
	}
	l.Reset(key)
	if !l.Allow(key) {
		t.Fatal("reset should clear the record")
	}
}

func TestAttemptLimiterNilAndDisabled(t *testing.T) {
	var nilLimiter *AttemptLimiter
	if !nilLimiter.Allow("k") {
		t.Fatal("nil limiter should allow")
	}
	nilLimiter.Hit("k")
	nilLimiter.Reset("k")

	disabled := NewAttemptLimiter(0, time.Minute, time.Minute)
	for i := 0; i < 100; i++ {
		disabled.Hit("k")
	}
	if !disabled.Allow("k") {
		t.Fatal("limit<=0 should disable limiting")
	}
}
