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
	Owner *int64 // 事件所属资源 owner；nil = 公共事件（全员可见）
}

// ownerResolverFunc 由 Server 注入：monitorID → ownerID。
// 查询失败返回 (0, false)，此时事件按公共处理（宁可多发不漏发 admin 场景由过滤兜底）。
type ownerResolverFunc func(ctxValidID int64) (int64, bool)

// EventBroker 是进程内 SSE 事件广播器，维护所有活跃的长连接客户端。
// M8 多用户：每个订阅者带身份，普通用户只收自己资源的事件，admin 收全量。
type EventBroker struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	// OwnerOf 由 main 注入（monitorID → ownerID）；nil 时全部事件视为公共。
	OwnerOf ownerResolverFunc
}

type client struct {
	ch      chan SSEMessage
	userID  int64
	isAdmin bool
}

// NewEventBroker 构造事件广播器。
func NewEventBroker() *EventBroker {
	return &EventBroker{
		clients: make(map[*client]struct{}),
	}
}

// BroadcastMonitor 向在线客户端广播一条与某监控相关的事件：
// admin 收全量，普通用户仅在自己是该监控 owner 时收到。
func (b *EventBroker) BroadcastMonitor(event string, monitorID int64, payload any) {
	var owner *int64
	if b.OwnerOf != nil {
		if oid, ok := b.OwnerOf(monitorID); ok {
			owner = &oid
		}
	}
	b.broadcast(event, owner, payload)
}

// Broadcast 向所有在线客户端广播公共事件（全员可见）。
func (b *EventBroker) Broadcast(event string, payload any) {
	b.broadcast(event, nil, payload)
}

func (b *EventBroker) broadcast(event string, owner *int64, payload any) {
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
	msg := SSEMessage{Event: event, Data: dataStr, Owner: owner}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for c := range b.clients {
		// 归属过滤：事件有 owner 且订阅者非 admin、非 owner → 跳过
		if msg.Owner != nil && !c.isAdmin && c.userID != *msg.Owner {
			continue
		}
		select {
		case c.ch <- msg:
		default:
			// 慢客户端通道已满，非阻塞跳过，防止阻塞全局广播
		}
	}
}

func (b *EventBroker) subscribe(userID int64, isAdmin bool) (chan SSEMessage, func()) {
	ch := make(chan SSEMessage, 16)
	c := &client{ch: ch, userID: userID, isAdmin: isAdmin}
	b.mu.Lock()
	b.clients[c] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.clients, c)
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

// ServeHTTP 处理 GET /api/v1/events 长连接请求（RequireAuth 已保证登录）。
func (b *EventBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	u := authUser(r)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Nginx 禁用缓冲直传

	ch, unsub := b.subscribe(u.ID, u.Role == "admin")
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
