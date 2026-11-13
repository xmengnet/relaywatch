// 供 internal/mcp 复用的导出面：HTTP API 与 MCP 工具共用同一套
// 校验 / 转换 / 脱敏逻辑，避免协议层各维护一份。
package httpapi

import (
	"context"

	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
	"relaywatch/internal/store"
)

// 视图与负载类型的别名（字段与 JSON 行为完全一致）。
type (
	MonitorPayload       = monitorPayload
	MonitorResponse      = monitorResponse
	ChannelPayload       = channelPayload
	ChannelResponse      = channelResponse
	CheckHistoryResponse = checkHistoryResponse
	VersionChangeResp    = versionChangeResponse
)

// 校验类。

func ValidateMonitorPayload(p *MonitorPayload) error { return p.validate() }

func ValidateChannelPayload(p *ChannelPayload) error { return p.validate() }

func ValidateSourceConfig(reg *source.Registry, p *MonitorPayload) error {
	return validateSourceConfig(reg, p)
}

func ValidateChannelIDs(ctx context.Context, st store.Store, u *store.User, ids []int64) error {
	return validateChannelIDs(ctx, st, u, ids)
}

func ValidateReference(ctx context.Context, st store.Store, selfID, selfOwner int64, refID *int64) error {
	return validateReference(ctx, st, selfID, selfOwner, refID)
}

func ValidateChannelType(reg *notifier.Registry, p *ChannelPayload) error {
	return validateChannelType(reg, p)
}

// 转换类。

func PayloadToMonitor(p *MonitorPayload) *store.Monitor { return payloadToMonitor(p) }

func ApplyMonitorPayload(m *store.Monitor, p *MonitorPayload) { applyPayload(m, p) }

func ToMonitorResponse(m *store.Monitor) *MonitorResponse { return toMonitorResponse(m) }

func ToChannelResponse(c *store.Channel) *ChannelResponse { return toChannelResponse(c) }

func ToCheckHistoryResponse(h *store.CheckHistory) *CheckHistoryResponse {
	return toCheckHistoryResponse(h)
}

func ToVersionChangeResponse(c *store.VersionChange) *VersionChangeResp {
	return toVersionChangeResponse(c)
}

func RefMonitorName(st store.Store, ctx context.Context, refID int64) string {
	return refMonitorName(st, ctx, refID)
}

// MaskedConfig 按渠道类型元数据掩码 secret 字段。
func MaskedConfig(reg *notifier.Registry, cfgType, config string) string {
	return maskedConfig(reg, cfgType, config)
}

// 访问控制类。

func CanAccessMonitor(u *store.User, m *store.Monitor) bool { return canAccessMonitor(u, m) }

// ScopeUserOwnerID 列表查询的 owner 过滤：admin 0 = 不过滤，普通用户 = 自己。
func ScopeUserOwnerID(u *store.User) int64 {
	if isAdmin(u) {
		return 0
	}
	return u.ID
}
