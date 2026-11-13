---
name: relaywatch
description: Monitor software sources for new upstream releases via RelayWatch (self-hosted). Use when the user asks to track package/tool versions, check for updates, create release monitors, acknowledge new versions, or troubleshoot notifications for their RelayWatch instance.
license: MIT
---

# RelayWatch

RelayWatch 是自托管的版本更新监控服务：追踪软件源（GitHub/GitLab 发行版、Arch/Debian 仓库、npm/PyPI/容器镜像等 30+ 源类型），发现新版本时通过告警渠道推送通知。

**服务地址与凭据由用户提供**：本 skill 不内置任何 URL 或 Token。

## 第一步：获取接入信息

向用户确认两项（或读取环境变量 `RELAYWATCH_URL` / `RELAYWATCH_TOKEN`）：

1. **Base URL**：RelayWatch 实例地址，例如 `<BASE_URL>`
2. **API Token**：用户在 Web 端「个人资料 → API Token」创建，形如 `rw_...`

安全要求：Token 只放在内存或环境变量里调用 API；不要写入任何文件、日志或对话外发的请求。

## 第二步：选择接入方式

**方式 A — MCP（推荐，功能完整）**：如果宿主支持 MCP，引导用户执行一次连接命令，之后直接使用 RelayWatch 提供的 MCP 工具（list_source_types / list_monitors / create_monitor / trigger_check / ack_monitor / list_channels / create_channel 等）：

```bash
claude mcp add --transport http relaywatch <BASE_URL>/mcp \
  --header "Authorization: Bearer <TOKEN>"
```

**方式 B — REST API**：无法使用 MCP 时直接调 HTTP API（见下表）。认证：`Authorization: Bearer <TOKEN>` 请求头；JSON 请求/响应。

## REST API 详解

所有响应为 JSON。错误格式统一为 `{"message": "...", "code": "..."}`（HTTP 状态码 4xx/5xx）；分页响应统一为 `{"items": [...], "total": N}`；时间字段均为 RFC3339（如 `2026-01-02T15:04:05Z`）。

### GET /api/v1/monitors — 监控列表

查询参数（均可选）：`state`（ok|has_update|error|missing|lagging|unknown）、`q`（名称搜索）、`tag`（标签）、`sort`/`order`、`page`（默认 1）、`per_page`（默认 25）。

响应 `items[]` 元素字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 监控 ID（后续操作都用它） |
| `name` | string | 监控名称 |
| `source_type` | string | 源类型，如 `github_release` |
| `source_config` | object | 源配置（结构随 source_type 而定） |
| `state` | string | 当前状态：ok / has_update / error / missing / lagging / unknown |
| `current_version` | string | 当前基线版本（已采纳的） |
| `latest_version` | string | 最近探测到的上游版本 |
| `enabled` | bool | 是否启用 |
| `last_checked_at` | string? | 上次检查时间 |
| `last_error` | string? | 上次失败的错误信息 |
| `consecutive_failures` | int | 连续失败次数 |
| `interval_sec` | int | 检查间隔秒数 |
| `notify_cooldown_sec` | int | 通知冷却秒数 |
| `tags` | string[] | 标签 |
| `note` | string? | 备注 |
| `channel_ids` | int64[] | 绑定的告警渠道 ID |
| `reference_monitor_id` | int64? | 参照监控 ID（双轨对比） |
| `created_at` / `updated_at` | string | 创建 / 更新时间 |

### GET /api/v1/monitors/{id} — 监控详情

同列表元素字段；`reference_monitor_id` 存在时额外含 `reference_monitor_name`。

### POST /api/v1/monitors — 创建监控

请求体（`name`、`source_type` 必填，其余可选；各 source_type 需要的 config 字段先查 `GET /api/v1/sources/types`）：

```json
{
  "name": "linux",
  "source_type": "github_release",
  "source_config": {"repo": "torvalds/linux"},
  "version_prefix": "", "version_suffix": "",
  "version_include_regex": "", "version_ignore_regex": "",
  "from_pattern": "", "to_pattern": "",
  "interval_sec": 300, "enabled": true,
  "notify_cooldown_sec": 0,
  "tags": ["kernel"], "note": "",
  "channel_ids": [1],
  "reference_monitor_id": null,
  "current_version": null, "state": null
}
```

`channel_ids` 必须是当前用户可见渠道；`reference_monitor_id` 参照监控不能已有上游参照（单层）。响应：`{"item": <监控对象>}`（字段同列表）。

