package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEMessage 代表一条向浏览器推送的 SSE 消息。
type SSEMessage struct {
	Event string
	Data  string
}

// EventBroker 是进程内 SSE 事件广播器，维护所有活跃的长连接客户端。
type EventBroker struct {
	mu      sync.RWMutex
	clients map[chan SSEMessage]struct{}
}

// NewEventBroker 构造事件广播器。
func NewEventBroker() *EventBroker {
	return &EventBroker{
		clients: make(map[chan SSEMessage]struct{}),
	}
}

// Broadcast 向所有在线客户端广播一条命名事件。
func (b *EventBroker) Broadcast(event string, payload any) {
	var dataStr string
	switch v := payload.(type) {
	case string:
		dataStr = v
	case []byte:
		dataStr = string(v)
	default:
		bs, _ := json.Marshal(payload)
		dataStr = string(bs)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	msg := SSEMessage{Event: event, Data: dataStr}
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// 慢客户端通道已满，非阻塞跳过，防止阻塞全局广播
		}
	}
}

func (b *EventBroker) subscribe() (chan SSEMessage, func()) {
	ch := make(chan SSEMessage, 16)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.clients, ch)
		close(ch)
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

// ClientCount 返回当前连接的客户端数。
func (b *EventBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// ServeHTTP 处理 GET /api/v1/events 长连接请求。
func (b *EventBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Nginx 禁用缓冲直传

	ch, unsub := b.subscribe()
	defer unsub()

	// 握手确认
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	// 15 秒心跳保活
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Event, msg.Data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
