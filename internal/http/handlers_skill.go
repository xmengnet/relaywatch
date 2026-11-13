package httpapi

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed skill.md
var skillMD string

// normalizeSkillPath 把 /SKILL.md 的任意大小写变体（/skill.md 等）重写为
// 规范路径。Agent 对 URL 大小写不敏感是常见行为（如 Claude 抓取 /Skill.Md）。
func normalizeSkillPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.URL.Path, "/skill.md") && r.URL.Path != "/SKILL.md" {
			r.URL.Path = "/SKILL.md"
		}
		next.ServeHTTP(w, r)
	})
}

// skillHandler GET /SKILL.md：对外分发的 Agent Skill 文档（agentskills.io
// 规范的 SKILL.md，AI 可自行抓取安装）。公开路由，纯静态使用说明，不含
// 任何用户数据与凭据；认证凭据由 AI 在使用时向用户索取（安装时公开、
// 使用时注入）。文中的 <BASE_URL> 占位符按实例对外地址替换。
func (s *Server) skillHandler(w http.ResponseWriter, r *http.Request) {
	base := s.BaseURL
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(skillMD, "<BASE_URL>", strings.TrimRight(base, "/"))))
}
