package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// AuthHandler 登录、登出、首次引导、状态查询。
type AuthHandler struct {
	Svc           *auth.Service
	Store         store.Store
	Mail          *MailConfigHandler   // 注册验证码发信；nil 时该功能不可用
	Limiter       *auth.AttemptLimiter // 登录失败/发码限流；nil 时不限制
	SecureCookies bool                 // base_url 为 https 时强制 Secure Cookie
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

// registrationNeedEmail 注册是否需邮箱验证码。
func registrationNeedEmail(ctx context.Context, st store.Store) bool {
	v, err := st.GetSetting(ctx, settingRegistrationNeedMail)
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
		"setup_required":             !has,
		"user":                       nil,
		"csrf_token":                 "",
		"registration_open":          h.Store != nil && registrationEnabled(r.Context(), h.Store),
		"registration_require_email": h.Store != nil && registrationNeedEmail(r.Context(), h.Store),
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
	key := loginKey(r, req.Username)
	if !h.Limiter.Allow(key) {
		writeError(w, http.StatusTooManyRequests, "登录尝试过于频繁，请稍后再试")
		return
	}
	u, sess, err := h.Svc.Login(r.Context(), req.Username, req.Password, r.UserAgent(), clientIP(r))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		h.Limiter.Hit(key)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	h.Limiter.Reset(key)
	secure := secureCookie(r, h.SecureCookies)
	auth.SetSessionCookie(w, sess.ID, int(sess.ExpiresAt.Sub(sess.CreatedAt).Seconds()), secure)
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(u), "csrf_token": sess.CSRFToken})
}

func (h *AuthHandler) newSession(w http.ResponseWriter, r *http.Request, u *store.User) (*store.Session, error) {
	sess, err := h.Svc.CreateSessionFor(r.Context(), u.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		return nil, err
	}
	secure := secureCookie(r, h.SecureCookies)
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

// clientIP 取客户端地址。默认只信 socket 地址；开启 server.trust_proxy 后，
// middleware.RealIP 已把 RemoteAddr 替换为转发头里的地址。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginKey 按 IP + 用户名限流：既防单账户爆破，也避免不同用户互相影响。
func loginKey(r *http.Request, username string) string {
	return "login:" + clientIP(r) + "|" + strings.ToLower(strings.TrimSpace(username))
}

// registerSendCode 发送注册邮箱验证码：注册开启 + require_email + 全局邮件已配置才可用。
func (h *AuthHandler) registerSendCode(w http.ResponseWriter, r *http.Request) {
	if !registrationEnabled(r.Context(), h.Store) {
		writeError(w, http.StatusNotFound, "未开放注册")
		return
	}
	if !registrationNeedEmail(r.Context(), h.Store) {
		writeError(w, http.StatusBadRequest, "注册未开启邮箱验证")
		return
	}
	// 匿名端点按 IP 限流，防止被用来批量发信
	limitKey := "sendcode:" + clientIP(r)
	if !h.Limiter.Allow(limitKey) {
		writeError(w, http.StatusTooManyRequests, "发送过于频繁，请稍后再试")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	h.Limiter.Hit(limitKey)
	// 防滥用：同一邮箱 60 秒内只发一次
	if rec, ok, err := h.Store.GetRegistrationCode(r.Context(), email); err == nil && ok {
		if time.Since(rec.SentAt) < time.Minute {
			writeError(w, http.StatusTooManyRequests, "发送过于频繁，请稍后再试")
			return
		}
	}
	code, err := GenerateRegCode(r.Context(), h.Store, email)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	body := fmt.Sprintf("你的 RelayWatch 注册验证码是：%s\n\n10 分钟内有效。如非本人操作请忽略本邮件。", code)
	if h.Mail == nil {
		writeError(w, http.StatusServiceUnavailable, "全局邮件未配置")
		return
	}
	if err := h.Mail.SendSystemMail(r.Context(), email, "RelayWatch 注册验证码", body); err != nil {
		writeError(w, http.StatusServiceUnavailable, "邮件发送失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// register 自助注册：仅 registration.enabled 时开放；关闭时 404 防探测。
func (h *AuthHandler) register(w http.ResponseWriter, r *http.Request) {
	if !registrationEnabled(r.Context(), h.Store) {
		writeError(w, http.StatusNotFound, "未开放注册")
		return
	}
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Email     string `json:"email"`
		EmailCode string `json:"email_code"` // require_email 开启时必填
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
	if registrationNeedEmail(r.Context(), h.Store) {
		if strings.TrimSpace(req.Email) == "" {
			writeError(w, http.StatusBadRequest, "注册需要邮箱")
			return
		}
		if err := VerifyRegCode(r.Context(), h.Store, req.Email, req.EmailCode); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
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
