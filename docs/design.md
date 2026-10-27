# RelayWatch 设计方案

版本：0.5.0
对应需求：[requirements.md](./requirements.md) v0.3.1
实现进度：M1–M6 全部完成（含双轨对比 §17 与 SSE 实时推送 §8.8），nvchecker 兼容源（§18 P0–P3 及正则重写）已实现并全量测试通过。

变更：v0.1 → v0.2 根据评审意见修订，同步需求 v0.2：新增 api_tokens / settings 表、deliveries 调度字段、版本过滤与变更周期语义、调度抖动与 per-host 限速、nvchecker 导入、手动检查改为同步返回、CSP 修正、`relaywatch rekey` 子命令。

v0.2 → v0.2.1 修正三处：`gjson` 版本改为实测的 v1.20.0；调度 jitter 改为由监控 ID 哈希推导的确定性因子（原方案重启后重新随机会导致到期时间跳变、触发多余检查）；补充 `deliveries` 的 `canceled` 终态与 deferred 补发前的四项校验，明确用户 ack/忽略时待投递记录作废。

v0.2.1 → v0.3.0（实现期修订，与 M1–M4 代码同步）：
- 前端 TypeScript 固定 5.9（vue-tsc 3.3.13 与 TS7 不兼容，`ERR_PACKAGE_PATH_NOT_EXPORTED`）
- jitter 公式修正：`Sum64()%2000/2000*0.2-0.1`（原 `%2001/10000` 上界恰好越界到 +0.1）
- 敏感字段加密 M4 已落地：渠道 config / 监控 source_config / OIDC client_secret 落库前 AES-256-GCM 加密；密文 `enc:` 前缀；明文兼容读取；`secret_key` 未设置拒绝启动
- 手动重发增加 Force 语义：跳过开放变更校验（用户明确要求再发一次），版本号仍取发送时刻 latest_version
- 认证中间件顺序实现为 `Recoverer → RealIP → 安全头 → Session/Bearer → RequireAuth → RequireCSRF`，API Token（Bearer）调用豁免 CSRF
- HTTP 层一律经由脱敏视图（views.go）输出，密码哈希与内部字段不出 API

v0.3.0 → v0.4.0（M6 规划增补）：
- 新增 §17 双轨对比设计：放弃 §14.3 原提的 `group_key` 对称分组，改为跟随监控上的有向引用 `reference_monitor_id`；对比前按源类型做版本对齐（`aur` 剥离 pkgrel/epoch——实测 `0.1.19-1` 会被 semver 解析为预发布，不剥离将永远误报落后）；对比事件（进入落后 / 差距扩大 / 已追平）复用 deliveries 投递管线（新增 `kind` / `context` 列）
- §3.1 表结构、§7.3 API、§12 测试策略、§14.3 扩展方向、§16 风险相应标注；SSE（§8.8）与 §17 同为 M6 范围

v0.4.0 → v0.5.0（M7 nvchecker 全面兼容与扩展源设计增补）：
- 新增 §18 nvchecker 全面兼容与监控源扩展设计：全面对标 nvchecker 官方 44 种 source，建立分类矩阵、参数映射表、导入器增强规范与分批实施路线（P0–P3）；
- 核心提取流水线增强：新增 `from_pattern`/`to_pattern` 正则重写、候选多版本统一排序挑选（`PickBestVersion`）；
- 提炼 nvchecker 源码成熟避坑实践：涵盖 OCI V2 认证探针、GitHub 限流重试与 GraphQL 查询、Maven XML 坐标定位、Yanked/Prerelease 过滤；
- 确立 Web 多用户环境下的安全与跨平台规范：对 `cmd` 实行严格的配置禁用与管理员准入，对本地 `pacman`/`alpm` 数据库通过官方 REST API 进行透明代换。

## 1. 技术选型

| 维度 | 选择 | 理由 |
|---|---|---|
| 语言 | Go 1.27+ | 单二进制、交叉编译方便、并发模型适合调度 |
| HTTP 路由 | `chi` v5 | 轻量、中间件友好、标准库兼容 |
| 数据库 | SQLite（`modernc.org/sqlite`） | 纯 Go 无 cgo，交叉编译无痛；WAL 模式足够 |
| 迁移 | 自研轻量迁移（`embed` + 版本表） | 避免引入重型 ORM/迁移框架 |
| 前端框架 | Vue 3.5 + Vuetify 4.3 | 管理后台形态，组件覆盖最全；与 `dufs-material-assets` 同源 |
| 前端构建 | Vite 8 + TypeScript 7 | 秒级冷启动，HMR；产物 `go:embed` 进二进制 |
| 前端路由 | vue-router 5.4 | 官方路由，Vuetify 4 已适配 |
| 前端状态 | Pinia 4.1 | 官方状态库，TS 友好 |
| 实时更新 | SSE（`text/event-stream`） | 单向推送足够，比 WebSocket 简单，自带重连 |
| 认证 | `golang.org/x/crypto/bcrypt`、`coreos/go-oidc/v3` | 成熟稳定 |
| 版本比较 | `Masterminds/semver/v3` + 自定义回退 | 覆盖绝大多数版本格式 |
| JSON 提取 | `tidwall/gjson` | `http_json` 源的路径查询；比 JSONPath 实现轻、语法直观 |
| 调度 | 自研 ticker + 基于 `last_checked_at` 的到期判断 | 见 §5；cron 不适合「每条记录自己的间隔」 |
| 配置 | 文件（YAML）+ 环境变量覆盖 | 运维友好 |
| 日志 | `log/slog` | 标准库，结构化 |
| 测试 | Go: `testing` + `httptest`；前端: Vitest | — |

前端细节（布局、组件、状态、可访问性）见 §8。

**关于「单二进制」**：构建时需要 Node + pnpm，运行时零依赖。`make build` 先 `pnpm build` 产出 `web/dist`，再由 `go:embed` 打进二进制。用户拿到的是单个可执行文件。Gitea、Grafana 都是这个模式——构建依赖和部署形态是两件事。

**关于 `modernc.org/sqlite`**：纯 Go 实现，无 cgo，代价是比 `mattn/go-sqlite3` 略慢，对本项目负载完全够用，换来交叉编译零配置。

## 2. 目录结构

```
relaywatch/
├── cmd/
│   └── relaywatch/
│       └── main.go              # 入口：加载配置、初始化、启动 HTTP
├── internal/
│   ├── config/                  # 配置加载与校验
│   │   ├── config.go
│   │   └── config_test.go
│   ├── store/                   # 数据访问层
│   │   ├── store.go             # 接口定义
│   │   ├── sqlite.go            # SQLite 实现
│   │   ├── migrations/          # embed 的 .sql 迁移文件
│   │   │   ├── 0001_init.sql
│   │   │   └── ...
│   │   └── models.go            # 领域模型
│   ├── auth/                    # 认证与授权
│   │   ├── local.go             # 本地账户
│   │   ├── oidc.go              # OIDC 流程
│   │   ├── session.go           # 会话管理
│   │   ├── middleware.go        # 鉴权中间件
│   │   └── password.go
│   ├── source/                  # 监控源（插件式）
│   │   ├── source.go            # 接口 + 注册表
│   │   ├── github.go
│   │   ├── gitlab.go
│   │   ├── gitea.go
│   │   ├── aur.go
│   │   ├── archpkg.go
│   │   ├── pypi.go
│   │   ├── npm.go
│   │   ├── crates.go
│   │   ├── gomodule.go
│   │   ├── httpjson.go
│   │   ├── httpregex.go
│   │   ├── docker.go
│   │   └── version.go           # 版本规范化与比较
│   ├── notifier/                # 推送渠道（插件式）
│   │   ├── notifier.go          # 接口 + 注册表
│   │   ├── webhook.go
│   │   ├── telegram.go
│   │   ├── discord.go
│   │   ├── slack.go
│   │   ├── ntfy.go
│   │   ├── gotify.go
│   │   ├── smtp.go
│   │   ├── feishu.go
│   │   ├── wecom.go
│   │   ├── dingtalk.go
│   │   ├── pushdeer.go
│   │   ├── serverchan.go
│   │   ├── bark.go
│   │   └── template.go          # 消息模板
│   ├── check/                   # 检查编排
│   │   ├── checker.go           # 单条检查逻辑
│   │   ├── state.go             # 状态机
│   │   └── history.go
│   ├── scheduler/               # 调度器
│   │   ├── scheduler.go
│   │   ├── ratelimit.go         # per-host 令牌桶
│   │   └── scheduler_test.go
│   ├── importer/                # nvchecker TOML / JSON 导入转换
│   │   ├── nvchecker.go
│   │   └── importer_test.go
│   ├── http/                    # HTTP 层
│   │   ├── server.go            # 路由注册、中间件链
│   │   ├── handlers_*.go        # 各资源 handler
│   │   ├── middleware.go        # 日志、恢复、CSRF、限流
│   │   ├── sse.go               # 实时进度推送
│   │   └── render.go            # JSON 响应、错误封装
│   ├── crypto/                  # 敏感字段加解密
│   │   └── secretbox.go
│   └── audit/                   # 审计日志
│       └── audit.go
├── web/                         # Vue 前端（详见 §8.2）
│   ├── index.html
│   ├── package.json
│   ├── pnpm-lock.yaml
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── src/
│   │   ├── main.ts              # 入口
│   │   ├── App.vue
│   │   ├── plugins/vuetify.ts   # Vuetify 实例与主题
│   │   ├── router/index.ts      # 路由表 + 守卫
│   │   ├── stores/              # Pinia：auth、monitors、channels、ui
│   │   ├── api/                 # client.ts + 各资源封装 + types.ts
│   │   ├── layouts/             # DefaultLayout、BlankLayout
│   │   ├── views/               # 各页面
│   │   ├── components/          # StatusChip、SourceConfigForm 等
│   │   ├── composables/         # useApi、useSse、useConfirm、useToast
│   │   ├── i18n/                # zh-Hans、en
│   │   └── styles/global.css
│   ├── dist/                    # 构建产物（git 忽略）
│   └── embed.go                 # go:embed all:dist
├── docs/
│   ├── requirements.md
│   └── design.md                # 本文档
├── config.example.yaml
├── go.mod
├── Makefile
├── Dockerfile
└── README.md
```

## 3. 数据模型

### 3.1 表结构

```sql
-- 用户
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    email         TEXT,
    password_hash TEXT,              -- OIDC 用户为 NULL
    role          TEXT    NOT NULL DEFAULT 'viewer',  -- admin | viewer
    oidc_provider TEXT,              -- 来自哪个 IdP
    oidc_subject  TEXT,              -- IdP 的 sub
    totp_secret   TEXT,              -- 预留
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    last_login_at DATETIME,
    UNIQUE (oidc_provider, oidc_subject)
);

-- 会话
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,     -- 随机 32 字节 base64url
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    user_agent TEXT,
    ip         TEXT,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- API Token（Bearer Token 认证）
CREATE TABLE api_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,   -- sha256(token)，明文只在创建时返回一次
    created_at  DATETIME NOT NULL,
    expires_at  DATETIME,               -- NULL = 不过期
    last_used_at DATETIME,
    revoked_at  DATETIME
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

-- OIDC Provider 配置
CREATE TABLE oidc_providers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,   -- 显示名，如 "GitHub"
    issuer        TEXT NOT NULL,
    client_id     TEXT NOT NULL,
    client_secret TEXT NOT NULL,          -- 加密存储
    scopes        TEXT NOT NULL DEFAULT 'openid email profile',
    claim_rules   TEXT,                   -- JSON：登录限制规则
    icon_url      TEXT NOT NULL DEFAULT '', -- 登录页按钮图标；空 = 按内置 name 映射（0003 迁移新增）
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL
);

-- 通知渠道
CREATE TABLE channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL,             -- telegram | webhook | ...
    config     TEXT NOT NULL,             -- JSON，敏感字段加密
    template   TEXT,                      -- 自定义模板，空则用默认
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- 监控记录
CREATE TABLE monitors (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    source_type    TEXT NOT NULL,
    source_config  TEXT NOT NULL,         -- JSON，敏感字段加密
    version_prefix TEXT,
    version_suffix TEXT,
    version_include_regex TEXT,           -- 只有匹配的版本才参与更新判断
    version_ignore_regex TEXT,            -- 匹配的版本跳过（记入历史，不改状态）
    interval_sec   INTEGER NOT NULL,      -- 检查间隔
    enabled        INTEGER NOT NULL DEFAULT 1,
    state          TEXT NOT NULL DEFAULT 'unknown',  -- unknown | up_to_date | has_update | error | missing
    current_version TEXT,                 -- 基线版本（上游回退时不跟随下调）
    latest_version  TEXT,                 -- 最近一次查到的版本
    last_checked_at DATETIME,
    last_success_at DATETIME,
    last_change_at  DATETIME,             -- 最近一次检测到新版本的时间（列表按此排序）
    last_error      TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    notify_cooldown_sec  INTEGER NOT NULL DEFAULT 0,
    last_notified_at     DATETIME,
    tags           TEXT,                  -- JSON 数组
    note           TEXT,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);
CREATE INDEX idx_monitors_state ON monitors(state);
CREATE INDEX idx_monitors_enabled ON monitors(enabled);

-- 监控 ↔ 渠道 多对多
CREATE TABLE monitor_channels (
    monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    PRIMARY KEY (monitor_id, channel_id)
);

-- 检查历史
CREATE TABLE check_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at  DATETIME NOT NULL,
    duration_ms INTEGER NOT NULL,
    ok          INTEGER NOT NULL,
    version     TEXT,
    error       TEXT
);
CREATE INDEX idx_check_history_monitor ON check_history(monitor_id, checked_at DESC);

-- 版本变更历史
CREATE TABLE version_changes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    old_version TEXT,
    new_version TEXT NOT NULL,
    detected_at DATETIME NOT NULL,
    acknowledged_at DATETIME,      -- 用户确认/忽略
    acknowledged_by INTEGER REFERENCES users(id)
);
CREATE INDEX idx_version_changes_monitor ON version_changes(monitor_id, detected_at DESC);

-- 通知投递记录
CREATE TABLE deliveries (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id   INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    channel_id   INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    change_id    INTEGER REFERENCES version_changes(id) ON DELETE SET NULL,
    payload      TEXT,                -- 实际发送内容
    status       TEXT NOT NULL,       -- pending | sent | failed | deferred(静默期推迟) | canceled(作废)
    attempts     INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME,         -- 下次重试/补发时间，worker 按此取任务
    last_error   TEXT,
    created_at   DATETIME NOT NULL,
    sent_at      DATETIME
);
CREATE INDEX idx_deliveries_pending ON deliveries(status, next_attempt_at);

-- 审计日志
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id),
    action     TEXT NOT NULL,
    target     TEXT,
    detail     TEXT,
    ip         TEXT,
    created_at DATETIME NOT NULL
);

-- 迁移版本
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL
);

-- 运行时系统设置（系统告警渠道、界面偏好等可在 UI 修改的项；
-- 端口、密钥等启动参数仍在 config.yaml）
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,        -- JSON
    updated_at DATETIME NOT NULL
);
```

