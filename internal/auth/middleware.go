package auth

import (
	"context"
	"net/http"
	"strings"

	"relaywatch/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
)

// WithUser 把认证结果放入请求上下文。
func WithUser(ctx context.Context, u *store.User, sess *store.Session) context.Context {
	ctx = context.WithValue(ctx, ctxUser, u)
	if sess != nil {
		ctx = context.WithValue(ctx, ctxSession, sess)
	}
	return ctx
}

// UserFromContext 取当前用户（可能为 nil）。
func UserFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxUser).(*store.User)
	return u
}

// SessionFromContext 取当前会话（API Token 认证时为 nil）。
func SessionFromContext(ctx context.Context) *store.Session {
	s, _ := ctx.Value(ctxSession).(*store.Session)
	return s
}

const sessionCookie = "relaywatch_session"

// SetSessionCookie 写会话 Cookie。
func SetSessionCookie(w http.ResponseWriter, id string, maxAge int, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie 清除会话 Cookie。
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionCookieName 供中间件读取。
func SessionCookieName() string { return sessionCookie }

// Middleware 认证中间件：支持会话 Cookie 与 Bearer Token。
// 未认证时请求继续但 ctx 中无用户，由各 handler 或 requireAuth 决定拒绝。
func Middleware(svc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if raw := r.Header.Get("Authorization"); raw != "" {
				if u, err := svc.AuthByAPIToken(ctx, raw); err == nil {
					next.ServeHTTP(w, r.WithContext(WithUser(ctx, u, nil)))
					return
				}
			}
			c, err := r.Cookie(sessionCookie)
			if err == nil && c.Value != "" {
				if u, sess, err := svc.UserBySession(ctx, c.Value); err == nil {
					next.ServeHTTP(w, r.WithContext(WithUser(ctx, u, sess)))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth 要求已登录，否则 401。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			http.Error(w, `{"message":"未登录"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin 要求 admin 角色。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			http.Error(w, `{"message":"未登录"}`, http.StatusUnauthorized)
			return
		}
		if u.Role != "admin" {
			http.Error(w, `{"message":"权限不足"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireCSRF 校验写请求的 CSRF token。API Token 调用（无会话）豁免。
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		sess := SessionFromContext(r.Context())
		if sess == nil {
			// Bearer Token 调用无 CSRF 风险
			next.ServeHTTP(w, r)
			return
		}
		tok := r.Header.Get("X-CSRF-Token")
		if tok == "" {
			tok = r.PostFormValue("_csrf")
		}
		if tok == "" || !strings.EqualFold(tok, sess.CSRFToken) {
			http.Error(w, `{"message":"CSRF 校验失败"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
