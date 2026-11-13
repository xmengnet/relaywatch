package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	httpapi "relaywatch/internal/http"
	"relaywatch/internal/store"
)

// addMonitorTools 注册监控相关工具。校验与视图全部复用 httpapi 的共享
// 逻辑（export_api.go 导出面），保证 MCP 与 Web 两种入口行为一致。
//
// 输出统一用 any（省略 output schema）：MonitorResponse.SourceConfig 是
// json.RawMessage，静态推导的 schema 与实际值不符反而会校验失败。

// monitorInput 与 httpapi.MonitorPayload 同构（AI 面向 JSON Schema，
// 字段说明用 jsonschema tag），经 JSON 往返后走同一校验链。
type monitorInput struct {
	Name                string         `json:"name" jsonschema:"监控名称，必填"`
	SourceType          string         `json:"source_type" jsonschema:"源类型，必填；取值见 list_source_types"`
	SourceConfig        map[string]any `json:"source_config,omitempty" jsonschema:"源配置对象，字段取值见 list_source_types 对应类型的 fields"`
	VersionPrefix       string         `json:"version_prefix,omitempty" jsonschema:"版本号前缀过滤，如 v"`
	VersionSuffix       string         `json:"version_suffix,omitempty" jsonschema:"版本号后缀过滤"`
	VersionIncludeRegex string         `json:"version_include_regex,omitempty" jsonschema:"仅纳入匹配该正则的版本"`
	VersionIgnoreRegex  string         `json:"version_ignore_regex,omitempty" jsonschema:"忽略匹配该正则的版本"`
	FromPattern         string         `json:"from_pattern,omitempty" jsonschema:"版本号重写正则（配合 to_pattern）"`
	ToPattern           string         `json:"to_pattern,omitempty" jsonschema:"版本号重写目标"`
	IntervalSec         int            `json:"interval_sec,omitempty" jsonschema:"检查间隔秒数，0 用默认 300"`
	Enabled             *bool          `json:"enabled,omitempty" jsonschema:"是否启用，默认 true"`
	NotifyCooldownSec   int            `json:"notify_cooldown_sec,omitempty" jsonschema:"通知冷却秒数"`
	Tags                []string       `json:"tags,omitempty" jsonschema:"标签列表"`
	Note                string         `json:"note,omitempty" jsonschema:"备注"`
	ChannelIDs          []int64        `json:"channel_ids,omitempty" jsonschema:"绑定的告警渠道 ID 列表（见 list_channels）"`
	ReferenceMonitorID  *int64         `json:"reference_monitor_id,omitempty" jsonschema:"参照监控 ID：以上游为准比较本地版本"`
	CurrentVersion      *string        `json:"current_version,omitempty" jsonschema:"手动设置当前版本基线（一般不用）"`
	State               *string        `json:"state,omitempty" jsonschema:"手动设置状态（一般不用）"`
}

