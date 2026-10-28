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
	// 服务端 302 跳转，不走 /api/v1（SPA 无法处理 302 到 IdP）
	r.Get("/auth/oidc/{name}", h.begin)
	r.Get("/auth/oidc/{name}/callback", h.callback)
}

func (h *OIDCHandler) routesAdmin(r chiRouterIface) {
	r.With(auth.RequireAdmin).Get("/auth/providers/{id}", h.getOne)
	r.With(auth.RequireAdmin).Get("/auth/oidc/callback-url", h.callbackBase)
	r.Post("/auth/providers", h.create)
	r.Patch("/auth/providers/{id}", h.update)
	r.Delete("/auth/providers/{id}", h.remove)
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

// callback GET /auth/oidc/{name}/callback → 建会话 → 302 回前端。
func (h *OIDCHandler) callback(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		desc := r.URL.Query().Get("error_description")
		http.Redirect(w, r, "/login?error="+errCode, http.StatusFound)
		void(desc)
		return
	}
	u, err := h.Svc.Complete(r.Context(), name, r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		http.Redirect(w, r, "/login?error=oidc_failed", http.StatusFound)
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

func void(...any) {}

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
