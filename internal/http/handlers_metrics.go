package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"relaywatch/internal/store"
)

// MetricsHandler Prometheus 文本格式指标（简化实现，不引 client 库）。
type MetricsHandler struct {
	St store.Store
}

// ServeHTTP 输出 /metrics。
func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var b strings.Builder
	fmt.Fprintln(&b, "# HELP relaywatch_monitors_total 监控总数")
	fmt.Fprintln(&b, "# TYPE relaywatch_monitors_total gauge")
	for _, st := range []store.MonitorState{
		store.StateUpToDate, store.StateHasUpdate, store.StateError, store.StateMissing, store.StateUnknown,
	} {
		_, n, err := h.St.ListMonitors(ctx, store.MonitorFilter{State: st, PerPage: 1})
		if err != nil {
			writeInternalError(w, err)
			return
		}
		fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", st, n)
	}
	_, total, _ := h.St.ListMonitors(ctx, store.MonitorFilter{PerPage: 1})
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=\"all\"} %d\n", total)

	enabled, _ := h.St.ListEnabledMonitors(ctx)
	fmt.Fprintln(&b, "# HELP relaywatch_monitors_enabled 启用的监控数")
	fmt.Fprintln(&b, "# TYPE relaywatch_monitors_enabled gauge")
	fmt.Fprintf(&b, "relaywatch_monitors_enabled %d\n", len(enabled))

	_, pending, _ := h.St.ListDeliveries(ctx, 0, 0, store.DeliveryPending, 1, 1)
	_, failed, _ := h.St.ListDeliveries(ctx, 0, 0, store.DeliveryFailed, 1, 1)
	fmt.Fprintln(&b, "# HELP relaywatch_deliveries 投递队列")
	fmt.Fprintln(&b, "# TYPE relaywatch_deliveries gauge")
	fmt.Fprintf(&b, "relaywatch_deliveries{status=\"pending\"} %d\n", pending)
	fmt.Fprintf(&b, "relaywatch_deliveries{status=\"failed\"} %d\n", failed)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}
