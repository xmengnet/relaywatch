package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"relaywatch/internal/check"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// CheckHandler 手动检查、历史、版本变更、源元数据。
type CheckHandler struct {
	St       store.Store
	Checker  *check.Checker
	Registry *source.Registry
}

func (h *CheckHandler) routes(r chiRouterIface) {
	r.Get("/sources/types", h.sourceTypes)
	r.Post("/monitors/{id}/check", h.checkNow)
	r.Post("/monitors/{id}/ack", h.ack)
	r.Get("/monitors/{id}/history", h.history)
	r.Get("/monitors/{id}/changes", h.changes)
	r.Post("/monitors/validate", h.validateSource)
}

func (h *CheckHandler) sourceTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.Registry.Metas()})
}

// canAccessMonitorID 按 id 校验监控归属，失败已写 404 响应。
func (h *CheckHandler) canAccessMonitorID(w http.ResponseWriter, r *http.Request, id int64) bool {
	m, err := h.St.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return false
	}
	if err != nil {
		writeInternalError(w, err)
		return false
	}
	if !canAccessMonitor(authUser(r), m) {
		writeError(w, http.StatusNotFound, "监控不存在") // 404 语义，防枚举
		return false
	}
	return true
}

// checkNow 同步执行检查并返回结果（需求 8.5.3）。
func (h *CheckHandler) checkNow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := h.St.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccessMonitor(authUser(r), m) {
		writeError(w, http.StatusNotFound, "监控不存在") // 404 语义，防枚举
		return
	}
	if !m.Enabled {
		writeError(w, http.StatusBadRequest, "监控已停用")
		return
	}
	out := h.Checker.Check(r.Context(), m)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          out.OK,
		"version":     out.Version,
		"state":       string(out.State),
		"error":       out.Error,
		"duration_ms": out.DurationMS,
		"skipped":     out.Skipped,
	})
}

// ack 确认更新：开放变更标记已确认，监控回到 up_to_date。
func (h *CheckHandler) ack(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := authUser(r)
	m, err := h.St.GetMonitor(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !canAccessMonitor(u, m) {
		writeError(w, http.StatusNotFound, "监控不存在")
		return
	}
	open, err := h.St.OpenVersionChange(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "没有待确认的更新")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	now := timeNow()
	uid := u.ID
	open.AcknowledgedAt = &now
	open.AcknowledgedBy = &uid
	if err := h.St.UpdateVersionChange(r.Context(), open); err != nil {
		writeInternalError(w, err)
		return
	}
	// 作废该变更周期的待投递记录（需求 3.3 静默期交互）
	if _, err := h.St.CancelOpenDeliveries(r.Context(), id, "acknowledged by user"); err != nil {
		slog.Error("cancel deliveries on ack", "err", err)
	}
	// ack 后监控回到 up_to_date，基线提升到 latest
	m.State = store.StateUpToDate
	m.CurrentVersion = m.LatestVersion
	if err := h.St.UpdateMonitor(r.Context(), m); err != nil {
		writeInternalError(w, err)
		return
	}
	_ = h.St.AddAudit(r.Context(), &store.AuditEntry{
		UserID: &uid, Action: "monitor.ack", Target: m.Name,
	})
	writeJSON(w, http.StatusOK, toMonitorResponse(m))
}

func (h *CheckHandler) history(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !h.canAccessMonitorID(w, r, id) {
		return
	}
	page, perPage := pageParams(r)
	items, total, err := h.St.ListCheckHistory(r.Context(), id, page, perPage)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]*checkHistoryResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toCheckHistoryResponse(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

func (h *CheckHandler) changes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if !h.canAccessMonitorID(w, r, id) {
		return
	}
	page, perPage := pageParams(r)
	items, total, err := h.St.ListVersionChanges(r.Context(), id, page, perPage)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]*versionChangeResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toVersionChangeResponse(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// validateSource 校验源配置（保存前前端调用）。
func (h *CheckHandler) validateSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceType   string         `json:"source_type"`
		SourceConfig map[string]any `json:"source_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	src, ok := h.Registry.Get(req.SourceType)
	if !ok {
		writeError(w, http.StatusBadRequest, "未知源类型: "+req.SourceType)
		return
	}
	if err := src.Validate(req.SourceConfig); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true})
}
