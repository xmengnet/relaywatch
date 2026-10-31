package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// OIDCHandler OIDC 登录路由 + Provider 管理。
type OIDCHandler struct {
	Svc           *auth.OIDCService
	St            store.Store
	SecureCookies bool // base_url 为 https 时强制 Secure Cookie
}

func (h *OIDCHandler) routesPublic(r chi.Router) {
	// 服务端 302 跳转，不走 /api/v1（SPA 无法处理 302 到 IdP）。
	// callback 登录与账号绑定共用（state 区分流程），IdP 侧只需注册一个回调地址。
	r.Get("/auth/oidc/{name}", h.begin)
	r.Get("/auth/oidc/{name}/callback", h.callback)
	r.Get("/auth/oidc/{name}/bind", h.bindBegin) // 绑定发起，需登录态
}

func (h *OIDCHandler) routesAdmin(r chiRouterIface) {
	// 静态段 all 优先于 {id} 匹配；管理页全量列表，勿与匿名 GET /auth/providers 混用
	r.With(auth.RequireAdmin).Get("/auth/providers/all", h.listAll)
	r.With(auth.RequireAdmin).Get("/auth/providers/{id}", h.getOne)
	r.With(auth.RequireAdmin).Get("/auth/oidc/callback-url", h.callbackBase)
	// 写接口同样仅 admin：普通用户可改 issuer 劫持全站 OIDC 登录/绑定流程
	r.With(auth.RequireAdmin).Post("/auth/providers", h.create)
	r.With(auth.RequireAdmin).Patch("/auth/providers/{id}", h.update)
	r.With(auth.RequireAdmin).Delete("/auth/providers/{id}", h.remove)
}

// routesAuth 绑定确认端点：所有登录用户可用（挂在 RequireAuth+RequireCSRF 组）。
func (h *OIDCHandler) routesAuth(r chiRouterIface) {
	r.Get("/auth/bind/pending", h.bindPending)
	r.Post("/auth/bind/confirm", h.bindConfirm)
}

// callbackBase GET /auth/oidc/callback-url（admin）：回调地址的 base_url 部分，
// 前端拼 /auth/oidc/{name}/callback 展示给用户配置 IdP。
func (h *OIDCHandler) callbackBase(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"base_url": h.Svc.BaseURL})
}

