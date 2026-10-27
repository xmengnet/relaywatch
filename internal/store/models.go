// Package store 定义领域模型与存储接口，提供 SQLite 实现。
package store

import (
	"time"
)

// Can 判定细粒度权限：admin 恒真；显式配置的 Permissions 优先；nil 按角色默认。
func (u *User) Can(perm string) bool {
	if u == nil {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	perms := u.Permissions
	if perms == nil {
		perms = DefaultPermissions(u.Role)
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// UserFilter 用户管理列表查询条件（admin 用）。
type UserFilter struct {
	Role    string // 空 = 全部
	Search  string // 匹配用户名/邮箱/显示名
	Page    int
	PerPage int
}

// MonitorState 监控状态机。
type MonitorState string

const (
	StateUnknown   MonitorState = "unknown"
	StateUpToDate  MonitorState = "up_to_date"
	StateHasUpdate MonitorState = "has_update"
	StateError     MonitorState = "error"
	StateMissing   MonitorState = "missing"
)

// User 本地或 OIDC 用户。
type User struct {
	ID           int64
	Username     string
	Email        string
	DisplayName  string
	PasswordHash []byte // OIDC 用户为 nil
	Role         string // admin | user | viewer
	Permissions  []string // 细粒度操作权限（见 Permission* 常量）；nil = 未配置（按角色默认）
	OIDCProvider string
	OIDCSubject  string
	Disabled     bool
	CreatedAt    time.Time
	LastLoginAt  *time.Time
}

// 细粒度权限项。admin 恒有全部权限；user/viewer 按用户配置，
// 未配置（nil）时回退到角色默认（DefaultPermissions）。
const (
	PermCreateMonitor = "monitor.create"
	PermCreateChannel = "channel.create"
	PermImport        = "import"
	PermExport        = "export"
)

// AllPermissions 全部权限项（用户管理 UI 用此顺序展示）。
var AllPermissions = []string{PermCreateMonitor, PermCreateChannel, PermImport, PermExport}

// DefaultPermissions 角色默认权限：user 可建监控/渠道/导入/导出，viewer 全只读。
// Permissions 字段为 nil 时按此回退；显式空数组 = 全部禁用。
func DefaultPermissions(role string) []string {
	switch role {
	case "admin":
		return AllPermissions
	case "viewer":
		return nil
	default: // user
		return AllPermissions
	}
}

// Session 登录会话。
type Session struct {
	ID        string
	UserID    int64
	CSRFToken string
	UserAgent string
	IP        string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// APIToken 脚本调用的 Bearer Token。
type APIToken struct {
	ID         int64
	UserID     int64
	Name       string
	TokenHash  string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// Monitor 一条监控记录。
type Monitor struct {
	ID                  int64
	OwnerID             int64 // 归属用户；0 = 未归属（迁移回填前的存量）
	Name                string
	SourceType          string
	SourceConfig        string // JSON
	VersionPrefix       string
	VersionSuffix       string
	VersionIncludeRegex string
	VersionIgnoreRegex  string
	FromPattern         string
	ToPattern           string
	IntervalSec         int
	Enabled             bool
	State               MonitorState
	CurrentVersion      string
	LatestVersion       string
	LastCheckedAt       *time.Time
	LastSuccessAt       *time.Time
	LastChangeAt        *time.Time
	LastError           string
	ConsecutiveFailures int
	NotifyCooldownSec   int
	LastNotifiedAt      *time.Time
	Tags                []string
	Note                string
	CreatedAt           time.Time
	UpdatedAt           time.Time

	ChannelIDs []int64 // 绑定的通知渠道

	// 双轨对比（M6）
	ReferenceMonitorID *int64
	CompareState       string // "" | "in_sync" | "lagging" | "ahead"
	CompareRefVersion  string
	CompareSince       *time.Time
	CompareNotifiedRef string
}

// MonitorFilter 列表查询条件。
type MonitorFilter struct {
	State        MonitorState
	CompareState string
	Tag          string
	Search       string
	OwnerID      int64 // 0 = 不过滤（仅 admin 允许）；>0 = 只看该用户
	Page         int
	PerPage      int
	SortBy       string
	SortOrder    string // asc | desc
}

// CheckHistory 一次检查的记录。
type CheckHistory struct {
	ID         int64
	MonitorID  int64
	CheckedAt  time.Time
	DurationMS int64
	OK         bool
	Version    string
	Error      string
}

// VersionChange 一次版本变更（未确认 = 开放周期）。
type VersionChange struct {
	ID             int64
	MonitorID      int64
	OldVersion     string
	NewVersion     string
	DetectedAt     time.Time
	AcknowledgedAt *time.Time
	AcknowledgedBy *int64
}

// Channel 通知渠道。
type Channel struct {
	ID        int64
	OwnerID   *int64 // NULL = 全局共享渠道（admin 建，所有用户可引用）
	Name      string
	Type      string // telegram | webhook | ...
	Config    string // JSON，敏感字段（M4 加密）
	Template  string // 自定义模板，空则用默认
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeliveryStatus 投递状态。
type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliverySent      DeliveryStatus = "sent"
	DeliveryFailed    DeliveryStatus = "failed"
	DeliveryDeferred  DeliveryStatus = "deferred" // 静默期推迟
	DeliveryCanceled  DeliveryStatus = "canceled" // 作废（终态）
)

// Delivery 一条通知投递任务/记录。
type Delivery struct {
	ID            int64
	MonitorID     int64
	ChannelID     int64
	ChangeID      *int64
	Payload       string // 实际发送内容（渲染后）
	Status        DeliveryStatus
	Attempts      int
	NextAttemptAt *time.Time
	LastError     string
	Kind          string // "update" | "compare"
	Context       string // JSON 快照
	CreatedAt     time.Time
	SentAt        *time.Time

	Force bool // 运行时标记：手动重发，跳过周期校验（不落库）
}

// AuditEntry 审计日志。
type AuditEntry struct {
	ID        int64
	UserID    *int64
	Action    string
	Target    string
	Detail    string
	IP        string
	CreatedAt time.Time
}
