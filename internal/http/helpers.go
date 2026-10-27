package httpapi

import (
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

// timeNow 便于测试替换。
var timeNow = time.Now

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
