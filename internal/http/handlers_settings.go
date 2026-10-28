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
	// 读与写都仅 admin：settings 含告警渠道归属、注册开关等系统级配置，
	// 普通用户不应看到（M8 起「设置」整页仅 admin 可达，接口同步收紧）
	r.Route("/settings", func(r chiRouterIface) {
		r.Use(requireAdminMiddleware)
		r.Get("/", h.get)
		r.Put("/", h.put)
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
	AlertChannelIDs          []int64 `json:"alert_channel_ids"`
	RegistrationEnabled      bool    `json:"registration_enabled"`
	RegistrationDefaultRole  string  `json:"registration_default_role"`
	RegistrationRequireEmail bool    `json:"registration_require_email"`
}

// settingsPatch 是 PUT 的请求体：指针字段用于区分「未传」与「显式设为零值」，
// 避免局部更新把其他设置覆盖回默认值。
type settingsPatch struct {
	AlertChannelIDs          *[]int64 `json:"alert_channel_ids"`
	RegistrationEnabled      *bool    `json:"registration_enabled"`
	RegistrationDefaultRole  *string  `json:"registration_default_role"`
	RegistrationRequireEmail *bool    `json:"registration_require_email"`
}

// load 读取当前设置并补齐默认值。
func (h *SettingsHandler) load(ctx context.Context) settingsResponse {
	resp := settingsResponse{AlertChannelIDs: []int64{}, RegistrationDefaultRole: "user"}
	raw, err := h.St.GetSetting(ctx, settingAlertChannels)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &resp.AlertChannelIDs)
	}
	resp.RegistrationEnabled = registrationEnabled(ctx, h.St)
	if v, err := h.St.GetSetting(ctx, settingRegistrationRole); err == nil && v != "" {
		resp.RegistrationDefaultRole = v
	}
	if v, err := h.St.GetSetting(ctx, settingRegistrationNeedMail); err == nil {
		resp.RegistrationRequireEmail = v == "true"
	}
	return resp
}

func (h *SettingsHandler) get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.load(r.Context()))
}

func (h *SettingsHandler) put(w http.ResponseWriter, r *http.Request) {
	var req settingsPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// 先读后写：只覆盖请求里出现的字段，未出现的保持原值。
	cur := h.load(r.Context())
	if req.AlertChannelIDs != nil {
		cur.AlertChannelIDs = *req.AlertChannelIDs
	}
	if req.RegistrationEnabled != nil {
		cur.RegistrationEnabled = *req.RegistrationEnabled
	}
	if req.RegistrationDefaultRole != nil {
		cur.RegistrationDefaultRole = *req.RegistrationDefaultRole
	}
	if req.RegistrationRequireEmail != nil {
		cur.RegistrationRequireEmail = *req.RegistrationRequireEmail
	}
	if cur.RegistrationDefaultRole == "" {
		cur.RegistrationDefaultRole = "user"
	}
	if cur.RegistrationDefaultRole != "user" && cur.RegistrationDefaultRole != "viewer" {
		writeError(w, http.StatusBadRequest, "registration_default_role 只能是 user 或 viewer")
		return
	}
	if cur.AlertChannelIDs == nil {
		cur.AlertChannelIDs = []int64{}
	}

	raw, _ := json.Marshal(cur.AlertChannelIDs)
	if err := h.St.SetSetting(r.Context(), settingAlertChannels, string(raw)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingRegistrationEnabled, boolStr(cur.RegistrationEnabled)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingRegistrationRole, cur.RegistrationDefaultRole); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingRegistrationNeedMail, boolStr(cur.RegistrationRequireEmail)); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