常见错误：`未知源类型` / `source_config 校验失败信息` / `绑定的渠道不存在（#N）` / 403 无权限（viewer）。

### PATCH /api/v1/monitors/{id} — 更新监控

请求体同创建（全量语义：与网页编辑保存一致，未传的配置字段会清空）。`current_version` / `state` 传值时用于手动改基线。响应：`{"item": <监控对象>}`。

### POST /api/v1/monitors/{id}/check — 立即检查

无请求体。响应：

| 字段 | 类型 | 说明 |
|------|------|------|
| `ok` | bool | 本次抓取是否成功 |
| `version` | string? | 抓到的最新版本 |
| `state` | string | 检查后状态 |
| `error` | string? | 失败原因 |
| `duration_ms` | int64 | 耗时 |
| `skipped` | bool | 是否被跳过（限流/抖动） |

注意：监控已停用时返回 400。

### POST /api/v1/monitors/{id}/ack — 确认新版本

无请求体。把基线提升到 `latest_version`，监控回到 ok，该变更周期的待投递通知作废。响应：`{"item": <更新后的监控对象>}`。没有待确认更新时返回 400 `没有待确认的更新`。

### GET /api/v1/monitors/{id}/history — 检查历史

参数：`page`、`per_page`。响应 `items[]`：`id`、`monitor_id`、`checked_at`、`duration_ms`、`ok`、`version?`、`error?`。

### GET /api/v1/monitors/{id}/changes — 版本变更

参数：`page`、`per_page`。响应 `items[]`：`id`、`monitor_id`、`old_version?`、`new_version`、`url?`、`detected_at`、`acknowledged_at?`（null 表示待确认）。

### GET /api/v1/channels — 渠道列表

无参数（admin 返回全部，普通用户返回自己的 + 全局共享）。响应 `items[]`：`id`、`name`、`type`（如 webhook / 邮件 / IM 类型）、`config`（字符串化的 JSON；他人的敏感字段会被掩码）、`template?`、`enabled`、`created_at`、`updated_at`。

### POST /api/v1/channels — 创建渠道

先查 `GET /api/v1/notifiers/types` 获取 `type` 与 config 字段说明（`type: "secret"` 的字段是敏感项）。请求体：`name`（必填）、`type`（必填）、`config`（对象，按类型定义）、`template?`、`enabled?`（默认 true）。响应：`{"item": <渠道对象>}`。

### GET /api/v1/deliveries — 投递记录（排查"没收到通知"）

参数：`monitor_id?`、`channel_id?`、`status?`（pending|sent|failed|cancelled）、`page`、`per_page`。响应 `items[]`：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | int64 | 投递 ID |
| `monitor_id` / `monitor_name` | int64 / string? | 关联监控 |
| `channel_id` / `channel_name` | int64 / string? | 目标渠道 |
| `kind` | string | update（版本更新）/ system 等 |
| `status` | string | pending / sent / failed / cancelled |
| `attempts` | int | 已尝试次数 |
| `next_attempt_at` | string? | 下次重试时间 |
| `last_error` | string? | 失败原因 |
| `created_at` / `sent_at?` | string | 创建 / 发送成功时间 |

### GET /api/v1/stats — 仪表盘统计

无参数。响应：`{"total": N, "ok": N, "has_update": N, "error": N, "missing": N, "unknown": N, "lagging": N}`，按当前用户可见范围统计。

### GET /api/v1/sources/types 与 GET /api/v1/notifiers/types — 类型发现

无参数。响应 `items[]`：每项含 `type`（标识符，写入 source_type / 渠道 type）、`label`（显示名）、`fields[]`。`fields[]` 每项含 `key`（config 里的字段名）、`label`、`help`（字段说明）、`placeholder`（示例值）、部分类型还有 `options`（可选值）。**创建监控/渠道前务必先查这里**，按 fields 拼配置对象。

## 行为约定

- 数据隔离：Token 对应的用户只能看到自己的监控（admin 可见全部）；「不存在」也可能意味着无权访问
- 只读角色（viewer）调用写接口会收到 403 `没有权限`；代用户执行写操作（建监控/改配置/确认）前先复述意图
- 不支持的操作（删除监控、用户管理、系统配置）引导用户到 Web 界面操作
- 排查"没收到通知"：先看监控状态与 `changes`（确认有待确认变更），再看 `deliveries` 的 `status` 与 `last_error`
