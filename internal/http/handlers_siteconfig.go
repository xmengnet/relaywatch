package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// SiteConfigHandler 「网站管理」：源站点默认值与 API 镜像地址，仅 admin。
type SiteConfigHandler struct {
	St store.Store
}

const (
	settingSiteMirrors  = "site.mirrors"       // JSON: source.MirrorRule[]
	settingSiteHostDefs = "site.host_defaults" // JSON: source.HostDefault[]
)

func (h *SiteConfigHandler) routes(r chiRouterIface) {
	r.Get("/site-config", h.get)
	r.Put("/site-config", h.put)
}

type siteConfigResponse struct {
	Mirrors  []source.MirrorRule  `json:"mirrors"`
	HostDefs []source.HostDefault `json:"host_defaults"`
}

func (h *SiteConfigHandler) get(w http.ResponseWriter, r *http.Request) {
	resp := siteConfigResponse{Mirrors: []source.MirrorRule{}, HostDefs: []source.HostDefault{}}
	if raw, err := h.St.GetSetting(r.Context(), settingSiteMirrors); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &resp.Mirrors)
	}
	if raw, err := h.St.GetSetting(r.Context(), settingSiteHostDefs); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &resp.HostDefs)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SiteConfigHandler) put(w http.ResponseWriter, r *http.Request) {
	var req siteConfigResponse
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Mirrors == nil {
		req.Mirrors = []source.MirrorRule{}
	}
	if req.HostDefs == nil {
		req.HostDefs = []source.HostDefault{}
	}
	// 校验镜像规则与 host 格式
	for _, m := range req.Mirrors {
		if _, _, err := source.RewriteURLInput(m.From, m.To); err != nil {
			writeError(w, http.StatusBadRequest, "镜像规则无效: "+m.From+" → "+m.To)
			return
		}
	}
	for _, d := range req.HostDefs {
		if d.Host != "" && !isHTTPURL(d.Host) {
			writeError(w, http.StatusBadRequest, "站点默认值无效: "+d.Host)
			return
		}
	}
	rawM, _ := json.Marshal(req.Mirrors)
	rawD, _ := json.Marshal(req.HostDefs)
	if err := h.St.SetSetting(r.Context(), settingSiteMirrors, string(rawM)); err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.SetSetting(r.Context(), settingSiteHostDefs, string(rawD)); err != nil {
		writeInternalError(w, err)
		return
	}
	// 立即生效（调度器与 handler 共用包级配置）
	source.SetSiteConfig(req.Mirrors, req.HostDefs)
	h.audit(r, "site_config.update")
	writeJSON(w, http.StatusOK, req)
}

// LoadSiteConfig 启动时从 settings 加载站点配置到包级内存（main 调用）。
func LoadSiteConfig(ctx context.Context, st store.Store) {
	var mirrors []source.MirrorRule
	var defs []source.HostDefault
	if raw, err := st.GetSetting(ctx, settingSiteMirrors); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &mirrors)
	}
	if raw, err := st.GetSetting(ctx, settingSiteHostDefs); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &defs)
	}
	source.SetSiteConfig(mirrors, defs)
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (h *SiteConfigHandler) audit(r *http.Request, action string) {
	u := authUser(r)
	_ = h.St.AddAudit(r.Context(), &store.AuditEntry{UserID: &u.ID, Action: action, IP: clientIP(r)})
}
