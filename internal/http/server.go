// Package httpapi 组装 HTTP 路由、中间件链与静态资源。
package httpapi

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"relaywatch/internal/auth"
	"relaywatch/internal/check"
	"relaywatch/internal/compare"
	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// Server HTTP 服务。
type Server struct {
	Auth       *auth.Service
	OIDC       *auth.OIDCService
	Store      store.Store
	WebDist    fs.FS // 前端构建产物，可为 nil（纯 API 模式）
	Checker    *check.Checker
	Registry   *source.Registry
	Notifiers  *notifier.Registry
	Evaluator  *compare.Evaluator
	Broker     *EventBroker
	TestSender func(ctx context.Context, ch *store.Channel) error
	ResendOne  func(ctx context.Context, d *store.Delivery) error
	SecretBox  store.SecretBox // 全局邮件密码等 settings 敏感字段加密（可为 nil）
	// TrustProxy 信任反向代理的 X-Forwarded-For / X-Real-IP；默认关闭，
	// 避免客户端伪造转发头绕过按 IP 的登录限流与审计。
	TrustProxy bool
	// SecureCookies 强制会话 Cookie 带 Secure（base_url 为 https 时开启）。
	SecureCookies bool
	// LoginLimiter 登录失败与注册验证码发送的进程内限流（可为 nil，测试用）。
	LoginLimiter *auth.AttemptLimiter
	// MetricsCfg Prometheus 监控抓取端点配置。
	MetricsCfg MetricsConfig
	// Version 构建版本号（-ldflags 注入，如 v0.2.5；dev 构建为 "dev"）。
	Version string
	// MCP MCP Streamable HTTP 端点（internal/mcp 构造），可为 nil（未启用）。
	// 挂载在认证组内：Bearer API Token 认证，CSRF 天然豁免（无会话）。
	MCP http.Handler
	// BaseURL 实例对外地址（/SKILL.md 占位符替换用；未配置时按请求反推）。
	BaseURL string
}

// MetricsConfig 监控端点配置。
type MetricsConfig struct {
	Enabled  bool
	Username string
	Password string
}

