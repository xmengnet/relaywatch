package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"relaywatch/internal/store"
)

// SettingsHandler 运行时设置（系统告警渠道等）。
type SettingsHandler struct {
	St store.Store
}

// 设置键：alert_channel_ids = JSON 数组
const settingAlertChannels = "alert_channel_ids"

func (h *SettingsHandler) routes(r chiRouterIface) {
	r.Get("/settings", h.get)
	r.Put("/settings", h.put)
}

type settingsResponse struct {
	AlertChannelIDs []int64 `json:"alert_channel_ids"`
}

func (h *SettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	resp := settingsResponse{AlertChannelIDs: []int64{}}
	raw, err := h.St.GetSetting(r.Context(), settingAlertChannels)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &resp.AlertChannelIDs)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var req settingsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.AlertChannelIDs == nil {
		req.AlertChannelIDs = []int64{}
	}
	raw, _ := json.Marshal(req.AlertChannelIDs)
	if err := h.St.SetSetting(r.Context(), settingAlertChannels, string(raw)); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// AlertChannelIDs 读取告警渠道配置（AlertSender 用）。
func AlertChannelIDs(ctx context.Context, st store.Store) []int64 {
	raw, err := st.GetSetting(ctx, settingAlertChannels)
	if err != nil || raw == "" {
		return nil
	}
	var ids []int64
	_ = json.Unmarshal([]byte(raw), &ids)
	return ids
}
