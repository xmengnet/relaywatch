package httpapi

import "net/http"

// versionHandler GET /api/v1/version：返回构建时 -ldflags 注入的版本号。
// 登录用户可读（设置页「关于」展示），不含敏感信息。
func (s *Server) versionHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.Version})
}