// Handler 构建完整路由。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(normalizeSkillPath)
	r.Use(limitBody)
	if s.TrustProxy {
		// 安全边界由配置兜住：仅当运维显式信任反代（trust_proxy: true）才启用，
		// 使登录限流与审计拿到真实客户端 IP；直连部署保持关闭。
		//lint:ignore SA1019 chi RealIP 已弃用（其默认无条件采信转发头），此处由 trust_proxy 显式开关兜住
		r.Use(middleware.RealIP)
	}
	r.Use(securityHeaders)
	r.Use(auth.Middleware(s.Auth))
	r.Use(accessLogger) // 须在 auth 之后（记录用户）；panic 交由外层 Recoverer 打印堆栈

	// 无需认证
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Agent Skill 分发：AI 可自行抓取安装（公开，无凭据）
	r.Get("/SKILL.md", s.skillHandler)
	// Prometheus 监控指标（受 HTTP Basic Auth 保护，不采信 Web 会话）
	r.Get("/metrics", s.metricsHandler())

	// OIDC 302 跳转（不经过 /api/v1，SPA 无法消费 302）
	if s.OIDC != nil {
		oidcH := &OIDCHandler{Svc: s.OIDC, St: s.Store, SecureCookies: s.SecureCookies}
		oidcH.routesPublic(r)
	}

	// MCP 端点：AI 客户端用 Bearer API Token 连接（POST/GET/DELETE 同路径）。
	// 与 /api/v1 同级的认证链：RequireAuth + RequireCSRF（Bearer 无会话天然豁免）。
	if s.MCP != nil {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(auth.RequireCSRF)
			r.Handle("/mcp", s.MCP)
		})
	}

	// API v1
	mailH := &MailConfigHandler{St: s.Store, Box: s.SecretBox}
	authH := &AuthHandler{Svc: s.Auth, Store: s.Store, Mail: mailH, Limiter: s.LoginLimiter, SecureCookies: s.SecureCookies}
	r.Route("/api/v1", func(r chi.Router) {
		// 匿名可达：状态查询（登录页/引导页用）、登录、首次引导、注册、OIDC provider 列表
		r.Group(func(r chi.Router) {
			r.Get("/auth/status", authH.status)
			r.Post("/auth/login", authH.login)
			r.Post("/auth/setup", authH.setup)
			r.Post("/auth/register", authH.register)
			r.Post("/auth/register/send-code", authH.registerSendCode)
			if s.OIDC != nil {
				r.Get("/auth/providers", (&OIDCHandler{Svc: s.OIDC, St: s.Store}).list)
			}
		})

		// 需要登录 + CSRF
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(auth.RequireCSRF)

			r.Post("/auth/logout", authH.logout)
			r.Get("/auth/csrf", authH.csrf)
			r.Get("/version", s.versionHandler)

			profH := &ProfileHandler{St: s.Store, Svc: s.Auth}
			profH.routes(r)

			userH := &UserHandler{St: s.Store}
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				userH.routes(r)
			})

			// 「网站管理」：源站点默认值与 API 镜像（仅 admin）
			siteH := &SiteConfigHandler{St: s.Store}
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				siteH.routes(r)
			})

			// 全局邮件配置（仅 admin）
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireAdmin)
				mailH.routes(r)
			})

			monH := &MonitorHandler{St: s.Store, Registry: s.Registry, Evaluator: s.Evaluator}
			monH.routes(r)

			checkH := &CheckHandler{St: s.Store, Checker: s.Checker, Registry: s.Registry}
			checkH.routes(r)

			chH := &ChannelHandler{St: s.Store, Notifiers: s.Notifiers, TestSender: s.TestSender}
			chH.routes(r)

			dvH := &DeliveryHandler{St: s.Store, Notifiers: s.Notifiers, ResendOne: s.ResendOne}
			dvH.routes(r)

			stH := &SettingsHandler{St: s.Store}
			stH.routes(r)

			ieH := &ImportExportHandler{St: s.Store, Registry: s.Registry}
			ieH.routes(r)

			if s.OIDC != nil {
				// providers 管理仅 admin；/auth/providers 公开端点在上面匿名组已注册
				oidcH := &OIDCHandler{Svc: s.OIDC, St: s.Store, SecureCookies: s.SecureCookies}
				oidcH.routesAdmin(r)
				// 绑定确认（所有登录用户）
				oidcH.routesAuth(r)
			}

			tokH := &TokenHandler{Svc: s.Auth, St: s.Store}
			tokH.routes(r)

			r.Get("/stats", func(w http.ResponseWriter, req *http.Request) {
				s.statsHandler(w, req)
			})

			if s.Broker != nil {
				r.Get("/events", s.Broker.ServeHTTP)
			}
		})
	})

	// 静态资源与 SPA fallback
	if s.WebDist != nil {
		r.Get("/*", s.spaHandler())
	}

	return r
}

func (s *Server) spaHandler() http.HandlerFunc {
	fileServer := http.FileServer(http.FS(s.WebDist))
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.WebDist, path); err != nil {
			r.URL.Path = "/"
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	}
}

// maxRequestBodyBytes 限制 JSON 请求体大小，防止超大 body 占满内存。
const maxRequestBodyBytes = 8 << 20

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBodyBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "请求体过大")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// img-src 允许 https: 外链：OIDC Provider 图标等由 admin 配置的外部图片
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// statsHandler 仪表盘计数：按当前用户可见范围统计（admin 全量，普通用户仅自己）。
func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	counts, err := s.Store.CountMonitorStates(r.Context(), scopeOwnerID(r))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":      counts.Total,
		"has_update": counts.HasUpdate,
		"error":      counts.Error,
		"ok":         counts.UpToDate,
		"missing":    counts.Missing,
		"unknown":    counts.Unknown,
		"lagging":    counts.Lagging,
	})
}

func (s *Server) metricsHandler() http.HandlerFunc {
	h := &MetricsHandler{St: s.Store}
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.MetricsCfg.Enabled {
			http.NotFound(w, r)
			return
		}
		if s.MetricsCfg.Password == "" {
			http.Error(w, `{"message":"metrics password not configured"}`, http.StatusForbidden)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(s.MetricsCfg.Username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(s.MetricsCfg.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="RelayWatch Metrics"`)
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}
}
