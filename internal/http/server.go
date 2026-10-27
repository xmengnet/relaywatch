// Package httpapi 组装 HTTP 路由、中间件链与静态资源。
package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"relaywatch/internal/auth"
	"relaywatch/internal/check"
	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// Server HTTP 服务。
type Server struct {
	Auth      *auth.Service
	OIDC      *auth.OIDCService
	Store     store.Store
	WebDist   fs.FS // 前端构建产物，可为 nil（纯 API 模式）
	Checker   *check.Checker
	Registry  *source.Registry
	Notifiers *notifier.Registry
	TestSender    func(ctx context.Context, ch *store.Channel) error
	ResendOne     func(ctx context.Context, d *store.Delivery) error
}

// Handler 构建完整路由。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(securityHeaders)
	r.Use(auth.Middleware(s.Auth))

	// 无需认证
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Handle("/metrics", &MetricsHandler{St: s.Store})

	// OIDC 302 跳转（不经过 /api/v1，SPA 无法消费 302）
	if s.OIDC != nil {
		oidcH := &OIDCHandler{Svc: s.OIDC, St: s.Store}
		oidcH.routesPublic(r)
	}

	// API v1
	authH := &AuthHandler{Svc: s.Auth}
	r.Route("/api/v1", func(r chi.Router) {
		// 匿名可达：状态查询（登录页/引导页用）、登录、首次引导、OIDC provider 列表
		r.Group(func(r chi.Router) {
			r.Get("/auth/status", authH.status)
			r.Post("/auth/login", authH.login)
			r.Post("/auth/setup", authH.setup)
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

			monH := &MonitorHandler{St: s.Store}
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
				oidcH := &OIDCHandler{Svc: s.OIDC, St: s.Store}
				oidcH.routesAdmin(r)
			}

			tokH := &TokenHandler{Svc: s.Auth, St: s.Store}
			tokH.routes(r)

			r.Get("/stats", func(w http.ResponseWriter, req *http.Request) {
				s.statsHandler(w, req)
			})
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

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// statsHandler 仪表盘计数：按状态分组统计。
func (s *Server) statsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats := map[string]any{"total": 0, "has_update": 0, "error": 0, "ok": 0, "missing": 0, "unknown": 0}
	// 简单实现：拉全量启用+停用的监控（千级规模可接受）
	items, total, err := s.Store.ListMonitors(ctx, store.MonitorFilter{PerPage: 1, Page: 1})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	stats["total"] = total
	_ = items
	for _, state := range []store.MonitorState{store.StateHasUpdate, store.StateError, store.StateMissing, store.StateUnknown, store.StateUpToDate} {
		_, n, err := s.Store.ListMonitors(ctx, store.MonitorFilter{State: state, PerPage: 1})
		if err != nil {
			writeInternalError(w, err)
			return
		}
		switch state {
		case store.StateHasUpdate:
			stats["has_update"] = n
		case store.StateError:
			stats["error"] = n
		case store.StateMissing:
			stats["missing"] = n
		case store.StateUnknown:
			stats["unknown"] = n
		case store.StateUpToDate:
			stats["ok"] = n
		}
	}
	writeJSON(w, http.StatusOK, stats)
}
