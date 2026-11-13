package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	httpapi "relaywatch/internal/http"
	"relaywatch/internal/store"
)

// addChannelTools 注册渠道相关工具。渠道 config 的写法由
// list_notifier_types 的元数据描述；列表返回的 config 一律脱敏。

func addChannelTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_channels",
		Description: "列出当前用户可见的告警渠道（自己的 + 全局共享；admin 为全部）。敏感字段已脱敏。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct {
		Items []*httpapi.ChannelResponse `json:"items"`
	}, error) {
		u := ctxUser(ctx)
		var (
			channels []*store.Channel
			err      error
		)
		if u.Role == "admin" {
			channels, err = d.St.ListChannelsAll(ctx)
		} else {
			channels, err = d.St.ListChannelsVisible(ctx, u.ID)
		}
		if err != nil {
			return nil, struct {
				Items []*httpapi.ChannelResponse `json:"items"`
			}{}, err
		}
		out := make([]*httpapi.ChannelResponse, 0, len(channels))
		for _, c := range channels {
			resp := httpapi.ToChannelResponse(c)
			// MCP 面向 AI，config 一律脱敏（无论是否本人可改）
			resp.Config = httpapi.MaskedConfig(d.Notifiers, c.Type, c.Config)
			out = append(out, resp)
		}
		return nil, struct {
			Items []*httpapi.ChannelResponse `json:"items"`
		}{Items: out}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_channel",
		Description: "创建告警渠道。先用 list_notifier_types 确认 type 与 config 写法（如 webhook/url、token 等）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Name     string         `json:"name" jsonschema:"渠道名称，必填"`
		Type     string         `json:"type" jsonschema:"渠道类型，必填；取值见 list_notifier_types"`
		Config   map[string]any `json:"config,omitempty" jsonschema:"渠道配置对象，字段取值见 list_notifier_types 对应类型的 fields"`
		Template string         `json:"template,omitempty" jsonschema:"通知模板（留空用默认模板）"`
		Enabled  *bool          `json:"enabled,omitempty" jsonschema:"是否启用，默认 true"`
	}) (*mcp.CallToolResult, struct {
		Item *httpapi.ChannelResponse `json:"item"`
	}, error) {
		u := ctxUser(ctx)
		if !u.Can(store.PermCreateChannel) {
			return nil, struct {
				Item *httpapi.ChannelResponse `json:"item"`
			}{}, toolErr("没有权限：创建渠道")
		}
		var rawCfg json.RawMessage
		if in.Config != nil {
			b, err := json.Marshal(in.Config)
			if err != nil {
				return nil, struct {
					Item *httpapi.ChannelResponse `json:"item"`
				}{}, err
			}
			rawCfg = b
		}
		p := &httpapi.ChannelPayload{
			Name:     in.Name,
			Type:     in.Type,
			Config:   rawCfg,
			Template: in.Template,
			Enabled:  in.Enabled,
		}
		if err := httpapi.ValidateChannelPayload(p); err != nil {
			return nil, struct {
				Item *httpapi.ChannelResponse `json:"item"`
			}{}, err
		}
		if err := httpapi.ValidateChannelType(d.Notifiers, p); err != nil {
			return nil, struct {
				Item *httpapi.ChannelResponse `json:"item"`
			}{}, err
		}
		enabled := true
		if p.Enabled != nil {
			enabled = *p.Enabled
		}
		c := &store.Channel{
			Name:     p.Name,
			Type:     p.Type,
			Config:   string(p.Config),
			Template: p.Template,
			Enabled:  enabled,
			OwnerID:  &u.ID, // MCP 一律建个人渠道；共享渠道请走 Web（admin）
		}
		if err := d.St.CreateChannel(ctx, c); err != nil {
			return nil, struct {
				Item *httpapi.ChannelResponse `json:"item"`
			}{}, err
		}
		audit(ctx, d, u, "mcp.channel.create", c.Name)
		out, err := d.St.GetChannel(ctx, c.ID)
		if err != nil {
			return nil, struct {
				Item *httpapi.ChannelResponse `json:"item"`
			}{}, fmt.Errorf("渠道已创建但回读失败: %w", err)
		}
		resp := httpapi.ToChannelResponse(out)
		resp.Config = httpapi.MaskedConfig(d.Notifiers, out.Type, out.Config)
		return nil, struct {
			Item *httpapi.ChannelResponse `json:"item"`
		}{Item: resp}, nil
	})
}
