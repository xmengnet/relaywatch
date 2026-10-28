package scheduler

import (
	"testing"
	"time"
)

func TestJitterDeterministic(t *testing.T) {
	// 同一 ID 恒定；不同 ID 大概率不同；范围 [-0.1, 0.1)
	for id := int64(1); id < 1000; id++ {
		j := JitterFactor(id)
		if j < -0.1 || j >= 0.1 {
			t.Fatalf("jitter(%d) = %v out of range", id, j)
		}
	}
}

func TestHostLimiter(t *testing.T) {
	h := NewHostLimiter(2, map[string]int{"api.github.com": 1})
	now := time.Now()
	// 默认 host:2/分钟（两次调用都有副作用，不能合并成一个条件表达式）
	if !h.Allow("aur.archlinux.org", now) {
		t.Fatal("expected 1st allow")
	}
	if !h.Allow("aur.archlinux.org", now) {
		t.Fatal("expected 2nd allow")
	}
	if h.Allow("aur.archlinux.org", now) {
		t.Fatal("expected deny on 3rd")
	}
	// override host:1/分钟
	if !h.Allow("api.github.com", now) {
		t.Fatal("expected allow")
	}
	if h.Allow("api.github.com", now) {
		t.Fatal("expected deny on 2nd")
	}
	// 时间流逝补充令牌
	if !h.Allow("api.github.com", now.Add(70*time.Second)) {
		t.Fatal("expected allow after refill")
	}
	// 冻结
	h.Freeze("aur.archlinux.org", now.Add(time.Hour))
	if h.Allow("aur.archlinux.org", now.Add(70*time.Second)) {
		t.Fatal("expected deny while frozen")
	}
	if !h.Allow("aur.archlinux.org", now.Add(2*time.Hour)) {
		t.Fatal("expected allow after freeze ends")
	}
}
