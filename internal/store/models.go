// Package store 定义领域模型与存储接口，提供 SQLite 实现。
package store

import (
	"time"
)

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
	PasswordHash []byte // OIDC 用户为 nil
	Role         string // admin | viewer
	OIDCProvider string
	OIDCSubject  string
	Disabled     bool
	CreatedAt    time.Time
	LastLoginAt  *time.Time
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
