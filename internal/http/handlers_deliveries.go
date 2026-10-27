package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"relaywatch/internal/notifier"
	"relaywatch/internal/store"
)

// DeliveryHandler 通知历史、重发。
type DeliveryHandler struct {
	St        store.Store
	Notifiers *notifier.Registry
	// ResendOne 由 main 注入：同步执行一条重发（渲染 + 发送）。
	ResendOne func(ctx context.Context, d *store.Delivery) error
}

func (h *DeliveryHandler) routes(r chiRouterIface) {
	r.Get("/deliveries", h.list)
	r.Post("/deliveries/{id}/resend", h.resend)
}

func (h *DeliveryHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	monitorID := atoi64(q.Get("monitor_id"))
	channelID := atoi64(q.Get("channel_id"))
	status := store.DeliveryStatus(q.Get("status"))
	page := atoiOr(q.Get("page"), 1)
	perPage := atoiOr(q.Get("per_page"), 25)

	items, total, err := h.St.ListDeliveries(r.Context(), monitorID, channelID, status, page, perPage)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]*deliveryResponse, 0, len(items))
	for _, d := range items {
		out = append(out, toDeliveryResponse(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// resend 手动重发：作废该变更其余待投递记录（需求 3.3），再立即投一条。
func (h *DeliveryHandler) resend(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	d, err := h.St.GetDelivery(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "投递记录不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if d.Status == store.DeliveryPending || d.Status == store.DeliveryDeferred {
		// 仍在队列中的记录不重复投递
		writeError(w, http.StatusConflict, "该记录仍在待投递队列中")
		return
	}
	if h.ResendOne == nil {
		writeError(w, http.StatusInternalServerError, "重发未配置")
		return
	}
	// 手动重发会作废同一变更周期的其它待投递记录
	if d.ChangeID != nil {
		if _, err := h.St.CancelOpenDeliveries(r.Context(), d.MonitorID, "manual resend"); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	// 新建一条 pending 立即投递的记录
	changeID := d.ChangeID
	nd := &store.Delivery{
		MonitorID:     d.MonitorID,
		ChannelID:     d.ChannelID,
		ChangeID:      changeID,
		Status:        store.DeliveryPending,
		NextAttemptAt: ptrTime(timeNow()),
	}
	if err := h.St.AddDelivery(r.Context(), nd); err != nil {
		writeInternalError(w, err)
		return
	}
	if h.ResendOne != nil {
		h.ResendOne(r.Context(), nd)
	}
	out, _ := h.St.GetDelivery(r.Context(), nd.ID)
	writeJSON(w, http.StatusOK, toDeliveryResponse(out))
}

func atoi64(s string) int64 {
	var n int64
	neg := false
	i := 0
	if s != "" && s[0] == '-' {
		neg = true
		i = 1
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int64(s[i]-'0')
	}
	if neg {
		return -n
	}
	return n
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return def
		}
		n = n*10 + int(s[i]-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func ptrTime(t time.Time) *time.Time { return &t }