func (in *monitorInput) payload() (*httpapi.MonitorPayload, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var p httpapi.MonitorPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (d Deps) validateMonitorPayload(ctx context.Context, u *store.User, p *httpapi.MonitorPayload, selfID int64) error {
	if err := httpapi.ValidateMonitorPayload(p); err != nil {
		return err
	}
	if err := httpapi.ValidateSourceConfig(d.Sources, p); err != nil {
		return err
	}
	if err := httpapi.ValidateChannelIDs(ctx, d.St, u, p.ChannelIDs); err != nil {
		return err
	}
	return httpapi.ValidateReference(ctx, d.St, selfID, u.ID, p.ReferenceMonitorID)
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"监控 ID"`
}

type pageInput struct {
	ID      int64 `json:"id" jsonschema:"监控 ID"`
	Page    int   `json:"page,omitempty" jsonschema:"页码，从 1 起，默认 1"`
	PerPage int   `json:"per_page,omitempty" jsonschema:"每页条数，默认 50，最大 200"`
}

func pageOf(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	return page, perPage
}

// monitorView 监控响应 + 参照监控名，与 Web 列表/详情字段一致。
func (d Deps) monitorView(ctx context.Context, m *store.Monitor) *httpapi.MonitorResponse {
	resp := httpapi.ToMonitorResponse(m)
	if m.ReferenceMonitorID != nil {
		resp.ReferenceMonitorName = httpapi.RefMonitorName(d.St, ctx, *m.ReferenceMonitorID)
	}
	return resp
}

func addMonitorTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_monitors",
		Description: "列出当前用户可见的监控项（admin 可见全部）。可按状态、关键词、标签过滤。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		State   string `json:"state,omitempty" jsonschema:"按状态过滤：ok|has_update|error|missing|lagging|unknown"`
		Search  string `json:"search,omitempty" jsonschema:"按名称关键词搜索"`
		Tag     string `json:"tag,omitempty" jsonschema:"按标签过滤"`
		Page    int    `json:"page,omitempty" jsonschema:"页码，默认 1"`
		PerPage int    `json:"per_page,omitempty" jsonschema:"每页条数，默认 50"`
	}) (*mcp.CallToolResult, any, error) {
		page, perPage := pageOf(in.Page, in.PerPage)
		f := store.MonitorFilter{
			State:   store.MonitorState(in.State),
			Tag:     in.Tag,
			Search:  in.Search,
			OwnerID: httpapi.ScopeUserOwnerID(ctxUser(ctx)),
			Page:    page, PerPage: perPage,
		}
		items, total, err := d.St.ListMonitors(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		out := make([]*httpapi.MonitorResponse, 0, len(items))
		for _, m := range items {
			out = append(out, d.monitorView(ctx, m))
		}
		return nil, map[string]any{"items": out, "total": total}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_monitor",
		Description: "查看单个监控的详情（含当前版本、最新版本、状态、绑定渠道）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		m, err := accessMonitor(ctx, d, in.ID)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"item": d.monitorView(ctx, m)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_monitor_history",
		Description: "查看监控的检查历史（每次检查的时间、结果、耗时）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageInput) (*mcp.CallToolResult, any, error) {
		if _, err := accessMonitor(ctx, d, in.ID); err != nil {
			return nil, nil, err
		}
		page, perPage := pageOf(in.Page, in.PerPage)
		items, total, err := d.St.ListCheckHistory(ctx, in.ID, page, perPage)
		if err != nil {
			return nil, nil, err
		}
		out := make([]*httpapi.CheckHistoryResponse, 0, len(items))
		for _, it := range items {
			out = append(out, httpapi.ToCheckHistoryResponse(it))
		}
		return nil, map[string]any{"items": out, "total": total}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_monitor_changes",
		Description: "查看监控的版本变更记录（新旧版本、发现时间、是否已确认）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in pageInput) (*mcp.CallToolResult, any, error) {
		if _, err := accessMonitor(ctx, d, in.ID); err != nil {
			return nil, nil, err
		}
		page, perPage := pageOf(in.Page, in.PerPage)
		items, total, err := d.St.ListVersionChanges(ctx, in.ID, page, perPage)
		if err != nil {
			return nil, nil, err
		}
		out := make([]*httpapi.VersionChangeResp, 0, len(items))
		for _, it := range items {
			out = append(out, httpapi.ToVersionChangeResponse(it))
		}
		return nil, map[string]any{"items": out, "total": total}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_monitor",
		Description: "创建监控项。先用 list_source_types 确认 source_type 与 source_config 写法；需要通知时先用 list_channels 拿渠道 ID 填 channel_ids。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monitorInput) (*mcp.CallToolResult, any, error) {
		u := ctxUser(ctx)
		if !u.Can(store.PermCreateMonitor) {
			return nil, nil, toolErr("没有权限：创建监控")
		}
		p, err := in.payload()
		if err != nil {
			return nil, nil, err
		}
		if err := d.validateMonitorPayload(ctx, u, p, 0); err != nil {
			return nil, nil, err
		}
		if p.IntervalSec == 0 {
			p.IntervalSec = 300
		}
		m := httpapi.PayloadToMonitor(p)
		m.State = store.StateUnknown
		m.OwnerID = u.ID // 归属强制当前用户，与 Web 入口一致
		if err := d.St.CreateMonitor(ctx, m); err != nil {
			return nil, nil, err
		}
		if m.ReferenceMonitorID != nil && d.Evaluator != nil {
			_ = d.Evaluator.EvaluatePair(ctx, m.ID)
		}
		audit(ctx, d, u, "mcp.monitor.create", m.Name)
		out, err := d.St.GetMonitor(ctx, m.ID)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"item": d.monitorView(ctx, out)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_monitor",
		Description: "更新监控项配置（全量字段，未传的配置字段会被清空为默认值，与网页编辑保存行为一致）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		monitorInput
		ID int64 `json:"id" jsonschema:"要修改的监控 ID"`
	}) (*mcp.CallToolResult, any, error) {
		m, err := accessMonitor(ctx, d, in.ID)
		if err != nil {
			return nil, nil, err
		}
		u := ctxUser(ctx)
		if !u.CanWrite() {
			return nil, nil, toolErr("只读用户无权执行该操作")
		}
		p, err := in.payload()
		if err != nil {
			return nil, nil, err
		}
		if err := d.validateMonitorPayload(ctx, u, p, in.ID); err != nil {
			return nil, nil, err
		}
		httpapi.ApplyMonitorPayload(m, p)
		if err := d.St.UpdateMonitor(ctx, m); err != nil {
			return nil, nil, err
		}
		// 手动改基线（current_version/state）走定向更新，不随整行编辑覆盖检查状态
		if p.CurrentVersion != nil || p.State != nil {
			var state *store.MonitorState
			if p.State != nil {
				st := store.MonitorState(*p.State)
				state = &st
			}
			if err := d.St.SetMonitorBaseline(ctx, in.ID, state, p.CurrentVersion); err != nil {
				return nil, nil, err
			}
		}
		if m.ReferenceMonitorID != nil && d.Evaluator != nil {
			_ = d.Evaluator.EvaluatePair(ctx, in.ID)
		}
		audit(ctx, d, u, "mcp.monitor.update", p.Name)
		out, err := d.St.GetMonitor(ctx, in.ID)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"item": d.monitorView(ctx, out)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "trigger_check",
		Description: "立即对指定监控执行一次检查，返回抓到的最新版本与状态。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		m, err := accessMonitor(ctx, d, in.ID)
		if err != nil {
			return nil, nil, err
		}
		if !ctxUser(ctx).CanWrite() {
			return nil, nil, toolErr("只读用户无权执行该操作")
		}
		if !m.Enabled {
			return nil, nil, toolErr("监控已停用")
		}
		out := d.Checker.Check(ctx, m)
		audit(ctx, d, ctxUser(ctx), "mcp.monitor.check", m.Name)
		return nil, map[string]any{
			"ok": out.OK, "version": out.Version, "state": string(out.State),
			"error": out.Error, "duration_ms": out.DurationMS, "skipped": out.Skipped,
		}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ack_monitor",
		Description: "确认指定监控的新版本（变更标记已读，监控回到 up_to_date，待投递通知作废）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		m, err := accessMonitor(ctx, d, in.ID)
		if err != nil {
			return nil, nil, err
		}
		u := ctxUser(ctx)
		if !u.CanWrite() {
			return nil, nil, toolErr("只读用户无权执行该操作")
		}
		open, err := d.St.OpenVersionChange(ctx, in.ID)
		if err == store.ErrNotFound {
			return nil, nil, toolErr("没有待确认的更新")
		}
		if err != nil {
			return nil, nil, err
		}
		now := time.Now()
		open.AcknowledgedAt = &now
		uid := u.ID
		open.AcknowledgedBy = &uid
		if err := d.St.UpdateVersionChange(ctx, open); err != nil {
			return nil, nil, err
		}
		// 与 Web 端一致：作废失败仅记日志，不阻断 ack
		if _, err := d.St.CancelOpenDeliveries(ctx, in.ID, "acknowledged by user"); err != nil {
			slog.Error("mcp ack cancel deliveries", "err", err)
		}
		upToDate := store.StateUpToDate
		if err := d.St.SetMonitorBaseline(ctx, in.ID, &upToDate, &m.LatestVersion); err != nil {
			return nil, nil, err
		}
		m.State = upToDate
		m.CurrentVersion = m.LatestVersion
		audit(ctx, d, u, "mcp.monitor.ack", m.Name)
		return nil, map[string]any{"item": d.monitorView(ctx, m)}, nil
	})
}