上表为 M1–M5 已实现的结构；M6 双轨对比的增量列（`monitors.reference_monitor_id`、`compare_*`，`deliveries.kind`/`context`）见 §17.1。

### 3.2 设计要点

- **敏感字段加密**：`channels.config`、`oidc_providers.client_secret`、`monitors.source_config` 中的 token 字段，入库前用 AES-256-GCM 加密。密钥来自环境变量 `RELAYWATCH_SECRET_KEY`（32 字节 base64）或配置文件。
- **JSON 字段**：SQLite 无原生 JSON 类型，用 TEXT 存储并在应用层序列化。查询频繁的字段（如 `tags`）如果将来需要索引，可加 generated column。
- **历史保留策略**：`check_history` 增长最快，配置保留天数（默认 30 天），后台定时清理。
- **外键约束**：SQLite 需 `PRAGMA foreign_keys = ON`，在连接初始化时设置。
- **密钥轮换**：`secret_key` 支持 `old_key` 过渡——配置里同时给出新旧密钥时，读取先尝试新密钥再尝试旧密钥；`relaywatch rekey` 子命令用新密钥重加密全部敏感字段后提示移除旧密钥。密钥丢失则敏感配置无法恢复，需重新填写，文档需强调备份 `secret_key`。

## 4. 核心接口设计

### 4.1 监控源接口

```go
package source

// Result 是一次版本查询的结果。
type Result struct {
    Version     string    // 原始版本字符串，如 "v0.1.19"
    URL         string    // 上游页面链接，用于通知
    PublishedAt time.Time // 可选：上游发布时间
    Extra       map[string]any // 可选：附加信息，如 release notes
}

// Source 是所有监控源必须实现的接口。
type Source interface {
    // Type 返回源类型标识，与 source_type 字段一致。
    Type() string
    // Validate 校验配置是否合法，在保存监控记录时调用。
    Validate(cfg map[string]any) error
    // Fetch 查询当前版本。ctx 用于超时和取消。
    Fetch(ctx context.Context, cfg map[string]any) (*Result, error)
}

// Registry 是源类型的注册表。
type Registry struct {
    mu      sync.RWMutex
    sources map[string]Source
}

func (r *Registry) Register(s Source)
func (r *Registry) Get(typ string) (Source, bool)
func (r *Registry) Types() []string
```

**注册方式**：每个源在 `init()` 中调用 `Register`，或在 `cmd/main.go` 显式注册（推荐后者，便于测试和依赖注入）。

### 4.2 通知渠道接口

```go
package notifier

// Message 是一条待发送的通知。
type Message struct {
    Title    string
    Body     string
    Markdown bool      // 渠道是否支持 Markdown
    URL      string    // 相关链接
    Monitor  MonitorInfo
    Change   ChangeInfo
}

type MonitorInfo struct {
    Name       string
    SourceType string
    Tags       []string
}

type ChangeInfo struct {
    OldVersion string
    NewVersion string
    DetectedAt time.Time
}

// Notifier 是推送渠道必须实现的接口。
type Notifier interface {
    Type() string
    Validate(cfg map[string]any) error
    // Send 发送一条消息。返回 error 表示需要重试。
    Send(ctx context.Context, cfg map[string]any, msg *Message) error
}

// 某些渠道能报告"不可重试错误"（如 400 参数错误），
// 用 sentinel error 区分，避免无谓重试。
var ErrPermanent = errors.New("permanent failure")
```

### 4.3 存储接口

```go
package store

type Store interface {
    // 用户
    CreateUser(ctx context.Context, u *User) error
    UserByUsername(ctx context.Context, name string) (*User, error)
    UserByOIDC(ctx context.Context, provider, subject string) (*User, error)
    UpdateUser(ctx context.Context, u *User) error
    ListUsers(ctx context.Context) ([]*User, error)

    // 会话
    CreateSession(ctx context.Context, s *Session) error
    SessionByID(ctx context.Context, id string) (*Session, error)
    RevokeSession(ctx context.Context, id string) error
    DeleteExpiredSessions(ctx context.Context) (int64, error)

    // API Token
    CreateAPIToken(ctx context.Context, t *APIToken) error
    APITokenByHash(ctx context.Context, hash string) (*APIToken, error)
    ListAPITokens(ctx context.Context, userID int64) ([]*APIToken, error)
    RevokeAPIToken(ctx context.Context, id int64) error
    TouchAPIToken(ctx context.Context, id int64) error // 更新 last_used_at

    // 监控
    CreateMonitor(ctx context.Context, m *Monitor) error
    GetMonitor(ctx context.Context, id int64) (*Monitor, error)
    ListMonitors(ctx context.Context, f MonitorFilter) ([]*Monitor, error)
    UpdateMonitor(ctx context.Context, m *Monitor) error
    DeleteMonitor(ctx context.Context, id int64) error
    SetMonitorState(ctx context.Context, id int64, st MonitorState) error

    // 检查与变更历史
    AddCheckHistory(ctx context.Context, h *CheckHistory) error
    ListCheckHistory(ctx context.Context, monitorID int64, p Pagination) ([]*CheckHistory, int, error)
    AddVersionChange(ctx context.Context, c *VersionChange) error
    UpdateVersionChange(ctx context.Context, c *VersionChange) error // 周期内更新 new_version / ack
    OpenVersionChange(ctx context.Context, monitorID int64) (*VersionChange, error) // 未 ack 的最新一条
    ListVersionChanges(ctx context.Context, monitorID int64, p Pagination) ([]*VersionChange, int, error)

    // 渠道
    CreateChannel(ctx context.Context, c *Channel) error
    ListChannels(ctx context.Context) ([]*Channel, error)
    GetChannel(ctx context.Context, id int64) (*Channel, error)
    UpdateChannel(ctx context.Context, c *Channel) error
    DeleteChannel(ctx context.Context, id int64) error

    // 投递
    AddDelivery(ctx context.Context, d *Delivery) error
    UpdateDelivery(ctx context.Context, d *Delivery) error
    ListPendingDeliveries(ctx context.Context, before time.Time, limit int) ([]*Delivery, error)
    ListDeliveries(ctx context.Context, f DeliveryFilter) ([]*Delivery, error)

    // 系统设置
    GetSetting(ctx context.Context, key string) (string, error)
    SetSetting(ctx context.Context, key, value string) error

    // 审计
    AddAudit(ctx context.Context, a *AuditEntry) error
    ListAudit(ctx context.Context, limit int) ([]*AuditEntry, error)
}
```

用接口抽象存储层，好处是测试可以用内存实现，将来换 PostgreSQL 也不用动业务代码。

## 5. 调度器设计

### 5.1 方案选择

**不用 `robfig/cron`**，因为监控的间隔是"每条记录自己的间隔"（如 5 分钟、1 小时），不是固定时刻表。cron 适合"每天 9 点"，不适合"每 5 分钟"。

**采用自研 ticker 调度**：

```go
type Scheduler struct {
    store     store.Store
    checker   *check.Checker
    tick      time.Duration      // 调度粒度，默认 10s
    maxConc   int                // 最大并发检查数
    sem       chan struct{}
    running   map[int64]bool     // 正在检查的监控，防重入
    hostLimit *ratelimit.Registry // per-host 令牌桶，见 5.2
    mu        sync.Mutex
    wg        sync.WaitGroup
    ctx       context.Context
    cancel    context.CancelFunc
}
```

### 5.2 调度算法

每 `tick` 执行一次：

1. 从数据库查询所有 `enabled = 1` 的监控
2. 对每条记录计算"是否到期"：`next_due = last_checked_at + interval_sec * (1 + jitter)`。抖动避免大量同间隔监控永远同时到期、对上游形成周期性脉冲

   `jitter` **必须是确定性的**，由监控 ID 推导，不能用进程内随机数：

   ```go
   // 取值范围 [-0.1, +0.1)，稳定不变。
   func jitterFactor(id int64) float64 {
       h := fnv.New64a()
       var b [8]byte
       binary.LittleEndian.PutUint64(b[:], uint64(id))
       h.Write(b[:])
       return float64(h.Sum64()%2001)/10000.0 - 0.1
   }
   ```

   理由：若重启后重新随机，`last_checked_at + interval*(1+jitter)` 的结果会跳变。从 `+8%` 变成 `-7%` 会让这条监控提前 15% 到期，重启即触发一轮本不该发生的检查；频繁重启的实例会因此产生不规则脉冲。用 ID 哈希保证同一监控在任何进程生命周期里 jitter 恒定，无需额外存储列。新增监控天然得到分散的相位。
3. 到期的记录提交到任务队列
4. 任务队列受 `sem` 信号量限流
5. 执行前检查 `running[id]`，已在跑则跳过（防重入）
6. 实际发请求前过 **per-host 令牌桶**：按目标主机名（`api.github.com`、`aur.archlinux.org` 等）限速，默认每 host 每分钟 30 次，可在 `config.yaml` 按 host 覆盖。令牌不足时任务延迟到下一轮 tick，不算检查失败

**Retry-After**：源返回 429 且带 `Retry-After` 时，该 host 的令牌桶被冻结到指定时间，本次检查按失败记录（错误信息注明限流）。

**优点**：
- 无需在内存维护复杂的定时器堆
- 进程重启后自动恢复，因为到期判断基于数据库里的 `last_checked_at`
- 天然支持"错过补跑"：停机期间到期的，重启后第一轮就会执行

**缺点**：调度粒度受 `tick` 限制，最小间隔不宜小于 `tick`。约束：`interval_sec >= 60`，`tick = 10s`，足够精确。

### 5.3 检查流程

```
Scheduler 取到期任务（手动检查走同一 Checker，但同步返回结果）
        │
        ▼
  Checker.Check(ctx, monitor)
        │
        ├─→ source.Fetch(ctx, cfg)          ── 失败 ──────────────┐
        │                                                          │
        │ 成功                                                     ▼
        ▼                                              判断失败类型：
  规范化版本号（剥离 prefix/suffix）              ┌─ 明确不存在(404等) → state = missing
        │                                          │   记录 check_history，告警走系统渠道
        ▼                                          └─ 其它错误 → 记录 check_history
  应用版本过滤（include/ignore regex）                 consecutive_failures++
        │                                              达阈值 → state = error，告警
        ├─ 被过滤 → 记 check_history（注明 skipped），状态不变
        ▼
  写入 check_history（ok=1）
  重置 consecutive_failures（missing/error 自动恢复）
        │
        ▼
  与 current_version 比较（semver）
        │
        ├─ 相同或无基线 ──→ state = up_to_date
        │                   首次则设 current_version = 查到的版本
        │                   （建立基线，不通知）
        │
        ├─ 更低（回退）───→ latest_version 跟随上游下调，记入
        │                   version_changes（不触发投递），
        │                   current_version 基线不动，状态不变
        │
        └─ 更高 ──────────→ state = has_update
                            ├─ 无未 ack 变更 → 新建 version_changes，
                            │    为绑定渠道创建 deliveries(pending)
                            ├─ 已有未 ack 变更（周期内连发新版）→
                            │    更新该变更的 new_version 与 latest_version，
                            │    距上次投递 < cooldown → 不新建投递
                            │    （或把 pending 投递改 deferred 到静默期结束）
                            └─ 更新 last_change_at
```

**系统告警**（源不可用 / missing）发送到 `settings` 表 `alert_channel_ids` 配置的渠道，与监控绑定渠道解耦；同一故障事件只告警一次（恢复后再次故障才重新告警）。

### 5.4 通知投递

**解耦设计**：检查完成后不直接同步发通知，而是写 `deliveries` 表（status=pending），由独立的投递 worker 消费。

好处：
- 推送慢或失败不阻塞检查
- 失败可重试
- 投递记录可查

投递 worker 用单独的 goroutine 池，每 2 秒轮询 `status IN ('pending','deferred') AND next_attempt_at <= now` 的记录，按指数退避重试（每次失败后按 `base_delay * 2^attempts` 更新 `next_attempt_at`，封顶 `max_delay`；deferred 记录的 `next_attempt_at` 为静默期结束时刻）。

**deferred 记录的补发前校验**：worker 取出 deferred 记录后，发送前必须重新校验，任一条件不满足则置 `status = 'canceled'` 并跳过：

1. 所属监控仍存在且 `enabled = 1`
2. 所属监控 `state = 'has_update'`（用户未 ack / 未忽略）
3. 该记录的 `change_id` 仍是该监控当前未确认的变更周期（未被新的 ack 作废）

补发时消息中的新版本号取**发送时刻**的 `monitors.latest_version`，而不是记录创建时快照的版本，避免补发已过时的版本号。

作废原因写入 `last_error` 字段（如 `canceled: acknowledged by user`），供通知历史展示。

## 6. 认证设计

### 6.1 本地账户

```go
// 注册
hash, _ := bcrypt.GenerateFromPassword([]byte(pw), 12)

// 登录
err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(pw))
```

首次启动检测 `users` 表为空，引导创建管理员。引导页面仅在无用户时可用，创建后立即失效。

### 6.2 OIDC 流程

```
浏览器                RelayWatch                    IdP
   │                      │                          │
   │ GET /auth/oidc/{name}│                          │
   ├─────────────────────▶│                          │
   │                      │ 生成 state + PKCE verifier│
   │                      │ 存 session（临时）        │
   │  302 到 IdP authorize│                          │
   │◀─────────────────────┤                          │
   │  GET authorize       │                          │
   ├─────────────────────────────────────────────────▶│
   │                      │        用户登录 + 授权     │
   │  302 /auth/oidc/{name}/callback?code=&state=     │
   │◀─────────────────────────────────────────────────┤
   │ GET callback         │                          │
   ├─────────────────────▶│                          │
   │                      │ 校验 state                │
   │                      │ POST token (code+verifier)│
   │                      ├─────────────────────────▶│
   │                      │      id_token            │
   │                      │◀─────────────────────────┤
   │                      │ 验证签名、iss、aud、exp    │
   │                      │ 提取 sub/email/name       │
   │                      │ 检查 claim_rules          │
   │                      │ 查找或创建用户             │
   │                      │ 建会话                    │
   │  302 /               │                          │
   │◀─────────────────────┤                          │
```

