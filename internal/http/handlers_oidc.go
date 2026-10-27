package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// OIDCHandler OIDC 登录路由 + Provider 管理。
type OIDCHandler struct {
	Svc *auth.OIDCService
	St  store.Store
}

func (h *OIDCHandler) routesPublic(r chi.Router) {
	// 服务端 302 跳转，不走 /api/v1（SPA 无法处理 302 到 IdP）
	r.Get("/auth/oidc/{name}", h.begin)
	r.Get("/auth/oidc/{name}/callback", h.callback)
}

func (h *OIDCHandler) routesAdmin(r chiRouterIface) {
	r.Post("/auth/providers", h.create)
	r.Patch("/auth/providers/{id}", h.update)
	r.Delete("/auth/providers/{id}", h.remove)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	secure := r.TLS != nil
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
			out = append(out, map[string]any{"name": p.Name})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

type providerPayload struct {
	Name         string          `json:"name"`
	Issuer       string          `json:"issuer"`
	ClientID     string          `json:"client_id"`
	ClientSecret json.RawMessage `json:"client_secret"` // PATCH 时可省略 = 不改
	Scopes       string          `json:"scopes"`
	ClaimRules   string          `json:"claim_rules"`
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
	secret, _ := p.ClientSecret, error(nil)
	_ = secret
	sec := string(p.ClientSecret)
	sec = trimJSONString(sec)
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
