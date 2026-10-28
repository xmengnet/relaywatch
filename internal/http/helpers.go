package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"relaywatch/internal/auth"
	"relaywatch/internal/store"
)

// authUser 从请求上下文取当前用户（RequireAuth 已保证非 nil）。
func authUser(r *http.Request) *store.User {
	return auth.UserFromContext(r.Context())
}

// authUserFromCtx 同 authUser，供非 *http.Request 签名的 helper 使用。
func authUserFromCtx(ctx context.Context) *store.User {
	return auth.UserFromContext(ctx)
}

// isAdmin 当前用户是否管理员。
func isAdmin(u *store.User) bool { return u != nil && u.Role == "admin" }

// forbidPermission 细粒度权限不足的统一 403 响应（与角色不足区分开）。
func forbidPermission(w http.ResponseWriter, perm string) {
	writeError(w, http.StatusForbidden, "没有权限："+perm)
}

// requireWrite 拒绝只读用户（viewer）对既有资源的增删改，返回 false 表示已写 403。
// 归属校验（404）应先于本助手执行，避免暴露资源是否存在。
func requireWrite(w http.ResponseWriter, r *http.Request) bool {
	if authUser(r).CanWrite() {
		return true
	}
	writeError(w, http.StatusForbidden, "只读用户无权执行该操作")
	return false
}

// scopeOwnerID 列表查询的 owner 过滤参数：admin 0 = 不过滤，普通用户 = 自己。
func scopeOwnerID(r *http.Request) int64 {
	u := authUser(r)
	if isAdmin(u) {
		return 0
	}
	return u.ID
}

// canAccessMonitor admin 全通；普通用户仅自己的资源。统一 404 语义由调用方处理。
func canAccessMonitor(u *store.User, m *store.Monitor) bool {
	return isAdmin(u) || (m != nil && m.OwnerID == u.ID)
}

// canAccessChannel admin 全通；普通用户可访问自己的与全局共享（owner 为 nil）渠道。
func canAccessChannel(u *store.User, c *store.Channel) bool {
	if isAdmin(u) {
		return true
	}
	return c != nil && c.OwnerID != nil && *c.OwnerID == u.ID
}

// canModifyChannel 在可见基础上进一步限制写权限：普通用户不能改全局共享渠道。
func canModifyChannel(u *store.User, c *store.Channel) bool {
	return canAccessChannel(u, c)
}

// timeNow 便于测试替换。
var timeNow = time.Now

// secureCookie 决定会话 Cookie 是否带 Secure：直连 TLS，或 base_url 配置为 https。
func secureCookie(r *http.Request, force bool) bool {
	return force || r.TLS != nil
}

func pageParams(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage <= 0 || perPage > 100 {
		perPage = 20
	}
	if page <= 0 {
		page = 1
	}
	return
}

// chiRouterIface 保持 handlers 签名简短。
type chiRouterIface = chi.Router
