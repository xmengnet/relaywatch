package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// MetricsHandler Prometheus 文本格式指标（简化实现，不引 client 库）。
// 挂 auth.RequireAuth（会话或 API Token）：运营计数不对匿名开放，
// 外部抓取器用 API Token 的 Authorization 头即可。
type MetricsHandler struct {
	St store.Store
}

// ServeHTTP 输出 /metrics。
func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if auth.UserFromContext(r.Context()) == nil {
		http.Error(w, `{"message":"未登录"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	var b strings.Builder

	// 一次 GROUP BY 拿全量状态分布，替代按状态逐个 COUNT 的 6 次查询
	counts, err := h.St.CountMonitorStates(ctx, 0)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	fmt.Fprintln(&b, "# HELP relaywatch_monitors_total 监控总数")
	fmt.Fprintln(&b, "# TYPE relaywatch_monitors_total gauge")
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", store.StateUpToDate, counts.UpToDate)
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", store.StateHasUpdate, counts.HasUpdate)
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", store.StateError, counts.Error)
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", store.StateMissing, counts.Missing)
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=%q} %d\n", store.StateUnknown, counts.Unknown)
	fmt.Fprintf(&b, "relaywatch_monitors_total{state=\"all\"} %d\n", counts.Total)

	enabled, err := h.St.CountEnabledMonitors(ctx)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	fmt.Fprintln(&b, "# HELP relaywatch_monitors_enabled 启用的监控数")
	fmt.Fprintln(&b, "# TYPE relaywatch_monitors_enabled gauge")
	fmt.Fprintf(&b, "relaywatch_monitors_enabled %d\n", enabled)

	_, pending, _ := h.St.ListDeliveries(ctx, 0, 0, store.DeliveryPending, 0, 1, 1)
	_, failed, _ := h.St.ListDeliveries(ctx, 0, 0, store.DeliveryFailed, 0, 1, 1)
	fmt.Fprintln(&b, "# HELP relaywatch_deliveries 投递队列")
	fmt.Fprintln(&b, "# TYPE relaywatch_deliveries gauge")
	fmt.Fprintf(&b, "relaywatch_deliveries{status=\"pending\"} %d\n", pending)
	fmt.Fprintf(&b, "relaywatch_deliveries{status=\"failed\"} %d\n", failed)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}
