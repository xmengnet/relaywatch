package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"relaywatch/internal/auth"
)

// accessLogger 访问日志：请求完成后按响应状态分级输出到 slog
// （5xx→Error、4xx→Warn、其余→Info），级别与格式跟随 logging 配置。
// 必须挂在 auth.Middleware 之后才能记录用户；panic 的请求不落访问日志，
// 由外层 Recoverer 打印堆栈。
func accessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 探活、指标抓取与前端静态资源高频且固定成功，不记
		if isNoise(r) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			// SSE 是长连接，服务端要到断开才返回，只在建立时记一条
			slog.Info("sse open", baseFields(r)...)
			next.ServeHTTP(w, r)
			return
		}

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		logRequest(r, ww.Status(), ww.BytesWritten(), time.Since(start))
	})
}

// isNoise 无需记录的路径。
func isNoise(r *http.Request) bool {
	p := r.URL.Path
	return p == "/healthz" || p == "/metrics" || strings.HasPrefix(p, "/assets/")
}

// logRequest 输出一条访问日志，按状态选择级别。
func logRequest(r *http.Request, status, n int, elapsed time.Duration) {
	fields := append(baseFields(r),
		"status", status,
		"duration_ms", float64(elapsed.Microseconds())/1e3,
		"bytes", n,
	)
	switch {
	case status >= 500:
		slog.Error("http request", fields...)
	case status >= 400:
		slog.Warn("http request", fields...)
	default:
		slog.Info("http request", fields...)
	}
}

// baseFields 所有访问日志共有的字段；已认证时附带用户名。
func baseFields(r *http.Request) []any {
	fields := []any{"method", r.Method, "path", r.URL.Path, "ip", logClientIP(r)}
	if u := auth.UserFromContext(r.Context()); u != nil {
		fields = append(fields, "user", u.Username)
	}
	return fields
}

// logClientIP 取访问日志用的客户端 IP：本中间件挂在 RealIP 之后，
// RemoteAddr 已是真实客户端地址，只需去掉端口（代换后可能不带端口）。
func logClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
