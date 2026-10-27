package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// TokenHandler API Token 管理。
type TokenHandler struct {
	Svc *auth.Service
	St  store.Store
}

func (h *TokenHandler) routes(r chi.Router) {
	r.Get("/tokens", h.list)
	r.Post("/tokens", h.create)
	r.Delete("/tokens/{id}", h.revoke)
}

func (h *TokenHandler) list(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	tokens, err := h.St.ListAPITokens(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if tokens == nil {
		tokens = []*store.APIToken{}
	}
	out := make([]*tokenResponse, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toTokenResponse(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *TokenHandler) create(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	var req struct {
		Name      string `json:"name"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name 不能为空")
		return
	}
	var expires *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at 需要 RFC3339 格式")
			return
		}
		expires = &t
	}
	plain, _, err := h.Svc.CreateAPIToken(r.Context(), u.ID, req.Name, expires)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// 明文只返回这一次
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain})
}

func (h *TokenHandler) revoke(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	// 只允许操作自己的 token
	tokens, err := h.St.ListAPITokens(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	owned := false
	for _, t := range tokens {
		if t.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		writeError(w, http.StatusNotFound, "token 不存在")
		return
	}
	if err := h.St.RevokeAPIToken(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		writeInternalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
