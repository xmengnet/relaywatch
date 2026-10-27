package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

func TestEventBrokerBroadcast(t *testing.T) {
	broker := NewEventBroker()

	ch, unsub := broker.subscribe(1, false)
	defer unsub()

	if broker.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", broker.ClientCount())
	}

	broker.Broadcast("test.event", map[string]string{"foo": "bar"})

	select {
	case msg := <-ch:
		if msg.Event != "test.event" {
			t.Errorf("got event %q, want test.event", msg.Event)
		}
		if !strings.Contains(msg.Data, `"foo":"bar"`) {
			t.Errorf("unexpected data: %s", msg.Data)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for SSE message")
	}
}

// 归属过滤：普通用户只收自己的监控事件，admin 收全量，无主事件全员可见。
func TestEventBrokerOwnerFilter(t *testing.T) {
	broker := NewEventBroker()
	broker.OwnerOf = func(monitorID int64) (int64, bool) {
		// 监控 42 属于用户 7，监控 99 查不到
		if monitorID == 42 {
			return 7, true
		}
		return 0, false
	}

	aliceCh, aliceUnsub := broker.subscribe(7, false) // owner
	bobCh, bobUnsub := broker.subscribe(8, false)     // 他人
	adminCh, adminUnsub := broker.subscribe(1, true)  // admin
	defer func() { aliceUnsub(); bobUnsub(); adminUnsub() }()

	broker.BroadcastMonitor("check.done", 42, map[string]any{"ok": true})

	// alice 收到
	select {
	case <-aliceCh:
	default:
		t.Fatal("owner should receive monitor event")
	}
	// admin 收到
	select {
	case <-adminCh:
	default:
		t.Fatal("admin should receive monitor event")
	}
	// bob 收不到
	select {
	case msg := <-bobCh:
		t.Fatalf("bob should NOT receive, got %+v", msg)
	default:
	}

	// 公共事件全员可见
	broker.Broadcast("delivery.done", map[string]any{"status": "sent"})
	for name, ch := range map[string]<-chan SSEMessage{"alice": aliceCh, "bob": bobCh, "admin": adminCh} {
		select {
		case <-ch:
		default:
			t.Fatalf("%s should receive public event", name)
		}
	}

	// 查不到 owner 的事件按公共处理（不丢消息）
	broker.BroadcastMonitor("check.done", 99, map[string]any{"ok": false})
	select {
	case <-bobCh:
	default:
		t.Fatal("unresolvable owner event should fall through to all")
	}
}

func TestEventBrokerHTTPStreaming(t *testing.T) {
	broker := NewEventBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 模拟已登录用户（ServeHTTP 从 ctx 取身份）
	u := &store.User{ID: 7, Username: "alice", Role: "user"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).
		WithContext(auth.WithUser(ctx, u, nil))
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.BroadcastMonitor("check.done", 42, map[string]any{"monitor_id": 42, "ok": true})
		time.Sleep(50 * time.Millisecond)
		cancel() // 结束客户端连接
	}()

	broker.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Errorf("missing connected event: %s", body)
	}
	if !strings.Contains(body, "event: check.done") || !strings.Contains(body, `"monitor_id":42`) {
		t.Errorf("missing check.done event: %s", body)
	}
}
