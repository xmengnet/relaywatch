package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"relaywatch/internal/notifier"
	"relaywatch/internal/source"
)

// addDiscoveryTools 注册发现类工具：让 AI 自查源/渠道类型的元数据
// （含字段名、说明、示例占位），从而正确拼出 source_config / 渠道 config。
// 元数据与 Web 前端动态表单共用同一份（Registry.Metas）。

func addDiscoveryTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_source_types",
		Description: "列出所有支持的监控源类型及其配置字段（fields 中 key/label/help 说明每个字段的含义）。创建监控前先调用本工具确定 source_type 与 source_config 的写法。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []source.Meta, error) {
		return nil, d.Sources.Metas(), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_notifier_types",
		Description: "列出所有支持的告警渠道类型及其配置字段（fields 中 key/label/help 说明每个字段的含义，type=secret 的字段为敏感项）。创建渠道前先调用本工具确定渠道 type 与 config 的写法。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []notifier.Meta, error) {
		return nil, d.Notifiers.Metas(), nil
	})
}
