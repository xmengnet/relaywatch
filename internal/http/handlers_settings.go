package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"relaywatch/internal/store"
)

// SettingsHandler 运行时设置（系统告警渠道、注册开关等）。
type SettingsHandler struct {
	St store.Store
}

// 设置键：alert_channel_ids = JSON 数组
const settingAlertChannels = "alert_channel_ids"

func (h *SettingsHandler) routes(r chiRouterIface) {
	r.Get("/settings", h.get)
	// 写设置仅 admin
	r.Group(func(r chiRouterIface) {
		r.Use(requireAdminMiddleware)
		r.Put("/settings", h.put)
	})
}

// requireAdminMiddleware 包内 admin 守卫（与 auth.RequireAdmin 同语义，避免额外 import 环节）。
func requireAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := authUser(r)
		if u == nil || u.Role != "admin" {
			writeError(w, http.StatusForbidden, "权限不足")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type settingsResponse struct {
	AlertChannelIDs           []int64 `json:"alert_channel_ids"`
	RegistrationEnabled       bool    `json:"registration_enabled"`
	RegistrationDefaultRole   string  `json:"registration_default_role"`
	RegistrationRequireEmail  bool    `json:"registration_require_email"`
}

func (h *SettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	resp := settingsResponse{AlertChannelIDs: []int64{}, RegistrationDefaultRole: "user"}
	raw, err := h.St.GetSetting(r.Context(), settingAlertChannels)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &resp.AlertChannelIDs)
	}
	resp.RegistrationEnabled = registrationEnabled(r.Context(), h.St)
	if v, err := h.St.GetSetting(r.Context(), settingRegistrationRole); err == nil && v != "" {
		resp.RegistrationDefaultRole = v
	}
	if v, err := h.St.GetSetting(r.Context(), settingRegistrationNeedMail); err == nil {
		resp.RegistrationRequireEmail = v == "true"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var req settingsResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	role := req.RegistrationDefaultRole
	if role == "" {
		role = "user" // 未传时保持默认，避免部分更新被拒
	}
	if role != "user" && role != "viewer" {
		writeError(w, http.StatusBadRequest, "registration_default_role 只能是 user 或 viewer")
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
	if err := h.St.SetSetting(r.Context(), settingRegistrationEnabled, boolStr(req.RegistrationEnabled)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingRegistrationRole, role); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingRegistrationNeedMail, boolStr(req.RegistrationRequireEmail)); err != nil {
		writeInternalError(w, err)
		return
	}
	req.RegistrationDefaultRole = role
	writeJSON(w, http.StatusOK, req)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
