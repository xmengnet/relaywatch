package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// AuthHandler 登录、登出、首次引导、状态查询。
type AuthHandler struct {
	Svc   *auth.Service
	Store store.Store
}

const (
	settingRegistrationEnabled  = "registration.enabled"
	settingRegistrationRole     = "registration.default_role"
	settingRegistrationNeedMail = "registration.require_email"
)

// registrationEnabled 注册开关（默认关闭）。
func registrationEnabled(ctx context.Context, st store.Store) bool {
	v, err := st.GetSetting(ctx, settingRegistrationEnabled)
	return err == nil && v == "true"
}

// registrationDefaultRole 注册默认角色（user | viewer，默认 user）。
func registrationDefaultRole(ctx context.Context, st store.Store) string {
	v, err := st.GetSetting(ctx, settingRegistrationRole)
	if err == nil && (v == "user" || v == "viewer") {
		return v
	}
	return "user"
}

// status 返回是否需要初始化、当前用户、CSRF token。匿名可达。
func (h *AuthHandler) status(w http.ResponseWriter, r *http.Request) {
	has, err := h.Svc.HasUsers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	resp := map[string]any{
		"setup_required":    !has,
		"user":              nil,
		"csrf_token":        "",
		"registration_open": h.Store != nil && registrationEnabled(r.Context(), h.Store),
	}
	if u := auth.UserFromContext(r.Context()); u != nil {
		resp["user"] = toUserResponse(u)
		if sess := auth.SessionFromContext(r.Context()); sess != nil {
			resp["csrf_token"] = sess.CSRFToken
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "用户名不能为空，密码至少 8 个字符")
		return
	}
	u, err := h.Svc.Setup(r.Context(), req.Username, req.Password)
	if errors.Is(err, auth.ErrSetupClosed) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	sess, err := h.newSession(w, r, u)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(u), "csrf_token": sess.CSRFToken})
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u, sess, err := h.Svc.Login(r.Context(), req.Username, req.Password, r.UserAgent(), clientIP(r))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	secure := r.TLS != nil
	auth.SetSessionCookie(w, sess.ID, int(sess.ExpiresAt.Sub(sess.CreatedAt).Seconds()), secure)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(u), "csrf_token": sess.CSRFToken})
}

func (h *AuthHandler) newSession(w http.ResponseWriter, r *http.Request, u *store.User) (*store.Session, error) {
	sess, err := h.Svc.CreateSessionFor(r.Context(), u.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		return nil, err
	}
	secure := r.TLS != nil
	auth.SetSessionCookie(w, sess.ID, int(sess.ExpiresAt.Sub(sess.CreatedAt).Seconds()), secure)
	return sess, nil
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		_ = h.Svc.Logout(r.Context(), sess.ID)
	}
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AuthHandler) csrf(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "未登录")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"csrf_token": sess.CSRFToken})
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return v
	}
	return r.RemoteAddr
}

// register 自助注册：仅 registration.enabled 时开放；关闭时 404 防探测。
func (h *AuthHandler) register(w http.ResponseWriter, r *http.Request) {
	if !registrationEnabled(r.Context(), h.Store) {
		writeError(w, http.StatusNotFound, "未开放注册")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "用户名不能为空，密码至少 8 个字符")
		return
	}
	if _, err := h.Store.UserByUsername(r.Context(), req.Username); err == nil {
		writeError(w, http.StatusConflict, "用户名已被占用")
		return
	}
	u := &store.User{
		Username: req.Username,
		Email:    req.Email,
		Role:     registrationDefaultRole(r.Context(), h.Store),
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	u.PasswordHash = hash
	if err := h.Store.CreateUser(r.Context(), u); err != nil {
		writeInternalError(w, err)
		return
	}
	_ = h.Store.AddAudit(r.Context(), &store.AuditEntry{
		Action: "user.register", Target: u.Username, IP: clientIP(r),
	})
	sess, err := h.newSession(w, r, u)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": toUserResponse(u), "csrf_token": sess.CSRFToken})
}