// begin GET /auth/oidc/{name} → 302 到 IdP。
func (h *OIDCHandler) begin(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	url, err := h.Svc.Begin(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// bindBegin GET /auth/oidc/{name}/bind：已登录用户发起绑定，302 到 IdP。
// 回调统一走 /auth/oidc/{name}/callback（state 区分流程），匿名访问直接回登录页。
func (h *OIDCHandler) bindBegin(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	url, err := h.Svc.BeginBind(r.Context(), chi.URLParam(r, "name"), u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// callback GET /auth/oidc/{name}/callback → 登录建会话或完成账号绑定 → 302 回前端。
// 两种流程共用此端点，由 pending 中的 state 区分。
func (h *OIDCHandler) callback(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	state := r.URL.Query().Get("state")
	// IdP 回传错误（如用户取消授权）：按流程类型回对应页面。
	// state 一次性：错误回跳同样消费，防止 TTL 内被携带合法 code 复用。
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		if h.Svc.TakePendingIsBind(state) {
			http.Redirect(w, r, "/profile?oidc_error=bind_failed", http.StatusFound)
		} else {
			http.Redirect(w, r, "/login?error="+errCode, http.StatusFound)
		}
		return
	}
	u, bound, confirm, err := h.Svc.Finish(r.Context(), name, state, r.URL.Query().Get("code"))
	if err != nil {
		if bound {
			code := "bind_failed"
			if errors.Is(err, auth.ErrOIDCTaken) {
				code = "bind_taken"
			}
			http.Redirect(w, r, "/profile?oidc_error="+code, http.StatusFound)
			return
		}
		code := "oidc_failed"
		if errors.Is(err, auth.ErrOIDCNotBound) {
			code = "oidc_unbound"
		}
		http.Redirect(w, r, "/login?error="+code, http.StatusFound)
		return
	}
	if bound {
		// 绑定不直接生效：回个人资料页展示确认 UI，由当前会话用户确认后落库
		if confirm != nil {
			http.Redirect(w, r, "/profile?bind_confirm="+confirm.Token, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/profile?oidc_bound=1", http.StatusFound)
		return
	}
	sess, err := h.Svc.SessionFor(r.Context(), u.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		slog.Error("oidc session create failed", "provider", name, "err", err)
		http.Redirect(w, r, "/login?error=oidc_failed", http.StatusFound)
		return
	}
	secure := secureCookie(r, h.SecureCookies)
	auth.SetSessionCookie(w, sess.ID, int(sess.ExpiresAt.Sub(sess.CreatedAt).Seconds()), secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

// bindPending GET /auth/bind/pending?token=：查询待确认绑定（校验归属当前用户）。
func (h *OIDCHandler) bindPending(w http.ResponseWriter, r *http.Request) {
	info, err := h.Svc.BindConfirmInfo(authUser(r).ID, r.URL.Query().Get("token"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": info.Provider, "identity": info.Identity})
}

// bindConfirm POST /auth/bind/confirm {token}：当前用户确认绑定生效。
func (h *OIDCHandler) bindConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.Svc.ConfirmBind(r.Context(), req.Token, authUser(r).ID); err != nil {
		if errors.Is(err, auth.ErrOIDCTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// list 公开：登录页渲染按钮（只暴露 name/label）。
func (h *OIDCHandler) list(w http.ResponseWriter, r *http.Request) {
	providers, err := h.St.ListOIDCProviders(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := []map[string]any{}
	for _, p := range providers {
		if p.Enabled {
			out = append(out, map[string]any{"name": p.Name, "icon_url": p.IconURL})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// listAll GET /auth/providers/all（admin）：管理页用的全量列表，含 id/enabled，
// 停用的也返回；secret 永不回传。登录页匿名列表 GET /auth/providers 只回 name/icon_url，
// 两者不可混用（管理页曾误用匿名列表导致编辑/删除因缺 id 静默失效）。
func (h *OIDCHandler) listAll(w http.ResponseWriter, r *http.Request) {
	providers, err := h.St.ListOIDCProviders(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "issuer": p.Issuer,
			"client_id": p.ClientID, "scopes": p.Scopes,
			"claim_rules": p.ClaimRules, "icon_url": p.IconURL, "enabled": p.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// getOne GET /auth/providers/{id}（admin）：编辑表单用的完整信息（secret 不返回）。
func (h *OIDCHandler) getOne(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	prov, err := h.St.GetOIDCProvider(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Provider 不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": prov.ID, "name": prov.Name, "issuer": prov.Issuer,
		"client_id": prov.ClientID, "scopes": prov.Scopes,
		"claim_rules": prov.ClaimRules, "icon_url": prov.IconURL, "enabled": prov.Enabled,
	})
}

type providerPayload struct {
	Name         string          `json:"name"`
	Issuer       string          `json:"issuer"`
	ClientID     string          `json:"client_id"`
	ClientSecret json.RawMessage `json:"client_secret"` // PATCH 时可省略 = 不改
	Scopes       string          `json:"scopes"`
	ClaimRules   string          `json:"claim_rules"`
	IconURL      string          `json:"icon_url"`
	Enabled      *bool           `json:"enabled"`
}

func (p *providerPayload) validate() error {
	if p.Name == "" || p.Issuer == "" || p.ClientID == "" {
		return errors.New("name、issuer、client_id 必填")
	}
	if strings.ContainsAny(p.Name, "/?#") {
		return errors.New("name 不能包含 URL 特殊字符")
	}
	// 与 auth.normalizeIssuer 同规则：入库前规范化，避免尾斜杠导致 go-oidc 严格比对失败
	p.Issuer = strings.TrimRight(strings.TrimSpace(p.Issuer), "/")
	return nil
}

func (h *OIDCHandler) create(w http.ResponseWriter, r *http.Request) {
	var p providerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sec := trimJSONString(string(p.ClientSecret))
	if sec == "" {
		writeError(w, http.StatusBadRequest, "client_secret 必填")
		return
	}
	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	scopes := p.Scopes
	if scopes == "" {
		scopes = "openid email profile"
	}
	prov := &store.OIDCProvider{
		Name:         p.Name,
		Issuer:       p.Issuer,
		ClientID:     p.ClientID,
		ClientSecret: sec,
		Scopes:       scopes,
		ClaimRules:   p.ClaimRules,
		IconURL:      strings.TrimSpace(p.IconURL),
		Enabled:      enabled,
	}
	if err := h.St.CreateOIDCProvider(r.Context(), prov); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": prov.ID})
}

// trimJSONString 处理 client_secret 可能带引号的情况。
func trimJSONString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

func (h *OIDCHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	prov, err := h.St.GetOIDCProvider(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Provider 不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var p providerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := p.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prov.Name = p.Name
	prov.Issuer = p.Issuer
	prov.ClientID = p.ClientID
	if sec := trimJSONString(string(p.ClientSecret)); sec != "" && string(p.ClientSecret) != "null" {
		prov.ClientSecret = sec
	}
	if p.Scopes != "" {
		prov.Scopes = p.Scopes
	}
	prov.ClaimRules = p.ClaimRules
	prov.IconURL = strings.TrimSpace(p.IconURL)
	if p.Enabled != nil {
		prov.Enabled = *p.Enabled
	}
	if err := h.St.UpdateOIDCProvider(r.Context(), prov); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *OIDCHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.St.DeleteOIDCProvider(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
