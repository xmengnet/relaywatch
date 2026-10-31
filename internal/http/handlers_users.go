package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// ProfileHandler 当前用户资料自助维护（所有角色）。
type ProfileHandler struct {
	St  store.Store
	Svc *auth.Service
}

func (h *ProfileHandler) routes(r chiRouterIface) {
	r.Get("/profile", h.get)
	r.Patch("/profile", h.update)
	r.Post("/profile/password", h.changePassword)
	r.Get("/profile/sessions", h.sessions)
	r.Delete("/profile/sessions/{id}", h.revokeSession)
	r.Delete("/profile/oidc", h.unbindOIDC)
}

func (h *ProfileHandler) get(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	got, err := h.St.UserByID(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	resp := map[string]any{
		"user":          toUserResponse(got),
		"oidc":          got.OIDCProvider != "", // OIDC-only 账户不可设密码
		"oidc_provider": got.OIDCProvider,       // 绑定的 IdP 名，未绑定为空
		"has_password":  len(got.PasswordHash) > 0,
		"has_ldap":      false,
	}
	writeJSON(w, http.StatusOK, resp)
}

// unbindOIDC DELETE /profile/oidc：解除当前用户的 OIDC 绑定。
// 未设本地密码的纯 OIDC 账户拒绝解绑，避免解绑后失去唯一登录方式。
func (h *ProfileHandler) unbindOIDC(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	got, err := h.St.UserByID(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if got.OIDCProvider == "" {
		writeError(w, http.StatusBadRequest, "尚未绑定 OIDC 账号")
		return
	}
	if len(got.PasswordHash) == 0 {
		writeError(w, http.StatusBadRequest, "该账户未设置本地密码，请先在「安全」中设置密码再解绑")
		return
	}
	if err := h.St.UnbindOIDCUser(r.Context(), got.ID); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *ProfileHandler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       *string `json:"email"`
		DisplayName *string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u := authUser(r)
	got, err := h.St.UserByID(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if req.Email != nil {
		got.Email = *req.Email
	}
	if req.DisplayName != nil {
		got.DisplayName = *req.DisplayName
	}
	// 只更新资料字段，密码/角色不经过此接口
	got.PasswordHash = nil
	if err := h.St.UpdateUserProfile(r.Context(), got); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": toUserResponse(got)})
}

func (h *ProfileHandler) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "新密码至少 8 个字符")
		return
	}
	u := authUser(r)
	got, err := h.St.UserByID(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if got.OIDCProvider != "" && len(got.PasswordHash) == 0 {
		writeError(w, http.StatusBadRequest, "OIDC 账户密码由身份提供方管理")
		return
	}
	if !auth.ComparePassword(got.PasswordHash, req.OldPassword) {
		writeError(w, http.StatusUnauthorized, "旧密码不正确")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if err := h.St.UpdateUserPassword(r.Context(), u.ID, hash); err != nil {
		writeInternalError(w, err)
		return
	}
	_ = h.St.AddAudit(r.Context(), &store.AuditEntry{UserID: &u.ID, Action: "profile.password_change", IP: clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type sessionItem struct {
	ID        string  `json:"id"`
	UserAgent string  `json:"user_agent,omitempty"`
	IP        string  `json:"ip,omitempty"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	Current   bool    `json:"current"`
	RevokedAt *string `json:"revoked_at,omitempty"`
}

// sessions 列出当前用户全部有效会话（profile 会话管理）。
func (h *ProfileHandler) sessions(w http.ResponseWriter, r *http.Request) {
	u := authUser(r)
	items, err := h.St.ListSessionsByUser(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	current := authSessionIDOf(r)
	out := make([]sessionItem, 0, len(items))
	for _, sess := range items {
		out = append(out, sessionItem{
			ID:        sess.ID,
			UserAgent: sess.UserAgent,
			IP:        sess.IP,
			CreatedAt: sess.CreatedAt.Format(timeRFC3339),
			ExpiresAt: sess.ExpiresAt.Format(timeRFC3339),
			Current:   sess.ID == current,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// authSessionIDOf 当前请求的会话 ID（API Token 认证时为空）。
func authSessionIDOf(r *http.Request) string {
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		return sess.ID
	}
	return ""
}

// revokeSession 吊销自己的一个会话；吊销当前会话即登出。
func (h *ProfileHandler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := authUser(r)
	// 只能吊销自己的会话（归属校验）
	mine := false
	sessions, err := h.St.ListSessionsByUser(r.Context(), u.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	for _, sess := range sessions {
		if sess.ID == id {
			mine = true
			break
		}
	}
	if !mine {
		writeError(w, http.StatusNotFound, "会话不存在")
		return
	}
	if err := h.St.RevokeSession(r.Context(), id); err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 用户管理（admin） ----------

// UserHandler 用户管理，全部端点挂 RequireAdmin。
type UserHandler struct {
	St store.Store
}

func (h *UserHandler) routes(r chiRouterIface) {
	r.Get("/users", h.list)
	r.Post("/users", h.create)
	r.Patch("/users/{id}", h.update)
	r.Post("/users/{id}/disable", h.disable)
	r.Delete("/users/{id}", h.remove)
}

func (h *UserHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	items, total, err := h.St.ListUsers(r.Context(), store.UserFilter{
		Role:    q.Get("role"),
		Search:  q.Get("q"),
		Page:    page,
		PerPage: perPage,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	out := make([]*adminUserResponse, 0, len(items))
	for _, u := range items {
		n, _ := h.St.CountMonitorsByOwner(r.Context(), u.ID)
		out = append(out, toAdminUserResponse(u, n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

type userPayload struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Email       *string  `json:"email"`
	DisplayName *string  `json:"display_name"`
	Role        *string  `json:"role"`
	Permissions []string `json:"permissions"` // nil = 未配置按角色默认；数组 = 显式配置
}

func validRole(role string) bool {
	return role == "admin" || role == "user" || role == "viewer"
}

// validPermissions 校验权限项均合法（允许空数组=全部禁用，允许 nil=按角色默认）。
func validPermissions(perms []string) bool {
	for _, p := range perms {
		switch p {
		case store.PermCreateMonitor, store.PermCreateChannel, store.PermImport, store.PermExport:
		default:
			return false
		}
	}
	return true
}

func (h *UserHandler) create(w http.ResponseWriter, r *http.Request) {
	var p userPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if p.Username == "" || len(p.Password) < 8 {
		writeError(w, http.StatusBadRequest, "用户名不能为空，密码至少 8 个字符")
		return
	}
	role := "user"
	if p.Role != nil {
		if !validRole(*p.Role) {
			writeError(w, http.StatusBadRequest, "无效角色: "+*p.Role)
			return
		}
		role = *p.Role
	}
	if !validPermissions(p.Permissions) {
		writeError(w, http.StatusBadRequest, "无效权限项")
		return
	}
	if _, err := h.St.UserByUsername(r.Context(), p.Username); err == nil {
		writeError(w, http.StatusConflict, "用户名已被占用")
		return
	}
	hash, err := auth.HashPassword(p.Password)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	u := &store.User{
		Username:     p.Username,
		Email:        deref(p.Email),
		DisplayName:  deref(p.DisplayName),
		PasswordHash: hash,
		Role:         role,
		Permissions:  p.Permissions,
	}
	if err := h.St.CreateUser(r.Context(), u); err != nil {
		writeInternalError(w, err)
		return
	}
	h.audit(r, "user.create", u.Username)
	writeJSON(w, http.StatusCreated, toAdminUserResponse(u, 0))
}

func (h *UserHandler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	target, err := h.St.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var p userPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	me := authUser(r)
	// 防自锁：不可修改自己的角色
	if p.Role != nil {
		if !validRole(*p.Role) {
			writeError(w, http.StatusBadRequest, "无效角色: "+*p.Role)
			return
		}
		if target.ID == me.ID && *p.Role != "admin" {
			writeError(w, http.StatusBadRequest, "不能修改自己的角色")
			return
		}
		// 角色降级/禁用前校验：系统至少保留一个可用 admin
		if target.Role == "admin" && *p.Role != "admin" {
			if n, _ := h.St.HasEnabledAdmin(r.Context()); n <= 1 {
				writeError(w, http.StatusConflict, "系统至少需要一个管理员")
				return
			}
		}
		target.Role = *p.Role
	}
	if p.Email != nil {
		target.Email = *p.Email
	}
	if p.DisplayName != nil {
		target.DisplayName = *p.DisplayName
	}
	if !validPermissions(p.Permissions) {
		writeError(w, http.StatusBadRequest, "无效权限项")
		return
	}
	if p.Permissions != nil {
		target.Permissions = p.Permissions
	}
	// 角色变更时重置权限为该角色默认（admin 恒全量），除非本次显式指定了 permissions
	if p.Role != nil && p.Permissions == nil {
		if *p.Role == "admin" {
			target.Permissions = nil
		} else {
			target.Permissions = nil // nil = 按新角色默认
		}
	}
	if p.Password != "" {
		if len(p.Password) < 8 {
			writeError(w, http.StatusBadRequest, "密码至少 8 个字符")
			return
		}
		hash, err := auth.HashPassword(p.Password)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		target.PasswordHash = hash
	}
	if err := h.St.UpdateUserProfile(r.Context(), target); err != nil {
		writeInternalError(w, err)
		return
	}
	// 重置密码后吊销该用户全部会话（防盗用）
	if p.Password != "" {
		if _, err := h.St.RevokeUserSessions(r.Context(), target.ID); err != nil {
			writeInternalError(w, err)
			return
		}
		h.audit(r, "user.password_reset", target.Username)
	}
	h.audit(r, "user.update", target.Username)
	n, _ := h.St.CountMonitorsByOwner(r.Context(), target.ID)
	writeJSON(w, http.StatusOK, toAdminUserResponse(target, n))
}

func (h *UserHandler) disable(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	target, err := h.St.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var req struct {
		Disabled *bool `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Disabled == nil {
		writeError(w, http.StatusBadRequest, "需要 disabled 字段")
		return
	}
	disabled := *req.Disabled
	// 启用操作无需自锁校验；禁用 admin 前校验剩余数量
	if disabled && target.Role == "admin" {
		if n, _ := h.St.HasEnabledAdmin(r.Context()); n <= 1 {
			writeError(w, http.StatusConflict, "系统至少需要一个管理员")
			return
		}
	}
	if err := h.St.SetUserDisabled(r.Context(), target.ID, disabled); err != nil {
		writeInternalError(w, err)
		return
	}
	// 禁用即吊销全部会话，立即生效
	if disabled {
		if _, err := h.St.RevokeUserSessions(r.Context(), target.ID); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	action := "user.enable"
	if disabled {
		action = "user.disable"
	}
	h.audit(r, action, target.Username)
	n, _ := h.St.CountMonitorsByOwner(r.Context(), target.ID)
	updated, _ := h.St.UserByID(r.Context(), target.ID)
	writeJSON(w, http.StatusOK, toAdminUserResponse(updated, n))
}

func (h *UserHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	target, err := h.St.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	me := authUser(r)
	if target.ID == me.ID {
		writeError(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	if target.Role == "admin" {
		if n, _ := h.St.HasEnabledAdmin(r.Context()); n <= 1 {
			writeError(w, http.StatusConflict, "系统至少需要一个管理员")
			return
		}
	}
	// 资源转移给操作者，会话/Token 级联删除
	if err := h.St.DeleteUserTransferResources(r.Context(), target.ID, me.ID); err != nil {
		writeInternalError(w, err)
		return
	}
	h.audit(r, "user.delete", target.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transferred_to": me.Username})
}

func (h *UserHandler) audit(r *http.Request, action, target string) {
	u := authUser(r)
	_ = h.St.AddAudit(r.Context(), &store.AuditEntry{
		UserID: &u.ID, Action: action, Target: target, IP: clientIP(r),
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
