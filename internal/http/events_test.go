package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBrokerBroadcast(t *testing.T) {
	broker := NewEventBroker()

	ch, unsub := broker.subscribe()
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

func TestEventBrokerHTTPStreaming(t *testing.T) {
	broker := NewEventBroker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	go func() {
		time.Sleep(50 * time.Millisecond)
		broker.Broadcast("check.done", map[string]any{"monitor_id": 42, "ok": true})
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