**使用 `coreos/go-oidc/v3`**：
- `oidc.NewProvider(ctx, issuer)` 自动发现配置
- `provider.Verifier(&oidc.Config{ClientID: ...})` 校验 ID Token
- PKCE 用 `golang.org/x/oauth2` 的 `S256ChallengeOption`

**Claim 规则**（`claim_rules` 字段，JSON）：
```json
{
  "email_domains": ["example.com"],
  "groups": {"claim": "groups", "any_of": ["relaywatch-users"]}
}
```
不满足则拒绝登录，记录审计日志。

### 6.3 会话与 CSRF

- 登录成功生成 32 字节随机 session ID，存 Cookie（`relaywatch_session`）
- CSRF token 存在 session 记录里，渲染表单时注入 hidden 字段
- 所有 `POST/PUT/DELETE` 校验 `X-CSRF-Token` 或表单字段
- 中间件顺序：`Recover → Logger → Session → CSRF → Auth → Handler`

### 6.4 限流

登录接口用令牌桶，每 IP 每分钟 5 次，失败 10 次锁定 15 分钟（记录在内存，重启清零，可接受）。

### 6.5 API Token 认证

- 创建：`POST /api/v1/tokens`，生成 32 字节随机 token（`rw_` 前缀 + base64url），响应里明文只返回这一次，库存 `sha256(token)`
- 认证：中间件检查 `Authorization: Bearer <token>`，sha256 后查 `api_tokens`，校验 `expires_at` / `revoked_at`，异步更新 `last_used_at`
- Token 继承所属用户的角色权限；审计日志记录 token 的创建与吊销

## 7. HTTP API 设计

**前后端分离**：前端是 Vue SPA，所有数据通过 `/api/v1` 的 JSON 接口获取。Go 只提供 API 和静态文件，不渲染 HTML。

唯一的例外是 OIDC 登录跳转（`/auth/oidc/{name}`），它需要服务端 302 到 IdP，无法在前端完成。

### 7.1 前端路由（vue-router，客户端）

这些路径由前端处理，Go 端统一 fallback 到 `index.html`。

| 路径 | 组件 | 说明 |
|---|---|---|
| `/` | `DashboardView` | 仪表盘 |
| `/monitors` | `MonitorListView` | 监控列表 |
| `/monitors/new` | `MonitorEditView` | 新建 |
| `/monitors/:id` | `MonitorDetailView` | 详情 |
| `/monitors/:id/edit` | `MonitorEditView` | 编辑 |
| `/channels` | `ChannelListView` | 渠道列表 |
| `/notifications` | `NotificationListView` | 通知历史 |
| `/settings` | `SettingsView` | 系统设置（仅 admin） |
| `/login` | `LoginView` | 登录（BlankLayout） |
| `/setup` | `SetupView` | 首次引导（BlankLayout） |

### 7.2 服务端路由（Go 处理）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/auth/oidc/{name}` | 发起 OIDC 登录，302 到 IdP |
| GET | `/auth/oidc/{name}/callback` | OIDC 回调，建会话后 302 回前端 |
| GET | `/api/v1/*` | 所有 JSON 接口（见 7.3） |
| GET | `/assets/*` | 静态资源，长缓存 |
| GET | `/*` | SPA fallback，返回 `index.html` |

### 7.3 JSON API

前缀 `/api/v1`，需要会话或 Bearer Token。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/monitors` | 列表，支持 `?state=&tag=&q=&sort=last_change&page=` |
| POST | `/api/v1/monitors` | 创建 |
| GET | `/api/v1/monitors/{id}` | 详情 |
| PATCH | `/api/v1/monitors/{id}` | 更新 |
| DELETE | `/api/v1/monitors/{id}` | 删除 |
| POST | `/api/v1/monitors/{id}/check` | 立即检查，**同步执行并返回结果**（有超时上限，见 8.5.3） |
| POST | `/api/v1/monitors/batch` | 批量操作：`{action: enable\|disable\|check\|ack\|delete, ids: [...]}` |
| GET | `/api/v1/monitors/{id}/history` | 检查历史（分页） |
| GET | `/api/v1/monitors/{id}/changes` | 版本变更历史（分页） |
| POST | `/api/v1/monitors/{id}/ack` | 确认更新（回到 up_to_date） |
| GET | `/api/v1/channels` | 渠道列表 |
| POST | `/api/v1/channels` | 创建 |
| PATCH | `/api/v1/channels/{id}` | 更新 |
| DELETE | `/api/v1/channels/{id}` | 删除 |
| POST | `/api/v1/channels/{id}/test` | 发送测试 |
| GET | `/api/v1/deliveries` | 通知历史，支持 `?monitor_id=&channel_id=&status=` |
| POST | `/api/v1/deliveries/{id}/resend` | 手动重发 |
| GET | `/api/v1/sources/types` | 可用的源类型及其参数定义 |
| GET | `/api/v1/notifiers/types` | 可用的渠道类型及其参数定义 |
| POST | `/api/v1/import/preview` | 解析 TOML/JSON 文本，返回转换结果与警告，不落库 |
| POST | `/api/v1/import` | 确认导入 |
| GET | `/api/v1/export` | 导出全部监控与渠道为 JSON，`?redact=1`（默认）脱敏敏感字段 |
| GET | `/api/v1/tokens` | 我的 API Token 列表 |
| POST | `/api/v1/tokens` | 创建 Token（明文仅此一次返回） |
| DELETE | `/api/v1/tokens/{id}` | 吊销 |
| GET/PUT | `/api/v1/settings` | 运行时设置（系统告警渠道等） |
| GET | `/api/v1/stats` | 仪表盘计数 |
| GET | `/api/v1/auth/providers` | 启用的 OIDC Provider（公开，供登录页渲染按钮） |
| GET | `/api/v1/auth/csrf` | 取当前会话 CSRF token |
| GET | `/api/v1/events` | SSE，推送检查/投递事件 |
| GET | `/healthz` | 健康检查（无需认证） |
| GET | `/metrics` | Prometheus 指标（可选） |

M6 双轨对比不新增端点：`monitors` 资源增加 `reference_monitor_id` / `compare_state` 等字段与 `?compare=lagging` 筛选，`stats` 增加落后计数，`deliveries` 记录带 `kind`（见 §17.6）。

### 7.4 源类型元数据

为了让前端表单动态渲染，每种源需要暴露参数定义：

```go
type FieldSpec struct {
    Key         string   `json:"key"`
    Label       string   `json:"label"`
    Type        string   `json:"type"`      // string | bool | int | select | secret
    Required    bool     `json:"required"`
    Default     any      `json:"default,omitempty"`
    Options     []string `json:"options,omitempty"`
    Placeholder string   `json:"placeholder,omitempty"`
    Help        string   `json:"help,omitempty"`
}

type SourceMeta struct {
    Type   string      `json:"type"`
    Label  string      `json:"label"`
    Fields []FieldSpec `json:"fields"`
}
```

前端拿到 `GET /api/v1/sources/types` 后，切换源类型时重新渲染表单。这样新增源类型**不需要改前端**。
## 8. 前端设计

前端采用 **Vue 3 + Vuetify 4 + Vite** 的单页应用（SPA），构建产物通过 `go:embed` 打进 Go 二进制。

选型依据见 §8.1。这一节把技术栈、布局、组件、状态、交互逐项定清楚。

### 8.1 技术选型与理由

| 层次 | 选择 | 版本 | 理由 |
|---|---|---|---|
| 框架 | Vue | 3.5.44 | 组合式 API，与现有 `dufs-material-assets` 项目同源 |
| 组件库 | Vuetify | 4.3.1 | Material Design，为管理后台设计，组件覆盖最全 |
| 路由 | vue-router | 5.4.0 | 官方路由，Vuetify 4 生态已适配 |
| 状态 | Pinia | 4.1.0 | 官方状态库，TypeScript 友好 |
| 构建 | Vite | 8.x | 秒级冷启动，HMR，产物小 |
| 语言 | TypeScript | 7.0.2 | 类型安全，Vuetify 提供完整类型定义 |
| HTTP | 原生 `fetch` + 薄封装 | — | 不需要 axios，减少依赖 |
| 图标 | `@mdi/js` + Vuetify 内置 | 7.x | Vuetify 默认图标集，与 `dufs-material-assets` 一致 |
| 样式 | Vuetify 主题系统 | — | 用 `createVuetify` 的 theme 配置，不写零散 CSS |
| 类型检查 | vue-tsc | 3.3.13 | 构建时类型检查 |

**为什么是 Vuetify 而不是别的**

这个项目的界面本质是**管理后台**：数据表格、表单、对话框、通知提示、导航。Vuetify 的组件直接对应：

| 需求 | Vuetify 组件 |
|---|---|
| 监控列表（排序/筛选/分页/批量选择） | `v-data-table-server` |
| 动态配置表单 | `v-text-field` / `v-select` / `v-switch` + `v-form` 校验 |
| 新建/编辑对话框 | `v-dialog` |
| 删除确认 | `v-dialog` + `v-btn` 危险色 |
| 操作结果提示 | `v-snackbar` |
| 侧边导航 | `v-navigation-drawer` |
| 顶栏 | `v-app-bar` |
| 状态标签 | `v-chip` |
| 空状态 | 自定义 `v-empty-state`（Vuetify 4 内置） |
| 加载骨架 | `v-skeleton-loader` |
| 实时状态更新 | `v-snackbar` + 表格行内刷新 |

这些如果用 Pico CSS 手写，等于自己实现一遍 Vuetify。

**关于「单二进制」的澄清**

之前的方案为了避免 Node 构建链而放弃组件库，这是本末倒置。实际上：

- **构建时**需要 Node + pnpm（开发者机器上）
- **运行时**零依赖，`go:embed` 把 `dist/` 打进二进制
- 用户拿到的是单个可执行文件，和纯 Go 方案完全一样

Gitea、Syncthing、Grafana 都是这个模式。构建依赖和部署形态是两件事。

**为什么不用 Next.js / Nuxt**

本项目不需要 SSR（登录后才能访问，SEO 无意义），也不需要文件路由约定。Vite + vue-router 更轻、更直接。

**为什么不用 Element Plus / Naive UI**

Element Plus 也可以，但 Vuetify 的 Material 风格更成熟，且与 `dufs-material-assets` 一致。Naive UI 体积更小但生态和组件数量不如 Vuetify。

### 8.2 项目结构

```
relaywatch/
├── cmd/relaywatch/main.go
├── internal/                      # Go 后端
│   ├── config/
│   ├── store/
│   ├── auth/
│   ├── source/
│   ├── notifier/
│   ├── check/
│   ├── scheduler/
│   ├── http/
│   └── ...
├── web/                           # Vue 前端
│   ├── index.html
│   ├── package.json
│   ├── pnpm-lock.yaml
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── env.d.ts
│   ├── src/
│   │   ├── main.ts                # 应用入口
│   │   ├── App.vue                # 根组件
│   │   ├── plugins/
│   │   │   └── vuetify.ts         # Vuetify 实例与主题
│   │   ├── router/
│   │   │   └── index.ts           # 路由表 + 守卫
│   │   ├── stores/                # Pinia
│   │   │   ├── auth.ts
│   │   │   ├── monitors.ts
│   │   │   ├── channels.ts
│   │   │   └── ui.ts              # 主题、语言、全局提示
│   │   ├── api/                   # 后端接口封装
│   │   │   ├── client.ts          # fetch 封装、CSRF、错误处理
│   │   │   ├── monitors.ts
│   │   │   ├── channels.ts
│   │   │   ├── auth.ts
│   │   │   └── types.ts           # 与后端共享的类型定义
│   │   ├── layouts/
│   │   │   ├── DefaultLayout.vue  # 侧边栏 + 顶栏
│   │   │   └── BlankLayout.vue    # 登录页用
│   │   ├── views/
│   │   │   ├── DashboardView.vue
│   │   │   ├── MonitorListView.vue
│   │   │   ├── MonitorDetailView.vue
│   │   │   ├── MonitorEditView.vue
│   │   │   ├── ChannelListView.vue
│   │   │   ├── NotificationListView.vue
│   │   │   ├── ImportView.vue          # 批量导入（粘贴/上传 + 预览）
│   │   │   ├── SettingsView.vue
│   │   │   ├── LoginView.vue
│   │   │   ├── SetupView.vue
│   │   │   └── NotFoundView.vue
│   │   ├── components/
│   │   │   ├── StatusChip.vue
│   │   │   ├── VersionDiff.vue
│   │   │   ├── SourceConfigForm.vue   # 动态表单核心
│   │   │   ├── NotifierConfigForm.vue
│   │   │   ├── SecretField.vue
│   │   │   ├── TagInput.vue
│   │   │   ├── ConfirmDialog.vue
│   │   │   ├── StatCard.vue
│   │   │   ├── EmptyState.vue
│   │   │   └── RelativeTime.vue
│   │   ├── composables/
│   │   │   ├── useApi.ts          # 请求状态封装
│   │   │   ├── useSse.ts          # SSE 连接与事件分发
│   │   │   ├── useConfirm.ts      # 确认对话框
│   │   │   ├── useToast.ts        # 全局提示
│   │   │   └── useTheme.ts        # 主题切换
│   │   ├── i18n/
│   │   │   ├── index.ts
│   │   │   ├── zh-Hans.ts
│   │   │   └── en.ts
│   │   └── styles/
│   │       └── global.css         # 极少量全局样式
│   └── dist/                      # 构建产物（git 忽略）
├── web/embed.go                   # go:embed dist
├── Makefile
└── docs/
```

**`web/embed.go`**：

```go
package web

