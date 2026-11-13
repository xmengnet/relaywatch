// Package mcp RelayWatch 的 MCP 服务：把监控/渠道/投递等能力以 MCP 工具
// 形式暴露给 AI 客户端。传输为 Streamable HTTP，认证复用现有 Bearer API
// Token（auth.Middleware 已在 /mcp 外层完成身份注入，工具层只读上下文）。
//
// 工具集是白名单，不是 HTTP API 的全量镜像：刻意不放删除类操作、
// 用户管理、站点/邮件配置等敏感或高危接口。
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"relaywatch/internal/auth"
	"relaywatch/internal/check"
	"relaywatch/internal/compare"
	httpapi "relaywatch/internal/http"
	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// Deps MCP 工具依赖的业务组件（与 httpapi.Server 共享同一实例）。
type Deps struct {
	St        store.Store
	Sources   *source.Registry
	Notifiers *notifier.Registry
	Checker   *check.Checker
	Evaluator *compare.Evaluator
	Version   string // 构建版本号（-ldflags 注入），进 serverInfo
}

// New 构建注册了全部工具的 MCP Server。
func New(d Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "relaywatch", Version: d.Version}, nil)
	addDiscoveryTools(s, d)
	addMonitorTools(s, d)
	addChannelTools(s, d)
	return s
}

// NewHTTPHandler 挂载 Streamable HTTP 传输。所有会话共享同一 Server
// 实例（官方 SDK 明确允许），用户身份从请求上下文逐次读取。
func NewHTTPHandler(s *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
}

// ---------- 工具层公共助手 ----------

// ctxUser 从请求上下文取当前用户（/mcp 在 RequireAuth 之内，不会为 nil）。
func ctxUser(ctx context.Context) *store.User {
	return auth.UserFromContext(ctx)
}

// errNotFound 统一 404 语义：归属不符与不存在同文案，防资源枚举。
var errNotFound = errors.New("不存在或无权访问")

// accessMonitor 取监控并校验归属，失败返回 errNotFound（AI 端显示为工具错误）。
func accessMonitor(ctx context.Context, d Deps, id int64) (*store.Monitor, error) {
	m, err := d.St.GetMonitor(ctx, id)
	if err != nil {
		return nil, errNotFound
	}
	if !httpapi.CanAccessMonitor(ctxUser(ctx), m) {
		return nil, errNotFound
	}
	return m, nil
}

// audit 写操作审计，action 加 mcp. 前缀以便与 Web 操作区分。
func audit(ctx context.Context, d Deps, u *store.User, action, target string) {
	if u == nil {
		return
	}
	uid := u.ID
	_ = d.St.AddAudit(ctx, &store.AuditEntry{UserID: &uid, Action: action, Target: target})
}

// toolErr 把业务错误包装成给 AI 看的可读信息。
func toolErr(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
