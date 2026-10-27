# RelayWatch 设计方案

版本：0.3.0
对应需求：[requirements.md](./requirements.md) v0.2.1
实现进度：M1–M4 已完成（见 README「状态」），本文标注「实现注记」处为编码时确定的事实。

变更：v0.1 → v0.2 根据评审意见修订，同步需求 v0.2：新增 api_tokens / settings 表、deliveries 调度字段、版本过滤与变更周期语义、调度抖动与 per-host 限速、nvchecker 导入、手动检查改为同步返回、CSP 修正、`relaywatch rekey` 子命令。

v0.2 → v0.2.1 修正三处：`gjson` 版本改为实测的 v1.20.0；调度 jitter 改为由监控 ID 哈希推导的确定性因子（原方案重启后重新随机会导致到期时间跳变、触发多余检查）；补充 `deliveries` 的 `canceled` 终态与 deferred 补发前的四项校验，明确用户 ack/忽略时待投递记录作废。

v0.2.1 → v0.3.0（实现期修订，与 M1–M4 代码同步）：
- 前端 TypeScript 固定 5.9（vue-tsc 3.3.13 与 TS7 不兼容，`ERR_PACKAGE_PATH_NOT_EXPORTED`）
- jitter 公式修正：`Sum64()%2000/2000*0.2-0.1`（原 `%2001/10000` 上界恰好越界到 +0.1）
- 敏感字段加密 M4 已落地：渠道 config / 监控 source_config / OIDC client_secret 落库前 AES-256-GCM 加密；密文 `enc:` 前缀；明文兼容读取；`secret_key` 未设置拒绝启动
- 手动重发增加 Force 语义：跳过开放变更校验（用户明确要求再发一次），版本号仍取发送时刻 latest_version
- 认证中间件顺序实现为 `Recoverer → RealIP → 安全头 → Session/Bearer → RequireAuth → RequireCSRF`，API Token（Bearer）调用豁免 CSRF
- HTTP 层一律经由脱敏视图（views.go）输出，密码哈希与内部字段不出 API

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

- **上游 vs 本地双轨对比**（v2 候选，需求文档 §5）：`monitors` 加 `group_key`，同组内上游源与发行版源（AUR/Arch）的版本并列展示并计算落后差
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