import (
    "embed"
    "io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回去掉 dist/ 前缀的文件系统，供 http.FileServer 使用。
func Dist() fs.FS {
    sub, _ := fs.Sub(distFS, "dist")
    return sub
}
```

### 8.3 Vuetify 配置与主题

**`src/plugins/vuetify.ts`**：

```ts
import 'vuetify/styles'
import '@mdi/font/css/materialdesignicons.css'
import { createVuetify } from 'vuetify'
import { aliases, mdi } from 'vuetify/iconsets/mdi'

export const vuetify = createVuetify({
  icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
  theme: {
    defaultTheme: 'light',
    themes: {
      light: {
        dark: false,
        colors: {
          primary:    '#2563eb',
          secondary:  '#64748b',
          success:    '#16a34a',
          warning:    '#d97706',
          error:      '#dc2626',
          info:       '#0284c7',
          surface:    '#ffffff',
          background: '#f6f7f9',
        },
      },
      dark: {
        dark: true,
        colors: {
          primary:    '#3b82f6',
          secondary:  '#94a3b8',
          success:    '#22c55e',
          warning:    '#f59e0b',
          error:      '#ef4444',
          info:       '#38bdf8',
          surface:    '#1e2128',
          background: '#16181d',
        },
      },
    },
  },
  defaults: {
    VBtn:        { variant: 'flat', rounded: 'lg' },
    VTextField:  { variant: 'outlined', density: 'comfortable' },
    VSelect:     { variant: 'outlined', density: 'comfortable' },
    VTextarea:   { variant: 'outlined', density: 'comfortable' },
    VDataTable:  { density: 'comfortable' },
    VCard:       { rounded: 'lg', elevation: 0, border: true },
    VDialog:     { maxWidth: 640 },
    VSnackbar:   { location: 'top right', timeout: 4000 },
  },
})
```

**主题切换**：三种模式 `light` / `dark` / `auto`，存 `localStorage.rw-theme`。`useTheme` composable 监听 `prefers-color-scheme` 变化，auto 模式下自动跟随系统。

```ts
// src/composables/useTheme.ts
import { useTheme as useVuetifyTheme } from 'vuetify'

export function useTheme() {
  const theme = useVuetifyTheme()
  const mode = ref<'light' | 'dark' | 'auto'>(
    (localStorage.getItem('rw-theme') as any) || 'auto'
  )

  const apply = () => {
    if (mode.value === 'auto') {
      const dark = window.matchMedia('(prefers-color-scheme: dark)').matches
      theme.global.name.value = dark ? 'dark' : 'light'
    } else {
      theme.global.name.value = mode.value
    }
    localStorage.setItem('rw-theme', mode.value)
  }

  watch(mode, apply, { immediate: true })
  window.matchMedia('(prefers-color-scheme: dark)')
    .addEventListener('change', () => mode.value === 'auto' && apply())

  return { mode, apply }
}
```

**设计原则**：不写自定义 CSS 覆盖 Vuetify，用 `defaults` 统一组件默认属性，用 theme 控制颜色。只有布局骨架（如内容区最大宽度）写少量 CSS。

### 8.4 整体布局

`DefaultLayout.vue` 使用 Vuetify 的 `v-app` + `v-navigation-drawer` + `v-app-bar` + `v-main`。

```
┌──────────────────────────────────────────────────────────────────┐
│ v-app-bar (56px, flat, border-bottom)                            │
│  ☰  RelayWatch                        🔔 2      🌙  admin ▾      │
├────────────┬─────────────────────────────────────────────────────┤
│            │                                                     │
│ 导航       │  v-main                                             │
│ 240px      │  max-width 1400px, padding 24px                     │
│            │                                                     │
│ ▸ 仪表盘   │  <router-view />                                    │
│ ▸ 监控     │                                                     │
│ ▸ 通知     │                                                     │
│ ▸ 渠道     │                                                     │
│ ▸ 设置     │                                                     │
│            │                                                     │
│ ────────── │                                                     │
│ v0.1.0     │                                                     │
└────────────┴─────────────────────────────────────────────────────┘
```

**响应式**：Vuetify 的 `v-navigation-drawer` 自带响应式——桌面端 `permanent`，移动端变 `temporary`（抽屉 + 遮罩），由 `useDisplay()` 判断。

```vue
<v-navigation-drawer
  v-model="drawer"
  :permanent="mdAndUp"
  :temporary="!mdAndUp"
  width="240"
>
  <v-list nav density="comfortable">
    <v-list-item
      v-for="item in navItems"
      :key="item.to"
      :to="item.to"
      :prepend-icon="item.icon"
      :title="item.title"
    />
  </v-list>
  <template #append>
    <div class="pa-4 text-caption text-medium-emphasis">v{{ version }}</div>
  </template>
</v-navigation-drawer>
```

**内容区宽度**：`v-main` 内套一个 `div.content`，`max-width: 1400px; margin: 0 auto;`。宽屏时居中，避免表格拉得过宽。

### 8.5 页面设计

#### 8.5.1 仪表盘 `/`

```
┌────────────────────────────────────────────────────────────────┐
│ 仪表盘                                    [全部立即检查]       │
├────────────────────────────────────────────────────────────────┤
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐           │
│ │    42    │ │     3    │ │     1    │ │    38    │           │
│ │ 监控总数 │ │  有更新  │ │  异常    │ │  正常运行│           │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘           │
│                                                                │
│ 需要关注                                          [全部 →]     │
│ ┌────────────────────────────────────────────────────────────┐ │
│ │ ⚠ kun            github  0.2.18 → 0.2.20    2 小时前      │ │
│ │ ⚠ aria2-config   aur     0.4.9  → 0.5-2     1 天前        │ │
│ │ ✕ mtoc           github  检查失败：HTTP 404  3 小时前     │ │
│ └────────────────────────────────────────────────────────────┘ │
│                                                                │
│ 最近通知                                        [查看全部 →]   │
│ ┌────────────────────────────────────────────────────────────┐ │
│ │ 12:34  kun → Telegram、飞书                    已送达     │ │
│ │ 11:02  mygo-cloudmusic → Telegram              已送达     │ │
│ │ 09:15  aria2-config → Discord                  失败(重试3)│ │
│ └────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

**实现**：

- 四张 `StatCard`，用 `v-row` + `v-col`（`cols="12" sm="6" md="3"`）。数字来自 `GET /api/v1/stats`，SSE 的 `stats` 事件实时更新
- 卡片可点击，跳转到带筛选参数的列表页
- 「需要关注」用 `v-list`，只显示 `has_update` 和 `error`，最多 5 条
- 「最近通知」用 `v-list`，显示最近 5 条投递记录
- 无数据时显示 `EmptyState`

#### 8.5.2 监控列表 `/monitors`

用 `v-data-table-server`，服务端分页、排序、筛选。

```
┌────────────────────────────────────────────────────────────────────┐
│ 监控                                        [+ 新建监控]           │
├────────────────────────────────────────────────────────────────────┤
│ [状态 ▾] [源类型 ▾] [标签 ▾]  🔍 搜索名称或仓库      42 条结果     │
├──────┬──────────────┬────────┬────────┬────────┬────────┬─────────┤
│ ☐    │ 名称         │ 源     │ 当前   │ 最新   │ 状态   │ 操作    │
├──────┼──────────────┼────────┼────────┼────────┼────────┼─────────┤
│ ☐    │ mygo-cloud…  │ github │ 0.1.19 │ 0.1.19 │ ● 正常 │ ⋯       │
│ ☐    │ kun          │ github │ 0.2.18 │ 0.2.20 │ ● 更新 │ ⋯       │
│ ☐    │ aria2-config │ aur    │ 0.4.9  │ 0.5-2  │ ● 更新 │ ⋯       │
│ ☐    │ mtoc         │ github │ 1.2.0  │ —      │ ● 异常 │ ⋯       │
├──────┴──────────────┴────────┴────────┴────────┴────────┴─────────┤
│ 已选 2 项：  [启用] [停用] [立即检查] [删除]                       │
│                                              ‹ 1 2 3 ›  每页 25  │
└────────────────────────────────────────────────────────────────────┘
```

**列定义**（`v-data-table-server` 的 `headers`）：

| key | 标题 | 宽度 | 可排序 | 自定义渲染 |
|---|---|---|---|---|
| `select` | 复选框 | 48px | ✕ | `v-checkbox-btn` |
| `name` | 名称 | 弹性 | ✅ | 点击跳详情，下方显示标签 `v-chip` |
| `source_type` | 源 | 110px | ✅ | 图标 + 文本 |
| `current_version` | 当前 | 130px | ✅ | 等宽字体 |
| `latest_version` | 最新 | 130px | ✅ | `VersionDiff` 组件 |
| `state` | 状态 | 110px | ✅ | `StatusChip` |
| `last_checked_at` | 上次检查 | 130px | ✅ | `RelativeTime` |
| `actions` | 操作 | 64px | ✕ | `v-menu` 下拉 |

**筛选状态同步到 URL**：用 `vue-router` 的 query，`?state=has_update&q=kun&page=2`。刷新和分享都能保持。

```ts
const route = useRoute()
const router = useRouter()

const filters = computed(() => ({
  state: route.query.state as string | undefined,
  q: route.query.q as string | undefined,
  page: Number(route.query.page ?? 1),
}))

function updateFilter(patch: Record<string, any>) {
  router.replace({ query: { ...route.query, ...patch, page: 1 } })
}
```

**手机端**：`v-data-table-server` 在窄屏用 `v-data-table` 的 `mobile` 插槽，或切换到卡片列表。推荐后者，体验更好：

```vue
<v-data-table-server v-if="mdAndUp" ... />
<v-list v-else>
  <v-list-item v-for="m in monitors" :key="m.id" :to="`/monitors/${m.id}`">
    <template #title>{{ m.name }}</template>
    <template #subtitle>
      <StatusChip :state="m.state" size="small" />
      <VersionDiff :old="m.current_version" :new="m.latest_version" />
    </template>
  </v-list-item>
</v-list>
```

**空状态**：无监控时用 `v-empty-state`（Vuetify 4 内置组件），带图标、标题、描述和主按钮，外加「添加示例监控」快捷按钮。

#### 8.5.3 监控详情/编辑 `/monitors/{id}`

两栏布局：左侧表单（`v-col cols="12" md="8"`），右侧状态卡片（`v-col cols="12" md="4"`，`md` 以上 sticky）。

```
┌────────────────────────────────────────────────────────────────────┐
│ ← 监控   mygo-cloudmusic                        [立即检查] [保存]  │
├─────────────────────────────────────┬──────────────────────────────┤
│ 基本信息                            │ 状态卡片（sticky）           │
│  名称    [mygo-cloudmusic       ]   │  ● 正常                      │
│  标签    [arch ✕] [音乐 ✕] [+]      │  当前版本  0.1.19            │
│  备注    [                      ]   │  最新版本  0.1.19            │
│                                     │  上次检查  2 分钟前          │
│ 监控源                              │  下次检查  8 分钟后          │
│  类型    [GitHub Release      ▾]    │                              │
│  ── 动态字段区 ──                   │ ───────────────────────────  │
│  仓库    [egoist/mygo-cloudmusic]   │ 版本历史                     │
│  包含预发布  [ ]                    │  0.1.19  10-27 03:07         │
│  Token   [••••••••] (可选)          │  0.1.13  10-26 09:32         │
│                                     │  0.1.12  10-25 21:10         │
│ 版本处理                            │                              │
│  前缀    [v]     后缀 [        ]    │ ───────────────────────────  │
│                                     │ 最近检查                     │
│ 检查设置                            │  ✓ 12:34  0.1.19  1.2s       │
│  间隔    [5 分钟 ▾]                 │  ✓ 12:29  0.1.19  0.9s       │
│  通知静默期 [0 分钟]                │  ✕ 12:24  HTTP 404           │
│                                     │                              │
│ 通知渠道                            │                              │
│  ☑ Telegram                         │                              │
│  ☑ 飞书                             │                              │
│  ☐ Discord                          │                              │
└─────────────────────────────────────┴──────────────────────────────┘
```

**表单校验**：用 `v-form` 的 `rules`。校验规则和字段定义一起从后端 `FieldSpec` 生成：

```ts
function fieldRules(f: FieldSpec) {
  const rules: ((v: any) => true | string)[] = []
  if (f.required) rules.push(v => !!v || `${f.label}不能为空`)
  if (f.type === 'int') rules.push(v => /^\d+$/.test(String(v)) || '必须是数字')
  if (f.pattern) rules.push(v => new RegExp(f.pattern).test(String(v)) || f.patternHelp)
  return rules
}
```

**「立即检查」交互**（同步接口，不走 SSE）：

1. 点击后按钮 `:loading="checking"`，禁用
2. `POST /api/v1/monitors/{id}/check` **同步执行检查并直接返回结果**（`{ok, version, state, error, duration_ms}`，服务端超时上限 30s）
3. 用返回值就地刷新状态卡片，`v-snackbar` 提示成功/失败

手动检查是低频交互且秒级完成，同步返回比「202 + 等 SSE 事件」简单可靠。SSE 的 `check.done` 事件仍保留，但只用于**调度器触发的后台检查**的列表行内更新。

**未保存离开**：`onBeforeRouteLeave` + 表单 dirty 状态，弹 `v-dialog` 确认。

#### 8.5.4 通知渠道 `/channels`

`v-row` + `v-col` 卡片网格，或 `v-data-table`。渠道数量通常不多，用卡片更直观。

```
┌────────────────────────────────────────────────────────────────────┐
│ 通知渠道                                    [+ 新建渠道]           │
├────────────────────────────────────────────────────────────────────┤
│ ┌────────────────────────────────────────────────────────────────┐ │
│ │ 📱 Telegram  @my_alerts_bot                       ● 启用        │ │
│ │ 被 12 条监控使用              [发送测试] [编辑] [删除]         │ │
│ ├────────────────────────────────────────────────────────────────┤ │
│ │ 💬 飞书  运维告警群                               ● 启用        │ │
│ │ 被 5 条监控使用               [发送测试] [编辑] [删除]         │ │
│ └────────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────┘
```

「发送测试」：`POST /api/v1/channels/{id}/test`，结果用 `v-snackbar` 反馈。

删除有引用的渠道时，`ConfirmDialog` 显示「被 N 条监控使用，删除后这些监控将不再通过此渠道通知」。

#### 8.5.5 通知历史 `/notifications`

`v-data-table-server`，列：时间、监控、渠道、变更、状态、操作（重发）。

失败行可展开（`show-expand`）查看错误详情和重试次数。

#### 8.5.6 设置 `/settings`

用 `v-tabs` 分页：认证、调度、历史保留、界面。

**认证页**：
- 本地账户开关
- 修改密码表单
- OIDC Provider 列表（`v-data-table`），可增删改
- 每个 Provider 有「测试连接」按钮

#### 8.5.7 登录 `/login`

`BlankLayout`，居中卡片：

```
┌──────────────────────────────────────────────┐
│                                              │
│              RelayWatch                      │
│        版本更新监控                          │
│                                              │
│  ┌────────────────────────────────────────┐  │
│  │ 用户名 [                          ]    │  │
│  │ 密码   [                          ]    │  │
│  │                          [    登录    ]│  │
│  └────────────────────────────────────────┘  │
│                                              │
│  ───────────── 或 ─────────────              │
│                                              │
│  [ 使用 GitHub 登录 ]                        │
│  [ 使用 公司 SSO 登录 ]                      │
│                                              │
└──────────────────────────────────────────────┘
```

OIDC 按钮来自 `GET /api/v1/auth/providers`（公开接口），点击跳转 `/auth/oidc/{name}`。

#### 8.5.8 首次引导 `/setup`

仅在用户表为空时可达，路由守卫检查 `GET /api/v1/auth/status`。表单：用户名、密码、确认密码。提交后自动登录跳转仪表盘。

### 8.6 核心组件设计

#### 8.6.1 动态配置表单 `SourceConfigForm.vue`

这是前端最核心的组件。后端 `GET /api/v1/sources/types` 返回字段定义：

```json
[
  {
    "type": "github_release",
    "label": "GitHub Release",
    "icon": "mdi-github",
    "fields": [
      {"key": "repo", "label": "仓库", "type": "string",
       "required": true, "placeholder": "owner/name",
       "help": "如 egoist/mygo-cloudmusic"},
      {"key": "include_prerelease", "label": "包含预发布",
       "type": "bool", "default": false},
      {"key": "token", "label": "访问令牌", "type": "secret",
       "required": false, "help": "公开仓库可留空，私有仓库需要"}
    ]
  }
]
```

组件实现：

```vue
<script setup lang="ts">
import type { SourceMeta, FieldSpec } from '@/api/types'

const props = defineProps<{
  modelValue: Record<string, any>
  sourceType: string
}>()
const emit = defineEmits<{
  'update:modelValue': [Record<string, any>]
  'update:sourceType': [string]
}>()

const types = ref<SourceMeta[]>([])
const current = computed(() => types.value.find(t => t.type === props.sourceType))
const fields = computed<FieldSpec[]>(() => current.value?.fields ?? [])

onMounted(async () => {
  types.value = await api.getSourceTypes()
})

// 切换类型时保留同名字段值
watch(() => props.sourceType, (next, prev) => {
  if (!prev) return
  const nextFields = types.value.find(t => t.type === next)?.fields ?? []
  const keep: Record<string, any> = {}
  for (const f of nextFields) {
    if (f.key in props.modelValue) keep[f.key] = props.modelValue[f.key]
    else if (f.default !== undefined) keep[f.key] = f.default
  }
  emit('update:modelValue', keep)
})

function setField(key: string, value: any) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}
</script>

<template>
  <v-select
    :model-value="sourceType"
    :items="types"
    item-title="label"
    item-value="type"
    label="监控源类型"
    @update:model-value="$emit('update:sourceType', $event)"
  />

  <template v-for="f in fields" :key="f.key">
    <v-switch
      v-if="f.type === 'bool'"
      :model-value="!!modelValue[f.key]"
      :label="f.label"
      :hint="f.help"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <v-select
      v-else-if="f.type === 'select'"
      :model-value="modelValue[f.key]"
      :items="f.options"
      :label="f.label"
      :hint="f.help"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />

    <SecretField
      v-else-if="f.type === 'secret'"
      :model-value="modelValue[f.key]"
      :label="f.label"
      :hint="f.help"
      :placeholder="f.placeholder"
      @update:model-value="setField(f.key, $event)"
    />

    <v-text-field
      v-else
      :model-value="modelValue[f.key]"
      :label="f.label"
      :hint="f.help"
      :placeholder="f.placeholder"
      :rules="fieldRules(f)"
      :type="f.type === 'int' ? 'number' : 'text'"
      persistent-hint
      @update:model-value="setField(f.key, $event)"
    />
  </template>
</template>
```

**新增源类型不需要改前端**，只要后端在 `SourceMeta` 里描述清楚字段。

**`SecretField.vue`**：密码框 + 显示/隐藏切换。编辑已有记录时后端返回掩码值 `"••••••••"`，提交时如果值未变则不覆盖。

```vue
<script setup lang="ts">
const show = ref(false)
const isMasked = computed(() =>
  props.modelValue === '••••••••' || props.modelValue === ''
)
</script>

<template>
  <v-text-field
    :model-value="modelValue"
    :type="show ? 'text' : 'password'"
    :append-inner-icon="show ? 'mdi-eye-off' : 'mdi-eye'"
    @click:append-inner="show = !show"
    @update:model-value="$emit('update:modelValue', $event)"
    @focus="isMasked && $emit('update:modelValue', '')"
  />
</template>
```

#### 8.6.2 状态标签 `StatusChip.vue`

```vue
<script setup lang="ts">
const props = defineProps<{ state: MonitorState; size?: 'small' | 'default' }>()

const map = {
  up_to_date: { color: 'success', text: '正常',   icon: 'mdi-check-circle' },
  has_update: { color: 'warning', text: '有更新', icon: 'mdi-arrow-up-circle' },
  error:      { color: 'error',   text: '异常',   icon: 'mdi-alert-circle' },
  missing:    { color: 'error',   text: '已消失', icon: 'mdi-cloud-off-outline' },
  unknown:    { color: 'default', text: '未知',   icon: 'mdi-help-circle' },
} as const

const meta = computed(() => map[props.state] ?? map.unknown)
</script>

<template>
  <v-chip :color="meta.color" :size="size" :prepend-icon="meta.icon" variant="tonal">
    {{ meta.text }}
  </v-chip>
</template>
```

#### 8.6.3 版本差异 `VersionDiff.vue`

```vue
<template>
  <span class="version-diff">
    <span v-if="old" class="old">{{ old }}</span>
    <v-icon v-if="old && new" size="x-small">mdi-arrow-right</v-icon>
    <span :class="{ new: old && new }">{{ new ?? '—' }}</span>
  </span>
</template>

<style scoped>
.version-diff { font-family: var(--rw-font-mono); font-size: .875rem; }
.old { text-decoration: line-through; opacity: .5; }
.new { color: rgb(var(--v-theme-warning)); font-weight: 600; }
</style>
```

#### 8.6.4 相对时间 `RelativeTime.vue`

```vue
<script setup lang="ts">
const props = defineProps<{ time: string | null }>()
const display = ref('')
const absolute = computed(() =>
  props.time ? new Date(props.time).toLocaleString() : ''
)

function update() {
  if (!props.time) { display.value = '—'; return }
  const diff = Date.now() - new Date(props.time).getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) display.value = '刚刚'
  else if (min < 60) display.value = `${min} 分钟前`
  else if (min < 1440) display.value = `${Math.floor(min / 60)} 小时前`
  else display.value = `${Math.floor(min / 1440)} 天前`
}

let timer: number
onMounted(() => { update(); timer = window.setInterval(update, 30000) })
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <v-tooltip :text="absolute">
    <template #activator="{ props }">
      <span v-bind="props">{{ display }}</span>
    </template>
  </v-tooltip>
</template>
```

#### 8.6.5 确认对话框 `ConfirmDialog.vue` + `useConfirm`

```ts
// composables/useConfirm.ts
const state = reactive({
  open: false,
  title: '',
  text: '',
  confirmText: '确认',
  color: 'error',
  resolve: null as ((v: boolean) => void) | null,
})

export function useConfirm() {
  function confirm(opts: { title: string; text: string; confirmText?: string; color?: string }) {
    Object.assign(state, opts, { open: true })
    return new Promise<boolean>(resolve => { state.resolve = resolve })
  }
  function answer(v: boolean) {
    state.open = false
    state.resolve?.(v)
    state.resolve = null
  }
  return { state, confirm, answer }
}
```

使用：

```ts
const ok = await confirm({
  title: '删除监控',
  text: '将删除「mygo-cloudmusic」，此操作不可恢复。',
  confirmText: '删除',
})
if (ok) await api.deleteMonitor(id)
```

### 8.7 状态管理

用 Pinia，按领域分 store。

| Store | 职责 |
|---|---|
| `auth` | 当前用户、登录状态、登出 |
| `monitors` | 列表、筛选条件、分页状态 |
| `channels` | 渠道列表 |
| `ui` | 主题模式、语言、全局 snackbar 队列 |

**原则**：store 只存「跨组件共享的状态」，页面内局部状态用 `ref` 即可。不要把所有数据都塞进 store。

**数据获取**：不引入 TanStack Query 之类的缓存库。页面加载时直接调 API，用 `useApi` 封装 loading/error：

```ts
// composables/useApi.ts
export function useApi<T>(fn: () => Promise<T>, immediate = true) {
  const data = ref<T>()
  const loading = ref(false)
  const error = ref<string>()

  async function run() {
    loading.value = true
    error.value = undefined
    try {
      data.value = await fn()
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  if (immediate) run()
  return { data, loading, error, refresh: run }
}
```

### 8.8 SSE 实时更新

```ts
// composables/useSse.ts
type Handler = (data: any) => void

const handlers = new Map<string, Set<Handler>>()
let source: EventSource | null = null
let retry = 0

function connect() {
  source = new EventSource('/api/v1/events')

  source.onopen = () => { retry = 0 }

  source.onerror = () => {
    source?.close()
    const delay = Math.min(30000, 1000 * 2 ** retry++)
    setTimeout(connect, delay)
  }

  for (const [event, set] of handlers) {
    source.addEventListener(event, (e: MessageEvent) => {
      const data = JSON.parse(e.data)
      set.forEach(h => h(data))
    })
  }
}

export function useSse(event: string, handler: Handler) {
  onMounted(() => {
    if (!handlers.has(event)) handlers.set(event, new Set())
    handlers.get(event)!.add(handler)
    if (!source) connect()
  })
  onUnmounted(() => {
    handlers.get(event)?.delete(handler)
  })
}
```

**事件类型**：

| 事件 | 数据 | 用途 |
|---|---|---|
| `check.started` | `{monitor_id}` | 行内 spinner |
| `check.done` | `{monitor_id, ok, version, state, error, duration_ms}` | 更新行内数据 |
| `delivery.done` | `{delivery_id, monitor_id, channel_id, ok}` | 更新通知历史 |
| `stats` | `{total, has_update, error, ok}` | 更新仪表盘卡片 |

**降级**：SSE 不可用时（代理不支持），`onerror` 连续失败 3 次后切换为 30 秒轮询。

### 8.9 路由与守卫

```ts
// router/index.ts
const routes = [
  { path: '/login', component: LoginView, meta: { layout: 'blank', public: true } },
  { path: '/setup', component: SetupView, meta: { layout: 'blank', public: true } },
  {
    path: '/',
    component: DefaultLayout,
    children: [
      { path: '', name: 'dashboard', component: DashboardView },
      { path: 'monitors', name: 'monitors', component: MonitorListView },
      { path: 'monitors/new', name: 'monitor-new', component: MonitorEditView },
      { path: 'monitors/:id', name: 'monitor-detail', component: MonitorDetailView },
      { path: 'monitors/:id/edit', name: 'monitor-edit', component: MonitorEditView },
      { path: 'channels', name: 'channels', component: ChannelListView },
      { path: 'notifications', name: 'notifications', component: NotificationListView },
      { path: 'import', name: 'import', component: ImportView },
      { path: 'settings', name: 'settings', component: SettingsView, meta: { admin: true } },
    ],
  },
  { path: '/:pathMatch(.*)*', component: NotFoundView },
]

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.loaded) await auth.load()
  if (to.meta.public) return true
  if (!auth.user) return { path: '/login', query: { redirect: to.fullPath } }
  if (to.meta.admin && auth.user.role !== 'admin') return { path: '/' }
  return true
})
```

### 8.10 API 客户端

```ts
// api/client.ts
let csrfToken = ''

export async function apiFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (init.method && init.method !== 'GET' && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  const res = await fetch(`/api/v1${path}`, {
    ...init,
    headers,
    credentials: 'same-origin',
  })

  if (res.status === 401) {
    location.href = '/login'
    throw new Error('未登录')
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new ApiError(body.message ?? `HTTP ${res.status}`, body.fields)
  }

  return res.status === 204 ? (undefined as T) : res.json()
}

export function setCsrf(token: string) { csrfToken = token }
```

CSRF token 在页面加载时从 `GET /api/v1/auth/csrf` 获取，或由登录响应返回。

### 8.11 国际化

Vue 生态用 `vue-i18n`，文案文件 `zh-Hans.ts` / `en.ts`。

```ts
// i18n/zh-Hans.ts
export default {
  common: { save: '保存', cancel: '取消', delete: '删除', confirm: '确认' },
  monitors: {
    title: '监控',
    new: '新建监控',
    empty: '还没有监控',
    emptyHint: '添加第一个监控，自动跟踪上游版本更新',
  },
  status: {
    up_to_date: '正常',
    has_update: '有更新',
    error: '异常',
    missing: '已消失',
    unknown: '未知',
  },
}
```

语言判定：`localStorage.rw-lang` → `navigator.language` → `zh-Hans`。切换时存 localStorage 并更新 `<html lang>`。

### 8.12 构建与嵌入

**`vite.config.ts`**：

```ts
export default defineConfig({
  plugins: [vue(), vuetify({ autoImport: true })],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vuetify: ['vuetify'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
    },
  },
})
```

**`Makefile`**：

```makefile
.PHONY: build web backend dev clean

build: web backend

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

backend:
	go build -trimpath -ldflags="-s -w" -o relaywatch ./cmd/relaywatch

dev:
	@echo "后端: go run ./cmd/relaywatch"
	@echo "前端: cd web && pnpm dev  (访问 http://localhost:5173)"

clean:
	rm -rf relaywatch web/dist
```

**开发流程**：
- 终端 1：`go run ./cmd/relaywatch`（后端，:8080）
- 终端 2：`cd web && pnpm dev`（Vite，:5173，API 代理到 8080）
- 浏览器访问 `http://localhost:5173`，享受 HMR

**生产构建**：`make build` 先构建前端到 `web/dist`，再 `go build` 时 `embed` 进去。

**`internal/http/server.go` 提供静态资源**：

```go
func (s *Server) staticHandler() http.Handler {
    dist := web.Dist()
    fileServer := http.FileServer(http.FS(dist))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // API 和 auth 走别的路由，这里只处理静态资源和 SPA fallback
        if _, err := fs.Stat(dist, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
            // 不存在的路径交给 index.html（SPA 路由）
            r.URL.Path = "/"
        }
        // 带 hash 的静态资源长缓存
        if strings.HasPrefix(r.URL.Path, "/assets/") {
            w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

### 8.13 性能预算

| 指标 | 预算 | 说明 |
|---|---|---|
| 首屏 JS（gzip） | < 200 KB | Vue + Vuetify + 业务代码 |
| 首屏 CSS（gzip） | < 40 KB | Vuetify 样式 + 主题 |
| 首屏渲染 | < 1 s | 局域网 |
| 路由切换 | < 200 ms | 路由懒加载 |
| 外部请求 | 0 | 所有资源本地，不引 CDN、不引 Google Fonts |

**优化措施**：
- 路由级懒加载：`component: () => import('./views/...')`
- Vuetify 按需引入（`vite-plugin-vuetify` 的 `autoImport`）
- `manualChunks` 把 Vuetify 单独分块，业务代码改动不影响其缓存
- 字体用系统字体栈，不加载 Web Font

### 8.14 可访问性

Vuetify 组件自带大部分可访问性支持（键盘导航、ARIA、焦点管理）。补充：

- 所有 `v-icon` 按钮必须有 `aria-label`
- `v-dialog` 自动管理焦点陷阱
- 表格的排序、分页有 ARIA 标注
- 状态不只靠颜色区分，`StatusChip` 带图标和文字
- 尊重 `prefers-reduced-motion`（Vuetify 有 `$vuetify.noTransitions` 配置）

## 9. 安全设计

| 威胁 | 对策 |
|---|---|
| SQL 注入 | 全部使用参数化查询，禁止字符串拼接 |
| XSS | Vue 模板默认转义；禁止用 `v-html` 渲染用户数据（确需时先白名单净化）；JSON API 设 `Content-Type: application/json` |
| CSRF | 双提交 Cookie + session 内 token 校验 |
| 点击劫持 | 响应头 `X-Frame-Options: DENY` |
| 会话劫持 | Cookie `HttpOnly` + `Secure` + `SameSite=Lax`；可配置会话过期 |
| 暴力破解 | 登录限流 + 失败锁定 |
| 敏感信息泄露 | 加密存储；UI 掩码；日志脱敏（不打印 token/密码） |
| SSRF | `http_json`/`http_regex` 源限制：禁止访问内网地址（可配置白名单） |
| 上游 API 滥用 | 并发限制 + 请求超时 + 尊重 429/Retry-After |
| 依赖漏洞 | 定期 `govulncheck` |

**安全响应头**（中间件统一设置）：

```
Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Strict-Transport-Security: max-age=31536000  (仅 HTTPS)
```

说明：`style-src 'unsafe-inline'` 是 Vuetify 运行时注入样式的硬性要求；`img-src` 不放行任意外部 https 图片（上游 favicon 等需求若出现再单独评估），保持最小面。`connect-src` 缺省继承 `default-src 'self'`，SSE 与 API 同源不受影响。

## 10. 配置设计

`config.yaml`（支持环境变量覆盖，前缀 `RELAYWATCH_`）：

```yaml
server:
  addr: ":8080"
  base_url: "https://relaywatch.example.com"   # OIDC 回调需要
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

database:
  path: "./data/relaywatch.db"

security:
  secret_key: ""              # 32 字节 base64，用于加密敏感字段
  old_secret_key: ""          # 轮换期过渡：解密时新密钥失败后回退尝试；relaywatch rekey 完成后删除
  session_ttl: "168h"
  login_rate_limit: 5         # 每分钟每 IP
  login_lockout: 15m

scheduler:
  tick: "10s"
  max_concurrency: 10
  min_interval: "60s"
  jitter: 0.1                 # 到期时间 ±10% 随机抖动
  host_rate_limit:
    default_per_minute: 30    # 每个主机名每分钟请求上限
    overrides:                # 按 host 覆盖
      api.github.com: 25      # 留余量给 429/Retry-After

history:
  check_retention: "720h"     # 30 天
  delivery_retention: "720h"

logging:
  level: "info"               # debug | info | warn | error
  format: "text"              # text | json

sources:
  http_timeout: "20s"
  user_agent: "RelayWatch/0.1"
  allow_private_networks: false

notifications:
  retry:
    max_attempts: 5
    base_delay: "2s"
    max_delay: "10m"
  default_template: ""        # 空则用内置
```

`secret_key` 未设置时，启动拒绝服务并提示生成方法：

```bash
openssl rand -base64 32
```

## 11. 关键流程时序

### 11.1 一次定时检查

```
Scheduler tick
   │
   ├─ SELECT * FROM monitors WHERE enabled=1
   │
   ├─ 过滤到期（last_checked_at + interval <= now）
   │
   ├─ 提交到任务池（受 max_concurrency 限制）
   │
   ▼
Checker.Check
   │
   ├─ 解密 source_config
   ├─ source.Fetch（带 timeout）
   │
   ├─ 成功：
   │    ├─ 规范化版本（剥离 prefix/suffix）
   │    ├─ INSERT check_history(ok=1)
   │    ├─ 重置 consecutive_failures
   │    ├─ 比较版本
   │    │    ├─ 无基线 → 设 current_version，state=up_to_date
   │    │    ├─ 相同   → state=up_to_date
   │    │    └─ 更高   → state=has_update
   │    │                INSERT version_changes
   │    │                INSERT deliveries(pending) × N 渠道
   │    └─ UPDATE monitors
   │
   └─ 失败：
        ├─ INSERT check_history(ok=0, error=...)
        ├─ consecutive_failures++
        └─ 达到阈值 → state=error，可选告警
```

### 11.2 通知投递

```
Delivery worker（每 2s）
   │
   ├─ SELECT * FROM deliveries
   │    WHERE status IN ('pending','deferred') AND next_attempt_at <= now
   │
   ├─ 对每条：
   │    ├─ 取 channel 配置（解密）
   │    ├─ 渲染模板
   │    ├─ notifier.Send
   │    │    ├─ 成功 → status=sent, sent_at=now
   │    │    └─ 失败 → attempts++, 计算退避
   │    │              若 attempts >= max → status=failed
   │    └─ UPDATE deliveries
```

## 12. 测试策略

| 层次 | 方式 |
|---|---|
| 版本比较 | 表驱动测试，覆盖 `v1.2.3`、`1.2.3-rc1`、`2024.01.15`、非 semver、include/ignore 过滤等 |
| 源实现 | `httptest.Server` 模拟各上游 API，测正常/404(→missing)/429+Retry-After/超时/畸形响应 |
| 渠道实现 | `httptest.Server` 接收请求，断言 body 与 headers |
| 状态机 | 纯函数测试，覆盖 unknown→up_to_date→has_update→ack 全路径 |
| 调度器 | 注入假时钟，测到期判断、抖动范围、防重入、per-host 限速 |
| 认证 | 本地登录、会话过期、CSRF 校验、OIDC 用假 IdP、API Token 创建/使用/吊销 |
| 导入器 | nvchecker keyfile 各源类型映射、未知字段告警、非法条目报错不中断 |
| 对比与对齐（M6） | 表驱动：对齐规则（pkgrel/epoch/双侧 aur/无后缀）、关系函数、事件迁移矩阵；评估器用临时 SQLite 验证入队去重、故障跳过、参照删除清理 |
| HTTP 层 | `httptest` 端到端，覆盖鉴权矩阵（admin/viewer/未登录） |
| 集成 | 临时 SQLite 文件，跑完整检查+投递流程 |

**不使用 mock 框架**，手写简单 stub，保持依赖精简。

## 13. 部署

### 13.1 构建

```bash
make build          # pnpm build + go build，产出 ./relaywatch
./relaywatch -config config.yaml
```

**子命令**：
- `relaywatch serve`（默认）启动服务
- `relaywatch rekey`：用 `security.secret_key`（新）重加密数据库中所有敏感字段——前提是把旧密钥配在 `security.old_secret_key`；完成后提示从配置中移除旧密钥

构建环境需要 Go 1.27+、Node 22+、pnpm。运行环境零依赖。

交叉编译：

```bash
make web            # 前端只构建一次
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o relaywatch-arm64 ./cmd/relaywatch
```

前端产物与平台无关，`embed` 后在所有目标平台通用。

### 13.2 Docker

多阶段构建，前端和后端分别在各自镜像里完成：

```dockerfile
# 前端构建
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 后端构建
FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relaywatch ./cmd/relaywatch

# 运行
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /relaywatch /usr/local/bin/relaywatch
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["relaywatch", "-config", "/data/config.yaml"]
```

镜像预计 40–60 MB（Alpine ~8 MB + ca-certificates/tzdata + 嵌入了前端产物的 Go 二进制），以首次构建实测为准。

### 13.3 systemd

```ini
[Unit]
Description=RelayWatch
After=network-online.target

[Service]
Type=simple
User=relaywatch
WorkingDirectory=/var/lib/relaywatch
ExecStart=/usr/local/bin/relaywatch -config /etc/relaywatch/config.yaml
Restart=on-failure
RestartSec=5s
# 安全加固
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/relaywatch
```

### 13.4 反向代理

SPA 需要 fallback 到 `index.html`。Go 服务已内置该逻辑，但若前面有 Nginx：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}

# SSE 需要关闭缓冲
location /api/v1/events {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_cache off;
    proxy_set_header Connection '';
    proxy_http_version 1.1;
    chunked_transfer_encoding off;
}
```

`proxy_buffering off` 是 SSE 能实时推送的关键，漏了会导致事件延迟到缓冲区满才送达。

## 14. 扩展性考虑

### 14.1 新增源类型

1. 在 `internal/source/` 新建文件，实现 `Source` 接口
2. 提供 `Meta()` 返回字段定义
3. 在 `cmd/main.go` 注册

前端自动适配，无需改代码。

### 14.2 新增通知渠道

同上，实现 `Notifier` 接口并注册。

### 14.3 未来可扩展方向

- **上游 vs 本地双轨对比**：已规划为 §17（M6，需求 §3.8）。原提的 `group_key` 对称分组方案已改为跟随监控上的有向引用 `reference_monitor_id`，理由见 §17.1
- 多用户数据隔离（给 `monitors` 加 `owner_id`）
- PostgreSQL 支持（实现 `store.Store` 的另一套）
- 自动更新 PKGBUILD（AUR 维护者场景）
- Webhook 入站（外部触发检查）
- 版本发布日历视图

## 15. 依赖清单

### Go 后端（已实测锁定，见 go.mod）

| 模块 | 用途 | 版本（2026-10 实测） |
|---|---|---|
| `github.com/go-chi/chi/v5` | 路由 | v5.5.0 |
| `modernc.org/sqlite` | 纯 Go SQLite | v1.61.0 |
| `golang.org/x/crypto` | bcrypt | v0.58.0 |
| `github.com/coreos/go-oidc/v3` | OIDC | v3.21.0 |
| `golang.org/x/oauth2` | OAuth2 流程 | v0.37.0 |
| `github.com/Masterminds/semver/v3` | 版本比较 | v3.5.0 |
| `gopkg.in/yaml.v3` | 配置解析 | v3.0.1 |
| `github.com/pelletier/go-toml/v2` | nvchecker keyfile 导入 | v2.2.4 |

注：`tidwall/gjson` 暂未引入——`http_json` 源类型尚未实现，届时引入。

### 前端（已实测锁定，见 web/package.json）

| 包 | 用途 | 版本（2026-10 实测） |
|---|---|---|
| `vue` | 框架 | 3.5.44 |
| `vuetify` | 组件库 | 4.3.1 |
| `vue-router` | 路由 | 5.4.0（peer 接受 pinia ^3.0.4 \|\| ^4.0.2，与 pinia 4.1.0 无冲突） |
| `pinia` | 状态管理 | 4.1.0 |
| `vue-i18n` | 国际化 | 11.4.16 |
| `@mdi/font` | Material 图标 | 7.4.47 |
| `vite` | 构建 | 8.3.5 |
| `@vitejs/plugin-vue` | Vue 插件 | 6.0.10 |
| `vite-plugin-vuetify` | Vuetify 按需引入 | 2.1.3 |
| `typescript` | 语言 | **5.9.3**（TS7 与 vue-tsc 3.3.13 不兼容） |
| `vue-tsc` | 类型检查 | 3.3.13 |
| `vitest` | 单元测试 | 3.2.7 |

### 刻意不引入

- **ORM**（`gorm`、`ent`）—— 表结构简单，`database/sql` 足够，且能精确控制 SQL
- **Go Web 框架**（`gin`、`echo`）—— `chi` + 标准库更轻
- **axios** —— 原生 `fetch` 加薄封装即可，少一个依赖
- **TanStack Query / SWR** —— 页面级数据获取，不需要跨页面缓存
- **CSS 框架**（Tailwind、UnoCSS）—— Vuetify 的 theme 系统已覆盖，避免两套样式体系冲突
- **Next.js / Nuxt** —— 不需要 SSR，Vite + vue-router 更直接
- **mock 框架** —— 手写 stub，保持依赖精简

## 16. 风险与权衡

| 风险 | 影响 | 缓解 |
|---|---|---|
| 纯 Go SQLite 性能低于 cgo 版 | 高并发写入可能慢 | 本项目写入频率低（每条监控几分钟一次），可忽略 |
| 构建需要 Node 环境 | 贡献者门槛略高 | 提供 Docker 构建；`make build` 一条命令；CI 里预装；仅改 Go 代码时用 `web/dist` 占位目录或构建标签跳过前端构建 |
| Vuetify 4 较新（2026 年正式版） | 生态文档可能滞后于 v3 | 锁定版本；关键组件先用 demo 验证；v3→v4 差异集中在主题 API |
| 前端包体积 | 首屏 200 KB gzip | 路由懒加载 + Vuetify 按需引入 + 手动分块 |
| 上游 API 限流 | 检查失败 | per-host 令牌桶限速、尊重 Retry-After、支持配置 token 提高配额 |
| SSE 被反代缓冲 | 实时更新失效 | 文档给出 Nginx `proxy_buffering off`；前端 3 次失败后降级轮询；手动检查不依赖 SSE |
| 加密密钥丢失 | 敏感配置无法解密 | 文档强调备份 `secret_key`；轮换用 `old_secret_key` 过渡 + `relaywatch rekey` 重加密 |
| 单实例架构 | 无法水平扩展 | v1 明确非目标；1000 条监控单实例足够 |
| 对比的 pkgrel 启发式对齐在极端上游版本（自身以 `-数字` 结尾）下可能误判 | 误报「一致」而漏提醒 | 对齐只对 `aur` 源类型生效、参照侧不剥离；影响限于对比提醒，不改变两侧监控自身的状态机与通知 |
| 上游发版 + 对比落后产生双通知（两监控绑同一渠道时） | 通知噪音 | 落后通知按事件去重（进入落后、差距扩大各一次）；跟随监控 `notify_cooldown_sec` 可节流；文档说明 |

## 17. 双轨对比设计（M6 规划）

对应需求 §3.8。把同一软件的上游发布源与本地/发行版源关联，检测「我的包落后了吗」，在落后出现、差距扩大、追平时提醒。本节为 M6 规划，尚未实现；与 M1–M5 已实现代码的衔接处逐一标注。

### 17.1 数据模型

§14.3 原提的 `group_key` 对称分组方案不采用：对比关系本质有向（谁落后于谁），对称分组需要额外 `role` 列或按源类型猜测基准，而「AUR 包 vs Arch 官方仓库」这类对比两边都是发行版源，启发式就会失效。改为**跟随监控上的显式有向引用**：

- 一条监控最多一个参照；参照自身不得有参照（depth-1，校验在 HTTP 层，环在结构上不可能出现）
- 一个参照可被多条监控引用（`foo`、`foo-bin`、`foo-git` 同源）
- 删除参照：`ON DELETE SET NULL` 自动解除关联；同时清空引用方的 `compare_*` 并作废其待投递的对比通知

迁移 0003（全部默认值安全，存量行零影响）：

```sql
ALTER TABLE monitors ADD COLUMN reference_monitor_id INTEGER REFERENCES monitors(id) ON DELETE SET NULL;
ALTER TABLE monitors ADD COLUMN compare_state TEXT NOT NULL DEFAULT '';        -- '' | in_sync | lagging | ahead
ALTER TABLE monitors ADD COLUMN compare_ref_version TEXT NOT NULL DEFAULT '';  -- 参照方 latest 快照（列表展示）
ALTER TABLE monitors ADD COLUMN compare_since DATETIME;                        -- 进入当前对比状态的时间
ALTER TABLE monitors ADD COLUMN compare_notified_ref TEXT NOT NULL DEFAULT ''; -- 上次落后通知时的参照版本（差距扩大去重）
CREATE INDEX idx_monitors_reference ON monitors(reference_monitor_id);

ALTER TABLE deliveries ADD COLUMN kind TEXT NOT NULL DEFAULT 'update';         -- update | compare（默认值保证存量行为不变）
ALTER TABLE deliveries ADD COLUMN context TEXT;                                -- JSON 事件快照，compare 用
```

对比状态存在跟随监控侧；`compare_*` 是对比路径的派生态，与监控自身状态机（`state`）互不影响。

### 17.2 版本对齐（关键实现点）

实测：`source.CompareVersions` 对三段式版本走 semver 路径，而 `0.1.19-1` 是合法 semver（`-1` 被解析为预发布），于是：

| 比较 | 现有结果 | 期望 |
|---|---|---|
| `0.1.19-1` vs `0.1.19` | -1（落后） | 0（一致） |
| `0.2.20-2` vs `0.2.20` | -1（落后） | 0（一致，仅 pkgrel rebuild） |
| `1.2.3-2` vs `1.2.3-1` | 1（领先） | 0（同上游版本不同 pkgrel） |

不处理则 AUR 对比场景会持续误报。因此对比前引入对齐层（`internal/compare` 内的规则表，按源类型）：

| 源类型 | 对齐规则 |
|---|---|
| `aur` | 剥离前导 epoch（`^\d+:`）与尾部 pkgrel（`-\d+$`） |
| 其他 | 原样 |

两侧各自应用自己的规则（AUR vs AUR 也正确；参照侧为 AUR 时同样剥离）。刻意**不做**通用的「剥离一切 `-数字` 结尾」：上游版本合法地以 `-1` 结尾时（少见），通用剥离会把真实差异误判为一致——误报落后只是吵，误报一致会漏掉更新。

### 17.3 对比关系与通知事件

关系由纯函数得出（便于表驱动测试）：

```go
cmp := source.CompareVersions(align(follower.LatestVersion, follower.SourceType),
                              align(ref.LatestVersion,      ref.SourceType))
// cmp < 0 → lagging ；== 0 → in_sync ；> 0 → ahead
```

事件与迁移（沿用既有「系统告警同一故障事件只提醒一次」的哲学：低频、可解释）：

| 事件 | 条件 | 去重依据 |
|---|---|---|
| `entered_lagging` | 已知状态 → `lagging`（含刚配置参照即发现落后） | `compare_state` 变化本身 |
| `gap_grew` | 保持 `lagging` 且参照版本 ≠ `compare_notified_ref` | `compare_notified_ref` |
| `caught_up` | `lagging` → `in_sync` | `compare_state` 变化本身 |

- 任一侧 `error`/`missing`/无版本（或参照已解除）→ **跳过评估、保持原状态**，避免「落后 → 上游故障 → 恢复」误报一次「追平」
- `ahead`、未配置 → `in_sync` 等迁移不通知
- 落后类事件发出后，`compare_notified_ref` 更新为当时的参照版本

### 17.4 评估器与触发点

新包 `internal/compare`（`Evaluator{Store, Enqueue}`）；`Checker` 增加第三个钩子（现有两个为 `OnEvent`/`OnChange`）：

```go
// Checker 每次检查完成后调用（覆盖调度器、批量、手动全部入口）
OnChecked func(ctx context.Context, m *store.Monitor, out Outcome)
```

```text
Checker.Check 成功落库
   └→ OnChecked(m)
        ├─ m 自身有参照        → 评估 pair(m 与其参照)
        └─ m 是被引用的参照     → 对每个 follower 评估 pair(follower, m)

评估 pair：重新加载两侧最新数据
   ├─ 任一侧不可用 → 跳过（保持原状态）
   ├─ 对齐 + 比较 → newState
   ├─ newState != follower.compare_state（或落后且参照版本变化）→ 事件入队
   │     （notify.Dispatcher.EnqueueCompare，复用静默期/退避/历史）
   └─ 更新 compare_state / compare_ref_version / compare_since / compare_notified_ref
         （专用部分更新 UpdateMonitorCompare，防止整行覆盖并发写，
           与 UpdateMonitorCheckResult 同理）
```

并发：两侧检查可能同时完成、同时评估同一 pair——按跟随监控 ID 加内存互斥（同 `scheduler.running` / `HostLimiter` 模式），配合「状态已更新则不重复入队」做到确定性防重。

补充触发点：

- 保存监控时参照关系变化 → 立即评估一次（配置后即刻看到对比结果，不等下次检查）
- 删除参照监控 → 清空 followers 的 `compare_*`、作废其待投递对比通知（`CancelOpenDeliveries` 扩展 kind 维度或新增对应方法）

### 17.5 投递与消息

复用 deliveries：对比通知插入 `kind='compare'` 记录，由现有 worker 消费（退避重试、静默期 deferred、历史、手动重发全部沿用）。

- **渠道**：跟随监控绑定的渠道；未绑定则不发送（UI 给提示，不自动回落系统告警渠道，避免稀释「系统告警」语义）
- **消息**：`TemplateData` 扩展 `Compare{RefMonitorName, RefVersion, FollowerVersion, State}`，默认模板与自定义模板同机制；标题形如「RelayWatch：AUR foo 落后于上游」
- **worker 分支**：`kind='compare'` 的补发前校验 = 「事件是否仍成立」——`entered_lagging`/`gap_grew` 要求当前仍 `lagging`、`caught_up` 要求当前 `in_sync`，且跟随监控 enabled、参照关系仍在；不成立 → `canceled`（原因写 `last_error`，供通知历史展示）
- **手动重发（Force）**：跳过事件校验，按**当前**对比状态渲染（与 update 重发「版本取发送时刻 latest」一致）

### 17.6 API 与前端

不新增端点，只在既有资源上加字段与筛选：

| 位置 | 增量 |
|---|---|
| `monitorResponse` / `monitorPayload` | `reference_monitor_id`；响应增加 `compare_state` / `compare_ref_version` / `compare_since` |
| `GET /api/v1/monitors` | `?compare=lagging\|in_sync\|ahead` 筛选 |
| `GET /api/v1/stats` | 增加 `lagging` 计数 |
| `GET /api/v1/deliveries` | 记录含 `kind`，可按类型筛选 |

前端：

- 监控编辑页：「上游参照」下拉（候选 = `reference_monitor_id` 为空且非自身的监控）+ 说明文案
- 监控列表：「对比」列（落后/一致/领先 chip，落后时展示两侧版本）+「仅看落后」筛选
- 监控详情：状态卡增加对比块（参照监控名可点击跳转、两侧版本复用 `VersionDiff`、落后时长）
- 仪表盘：「落后」计数卡片；「需要关注」包含 `lagging` 的跟随监控
- i18n：zh-Hans / en 新增文案

### 17.7 测试策略

| 层次 | 方式 |
|---|---|
| 对齐与关系 | 表驱动：`0.1.19-1` / epoch / 双侧 aur / 无 pkgrel / 非 semver 回退 |
| 事件迁移 | 状态 × 事件矩阵：进入落后、差距扩大去重、追平、故障跳过、ahead 不通知 |
| 评估器 | 临时 SQLite：入队一次且重复评估不重复；参照删除清理与作废；并发去重 |
| HTTP | `?compare=` 筛选、参照校验矩阵（自身 / 不存在 / 目标已有参照） |

### 17.8 明确不做

- 链式对比（AUR → Arch 官方 → GitHub 的多级传播）
- 「落后 N 个版本」计数（需历史推导，收益低）
- 组管理页面（组视图 = 按参照筛选 followers，列表筛选够用）
- 通用 `-数字` 剥离（只按源类型对齐，见 §17.2）

## 18. nvchecker 全面兼容与监控源扩展设计（M7 规划）

### 18.1 背景与目标

[nvchecker](https://github.com/lilydjwg/nvchecker) 作为 Linux 打包（尤其是 Arch Linux 生态）和开源版本跟踪领域的标杆，经历了十余年演进，内置了 44 种成熟稳定的监控源，涵盖了各种边界情况与上游 API 变更陷阱。

RelayWatch 在 M1–M5 阶段实现了 13 种最核心的监控源。为了让用户能够**无痛从 nvchecker 迁移并保证导入的 keyfile TOML 100% 可用**，RelayWatch 规划在 M7 阶段实现对 nvchecker 全量源的深度兼容与对齐。

#### 44 种源全量对齐矩阵

| 分类 | nvchecker 官方源（44 种） | RelayWatch 目标源类型 | 支持状态与演进规划 |
|---|---|---|---|
| **代码托管** | `github` | `github_release`, `github_tag` | 已支持，补充 `use_release_name`、GraphQL 聚合支持 |
| | `gitlab` | `gitlab_release`, `gitlab_tag` | `gitlab_release` 已支持，规划增加 tag 模式 |
| | `gitea` | `gitea_release`, `gitea_tag` | `gitea_release` 已支持，规划增加 tag 模式 |
| | `bitbucket` | `bitbucket` | 规划 P1：支持 tags/commits 检查 |
| | `pagure` | `pagure` | 规划 P1：支持 tags 检查 |
| **通用 VCS** | `git`, `git_dulwich`, `git_pygit2` | `git_tag` | **规划 P1**：远程 `git ls-remote --tags`，免 API 限制通杀所有自建 Git |
| | `mercurial` | `hg_tag` | 规划 P3：极少见，按需实现 |
| **语言包管理器** | `pypi` | `pypi` | 已支持（自动过滤 yanked 版本） |
| | `npm` | `npm` | 已支持（支持 dist_tag，默认 latest） |
| | `cratesio` | `crates` | 已支持（已注入合规 User-Agent） |
| | `go` | `go_module` | 已支持（通过 proxy.golang.org 查询 @latest） |
| | `gems` | `rubygems` | **规划 P0**：RubyGems API，自动过滤预发布/撤回版本 |
| | `maven` | `maven` | **规划 P0**：支持 Central 及自建 Maven 仓库 `maven-metadata.xml` |
| | `packagist` | `packagist` | **规划 P0**：PHP Composer API，排除 `dev-*` 分支并按发布排序 |
| | `cpan` | `cpan` | 规划 P2：MetaCPAN 快速 API |
| | `cran` | `cran` | 规划 P2：R CRAN 官方包库索引 |
| | `hackage` | `hackage` | 规划 P2：Haskell Hackage preferred.json |
| | `opam` | `opam` | 规划 P2：OCaml OPAM 仓库索引 |
| **版本聚合平台** | `repology` | `repology` | **规划 P0**：Repology REST API，极高价值（跨发行版版本对比基准） |
| | `anitya` | `anitya` | **规划 P0**：release-monitoring.org 开源上游追踪 API |
| **发行版与仓库** | `aur` | `aur` | 已支持（AUR RPC v5） |
| | `archpkg` | `arch_official` | 已支持（Arch 官方 packages REST API） |
| | `debianpkg` | `debianpkg` | 规划 P2：sources.debian.org API，支持 suite 筛选与 `strip_release` |
| | `ubuntupkg` | `ubuntupkg` | 规划 P2：Launchpad API，支持 suite 筛选与 `strip_release` |
| | `apt` | `apt_repo` | 规划 P2：直接解析远程 InRelease/Release/Packages.xz |
| | `rpmrepo` | `rpm_repo` | 规划 P2：直接解析远程 repomd.xml/primary.xml.gz |
| | `pacman`, `alpm`, `alpmfiles` | `arch_official`（自动转换） | **环境适配**：避免容器无 pacman 数据库问题，导入时智能代换为 `arch_official` |
| **通用网络提取** | `regex` | `http_regex` | 已支持（支持正则组匹配） |
| | `jq` | `http_json` | 已支持（点路径查找，规划增强标准 jq/gjson 语法） |
| | `httpheader` | `http_header` | **规划 P1**：HEAD 请求捕获 Location 重定向或特定响应头 |
| | `htmlparser` | `html_xpath` | **规划 P1**：XPath / HTML 节点选择器解析，弥补正则无法解析复杂 HTML |
| **容器镜像** | `container` | `docker_tag` / `container` | **规划 P2**：升级为标准 OCI Registry V2 规范（支持 ghcr.io, quay.io, k8s） |
| **生态与应用市场** | `vsmarketplace` | `vsmarketplace` | 规划 P3：VS Code 扩展市场查询 API |
| | `openvsx` | `openvsx` | 规划 P3：Open VSX 市场 API |
| | `snapcraft` | `snapcraft` | 规划 P3：Canonical Snap 市场 API |
| | `android_sdk` | `android_sdk` | 规划 P3：Google Android SDK XML 索引 |
| | `sparkle` | `sparkle` | 规划 P3：macOS Sparkle Appcast XML feed 检查 |
| **系统与受控源** | `manual` | `manual` | 规划 P3：静态手动版本，用于挂起人肉确认 |
| | `cmd` | `cmd` | 规划 P3：**严格受控**，默认全局禁用防 RCE，仅支持独立配置开启 |
| | `combiner` | `combiner` | 规划 P3：多监控版本合成（依托双轨对比体系实现） |
| | `none`, `launchpad` | 忽略或转特定源 | 自动识别并给出平滑处理 |

---

### 18.2 版本提取与规范化流水线升级

nvchecker 的强大不仅在于源多，还在于其高度统一的版本提取、候选过滤与规范化流水线。RelayWatch 将扩展现有版本处理机制，全面对齐 nvchecker 的处理流程：

```
上游返回 (原始版本/候选列表)
           │
           ▼
1. 候选多版本过滤 (apply_list_options)
   ├── include_regex 正则包含过滤
   ├── ignore_regex 正则排除过滤
   └── ignored 显式跳过黑名单列表
           │
           ▼
2. 最优版本选取 (SortAndPickBest)
   ├── semver 排序优先
   ├── vercmp (Arch 语义) 回退
   └── 降级字符串/字典序
           │
           ▼
3. 版本重写与规范化 (substitute_version)
   ├── prefix / suffix 剥离 (如 'v' 前缀)
   └── from_pattern -> to_pattern 正则替换重写
           │
           ▼
最终标准版本 (入库、状态机比较、触发通知)
```

#### 关键增强点：
1. **多候选统一排序机制 (`SortAndPickBest`)**：
   - 现存源（如 `github_tag`、`docker_tag`）各自手写循环选最大版本。
   - 升级为：源允许返回多条候选版本 `[]string`，由核心调用统一的 `SortAndPickBest` 函数，结合全局配置的 `version_include_regex`、`version_ignore_regex`、`ignored`，并按 semver/分段逻辑统一挑选最高版本。
2. **正则重写 (`from_pattern` / `to_pattern`)**：
   - 在 `monitors` 表增设 `from_pattern` 与 `to_pattern` 字段。
   - 在版本完成剥离前缀后，执行 `re.ReplaceAllString(ver, to_pattern)`，支持把上游非标准日期（如 `20261027`）或混合 tag（如 `release-1.2.3`）转换为标准语义版本。
3. **显式版本黑名单 (`ignored`)**：
   - 支持配置特定跳过的版本列表（如上游已知崩溃的某个破损版本 `1.0.4 1.0.4.1`），避免即使没有新版本也反复报警。

---

### 18.3 分批扩展演进路线（P0 – P3）

为稳步推进，44 种源将按照**「用户需求频次 × 实现成本 × 外部依赖」**分为四个批次交付：

#### P0：高频上游聚合与主流语言包管理器
核心聚焦 AUR 打包者与微服务开发者的刚需，零新增外部重型依赖，通过标准 HTTP JSON/XML 完成：
- **`repology`**：
  - URL：`https://repology.org/api/v1/project/{project}`
  - 参数：`project`（必填）、`repo`（必填，如 `arch` / `debian_unstable` / `aur`）、`subrepo`（可选）
  - 价值：AUR 维护者通过一条配置即可知晓上游发行版是否有新版本打包。
- **`anitya`**：
  - URL：`https://release-monitoring.org/api/project/{id}`
  - 参数：`project_id` 或 `project_name`
  - 价值：Fedora/RedHat 官方主导的全球开源软件版本元数据库。
- **`rubygems`**：
  - URL：`https://rubygems.org/api/v1/versions/{name}.json`
  - 参数：`gem`（必填）
- **`maven`**：
  - URL：`{repo}/{group_path}/{artifact}/maven-metadata.xml`（默认 Central `https://repo1.maven.org/maven2`）
  - 参数：`group`（必填）、`artifact`（必填）、`repo`（可选，支持自建 Nexus/Artifactory）
  - XML 解析：提取 `<versioning><release>`，无则降级 `<versioning><latest>`。
- **`packagist`**：
  - URL：`https://packagist.org/packages/{name}.json`
  - 参数：`package`（必填，如 `monolog/monolog`）

#### P1：通用 VCS 与高级网络提取
彻底打破对 GitHub/GitLab 平台 REST API 的依赖，通杀任意 Git 与复杂网页：
- **`git_tag` (通用 Git 源)**：
  - 参数：`url`（支持 https/git）、`branch`（可选，查 commit）、`use_commit`（布尔值）
  - 实现策略：利用纯 Go 的 `github.com/go-git/go-git/v5`（或受限调用本地无副作用命令 `git ls-remote --tags --refs {url}`），直接拉取远端 ref 列表，无需克隆完整仓库，内存消耗极低。
- **`http_header`**：
  - 参数：`url`、`header`（默认 `Location`）、`regex`、`group`
  - 请求使用 `HEAD` 方法，禁止跟随 30x 重定向，从响应头抓取真实跳转目标（完美契合形如 `.../releases/latest` 的自动化版本捕获）。
- **`htmlparser`**：
  - 参数：`url`、`xpath`、`headers`
  - 采用轻量 HTML XPath 库（`github.com/antchfx/htmlquery`）提取节点内容，解决单行正则表达式无法解析复杂多行 HTML 的痛点。
- **`bitbucket` & `pagure`**：实现其官方 REST API 适配。

#### P2：标准 OCI 容器与 Linux 跨发行版生态
- **`container` (OCI Registry V2)**：
  - 按照标准 OCI 分发规范重构当前仅限 Docker Hub 的 `docker_tag`：
    1. 请求 `https://{host}/v2/` 触发 401，解析响应头 `WWW-Authenticate: Bearer realm="...",service="..."`；
    2. 向 realm 发起 OAuth2 Token 申请，附带 `scope=repository:{image}:pull`；
    3. 携带 Bearer Token 访问 `https://{host}/v2/{image}/tags/list`；
    4. 分页提取所有 tag 并做版本比较。
  - 成果：原生覆盖 Docker Hub、`ghcr.io` (GitHub Packages)、`quay.io`、`registry.k8s.io` 以及私有 Harbor 仓库。
- **`debianpkg` & `ubuntupkg`**：对接 Debian Sources API 及 Launchpad REST API。
- **`apt_repo` & `rpm_repo`**：解析远程发行版仓库元数据（`InRelease` / `repomd.xml`）。
- **`cpan`, `cran`, `hackage`, `opam`**：补全小众语言包管理器。

#### P3：扩展应用市场、系统与特殊源
- **`vsmarketplace` & `openvsx`**：VS Code 与开源插件生态。
- **`manual`**：静态版本，支持手动触发状态确认。
- **`combiner`**：多监控版本合成器。
- **`cmd`**：本地脚本受控执行源（见下文安全设计）。

---

### 18.4 借鉴 nvchecker 源码的避坑指南

通过研读 nvchecker 源码，提炼出以下在实现各渠道时至关重要的成熟经验：

1. **GitHub 限流与退避处理** (`.tmp-nvchecker/nvchecker_source/github.py`)：
   - GitHub 对匿名请求限制为 60次/小时，带 token 为 5000次/小时；
   - 遇到 HTTP 403/429 时，nvchecker 会读取 `X-RateLimit-Reset` 或 `Retry-After` 头，在并发层挂起对应 host 的请求队列（`ALLOW_REQUEST.clear()`），休眠到重置时间后统一唤醒重试，避免直接判定为 check error 或雪崩重试。RelayWatch 的 `scheduler/ratelimit.go` 应集成该动态退避能力。
   - `use_latest_release` vs `use_release_name`：部分仓库 tag 为内部编号（如 `build-1234`），而 release name 为真实版本（如 `1.5.0`），需要支持提取 `release.name` 作为版本号。
2. **OCI 容器鉴权兼容陷阱** (`.tmp-nvchecker/nvchecker_source/container.py`)：
   - 规范上要求 `WWW-Authenticate` 必须返回 `service` 参数；
   - 但实测 **GitHub 容器镜像服务 (`ghcr.io`)** 在返回的 401 响应头中**不包含** `service` 字段！因此鉴权解析器必须允许 `service` 为空，在申请 token 时仅传入 `scope`，否则监控 ghcr.io 镜像将全量报错。
3. **Maven Central 路径构建规则** (`.tmp-nvchecker/nvchecker_source/maven.py`)：
   - `group_id` 必须将所有 `.` 转换为 `/`（例如 `org.apache.commons` 转换为 `org/apache/commons`）；
   - 文件路径严格为 `{group_path}/{artifact_id}/maven-metadata.xml`；
   - 提取版本时，必须优先取 `<versioning><release>`，当 `<release>` 节点不存在时（通常在非完全遵循规范的私服中），再回退读取 `<versioning><latest>`。
4. **包管理器 Yanked / Prerelease 过滤**：
   - **PyPI / Crates.io / RubyGems**：上游开发者撤回（yanked）的版本仍存在于历史列表中，必须排除，否则会将一个已作废的破损版本错误地上报为最新版。
   - **Packagist**：必须过滤以 `dev-` 开头的分支标签，并按 release 时间排序。
5. **合规自定义 User-Agent**：
   - Crates.io 和 MetaCPAN 会对默认的 Go-http-client 或空 UA 封锁或限流，所有 HTTP 请求必须附带符合规范的 UA（如 `RelayWatch/0.5.0 (https://github.com/xmengnet/relaywatch)`）。

---

### 18.5 安全设计与跨平台环境适配

由于 nvchecker 是本地单机 CLI，而 RelayWatch 是支持多用户、具有 Web 界面并常以 Docker 容器化运行的服务，因此在设计特殊源时必须保持最高等级的安全性：

#### 1. `cmd` 监控源安全准入与隔离
- **风险**：nvchecker 允许配置 `source = "cmd"` 执行任意 shell 脚本。在 RelayWatch 中，若允许用户在 Web 端或导入 nvchecker 配置时直接执行任意 shell 命令，将构成高危的远程代码执行（RCE）漏洞。
- **设计策略**：
  - **默认全局禁用**：服务默认关闭 `cmd` 监控源。仅当服务端配置文件显式指定 `security.enable_cmd_source: true` 时才允许加载；
  - **权限管控**：仅系统管理员（`role = "admin"`）有权限创建或保存包含 `cmd` 源的监控；
  - **nvchecker 导入保护**：从 nvchecker TOML 导入时，如果系统未开启 `enable_cmd_source`，导入器必须拦截所有 `source = "cmd"` 的条目，将其标记在 `Ignored` 告警中，并提示：“出于安全考虑，已跳过本地 shell 命令条目，建议改用 HTTP/Git 等安全源”；
  - **执行隔离**：若管理员开启该源，执行进程必须受限于严格的超时时间（默认 30s），不得继承敏感环境变量（如 `RELAYWATCH_SECRET_KEY`、数据库密码），并记录审计日志。

#### 2. `pacman` / `alpm` 本地源透明代换
- **痛点**：nvchecker 的 `pacman` 依赖本地 `pacman -Si` 命令，`alpm` 依赖调用本地 `/var/lib/pacman` 数据库。而在容器化（如官方 Docker 镜像基于 Alpine 或 Debian）或跨平台运行 RelayWatch 时，宿主机根本没有 Arch Linux 或 libalpm。
- **解决策略**：
  - 在导入 nvchecker 配置时，导入器将 `source = "pacman"` 和 `source = "alpm"` **无缝自动映射为 RelayWatch 的 `arch_official` 源**；
  - 参数直接映射（如 `alpm = "foo"` 转换为 `pkgname = "foo"`，`repo` 保持一致，`strip_release` 保留）；
  - 这样用户导入现有的 Arch 打包 nvchecker 配置文件时，既保留了 100% 的业务检测语义，又无需在运行环境安装 Arch 依赖包或挂载本地 pacman 数据库。

---

### 18.6 nvchecker 导入器全量升级规范

`internal/importer/nvchecker.go` 将从当前的有限映射，升级为完备的双向兼容转换器：

| nvchecker TOML 字段 | 对应 RelayWatch 字段 / 源类型 | 转换处理细节 |
|---|---|---|
| `source = "github"` | `github_release` / `github_tag` | 包含 `use_latest_release=true` 映射为 `github_release`；其余映射为 `github_tag`；保留 `use_release_name`、`include_prereleases` |
| `source = "gitlab"` | `gitlab_release` | 提取 `host`、`project` |
| `source = "gitea"` | `gitea_release` | 提取 `host`、`repo` |
| `source = "bitbucket"` | `bitbucket` | 提取 `repo`、`branch`、`use_max_tag` |
| `source = "pagure"` | `pagure` | 提取 `repo` |
| `source = "git"` / `git_dulwich` / `git_pygit2` | `git_tag` | 提取 `url`、`branch`、`use_commit` |
| `source = "aur"` | `aur` | 提取 `pkgname` |
| `source = "archpkg"` | `arch_official` | 提取 `pkgname`、`repo` |
| `source = "pacman"` / `alpm` | `arch_official` | 智能透明代换，提取 `pkgname`，记录优化日志 |
| `source = "pypi"` | `pypi` | 提取 `project` |
| `source = "npm"` | `npm` | 提取 `package`、`dist_tag` |
| `source = "cratesio"` | `crates` | 提取 `crate` |
| `source = "go"` / `goproxy` | `go_module` | 提取 `module` |
| `source = "gems"` | `rubygems` | 提取 `gem` |
| `source = "maven"` | `maven` | 提取 `group`、`artifact`、`repo` |
| `source = "packagist"` | `packagist` | 提取 `package` |
| `source = "cpan"` | `cpan` | 提取 `cpan` |
| `source = "cran"` | `cran` | 提取 `cran` |
| `source = "hackage"` | `hackage` | 提取 `hackage` |
| `source = "repology"` | `repology` | 提取 `project`、`repo`、`subrepo` |
| `source = "anitya"` | `anitya` | 提取 `project_id` |
| `source = "debianpkg"` | `debianpkg` | 提取 `pkgname`、`suite`、`strip_release` |
| `source = "ubuntupkg"` | `ubuntupkg` | 提取 `pkgname`、`suite`、`strip_release` |
| `source = "regex"` | `http_regex` | 提取 `url`、`regex` |
| `source = "httpheader"` | `http_header` | 提取 `url`、`header`、`regex` |
| `source = "htmlparser"` | `html_xpath` | 提取 `url`、`xpath` |
| `source = "jq"` | `http_json` | 提取 `url`、`filter` (转换为 path) |
| `source = "container"` | `container` / `docker_tag` | 提取 `image`、`registry`、`tag_regex` |
| `source = "vsmarketplace"` | `vsmarketplace` | 提取 `extension` |
| `source = "openvsx"` | `openvsx` | 提取 `extension` |
| `source = "snapcraft"` | `snapcraft` | 提取 `snap` |
| `source = "manual"` | `manual` | 提取 `manual` 静态版本值 |
| `source = "cmd"` | `cmd` | 仅在系统开启时导入，否则安全拦截告警 |
| `from_pattern` + `to_pattern` | `from_pattern` + `to_pattern` | 映射入库，由规范化流水线执行正则替换 |
| `ignored` | `version_ignored` | 空格分隔列表映射 |
| `include_regex` / `exclude_regex` | `version_include_regex` / `version_ignore_regex` | 直接映射 |
| `prefix` | `version_prefix` | 直接映射 |

