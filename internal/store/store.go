package store

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SecretBox 敏感字段加解密接口（实现见 internal/crypto）。
type SecretBox interface {
	Encrypt(plain string) (string, error)
	Decrypt(s string) (string, error)
}

// sqliteStore 是 Store 的 SQLite 实现。
type sqliteStore struct {
	db   *sqlDB
	box  SecretBox
}

// Store 数据访问接口。M1 覆盖用户/会话/Token/监控；其余随里程碑扩展。
type Store interface {
	// 迁移与生命周期
	Migrate(ctx context.Context) error
	Close() error
	// SetSecretBox 注入敏感字段加解密器（nil = 不加密，仅测试用）。
	SetSecretBox(box SecretBox)

	// 用户
	CreateUser(ctx context.Context, u *User) error
	UserByUsername(ctx context.Context, name string) (*User, error)
	UserByID(ctx context.Context, id int64) (*User, error)
	UserCount(ctx context.Context) (int64, error)
	UpdateUserPassword(ctx context.Context, id int64, hash []byte) error
	UpdateUserLogin(ctx context.Context, id int64, at time.Time) error
	// 用户管理（M8）
	ListUsers(ctx context.Context, f UserFilter) ([]*User, int, error)
	UpdateUserProfile(ctx context.Context, u *User) error // email / display_name / role / password_hash（nil 不改）
	SetUserDisabled(ctx context.Context, id int64, disabled bool) error
	HasEnabledAdmin(ctx context.Context) (int64, error)
	DeleteUserTransferResources(ctx context.Context, userID, toUserID int64) error
	CountMonitorsByOwner(ctx context.Context, ownerID int64) (int64, error)

	// 会话
	CreateSession(ctx context.Context, s *Session) error
	SessionByID(ctx context.Context, id string) (*Session, error)
	RevokeSession(ctx context.Context, id string) error
	// RevokeUserSessions 吊销某用户全部未过期会话（禁用/重置密码用），返回吊销数。
	RevokeUserSessions(ctx context.Context, userID int64) (int64, error)
	// ListSessionsByUser 用户全部未吊销且未过期的会话（按创建时间倒序，profile 会话管理用）。
	ListSessionsByUser(ctx context.Context, userID int64) ([]*Session, error)
	DeleteExpiredSessions(ctx context.Context) (int64, error)

	// API Token
	CreateAPIToken(ctx context.Context, t *APIToken) error
	APITokenByHash(ctx context.Context, hash string) (*APIToken, error)
	ListAPITokens(ctx context.Context, userID int64) ([]*APIToken, error)
	RevokeAPIToken(ctx context.Context, id int64) error
	TouchAPIToken(ctx context.Context, id int64) error

	// 监控
	CreateMonitor(ctx context.Context, m *Monitor) error
	GetMonitor(ctx context.Context, id int64) (*Monitor, error)
	ListMonitors(ctx context.Context, f MonitorFilter) ([]*Monitor, int, error)
	ListEnabledMonitors(ctx context.Context) ([]*Monitor, error)
	UpdateMonitor(ctx context.Context, m *Monitor) error
	DeleteMonitor(ctx context.Context, id int64) error
	SetMonitorChannels(ctx context.Context, monitorID int64, channelIDs []int64) error
	SetMonitorOwner(ctx context.Context, monitorID, ownerID int64) error

	// 检查历史
	AddCheckHistory(ctx context.Context, h *CheckHistory) error
	ListCheckHistory(ctx context.Context, monitorID int64, page, perPage int) ([]*CheckHistory, int, error)

	// 版本变更
	AddVersionChange(ctx context.Context, c *VersionChange) error
	OpenVersionChange(ctx context.Context, monitorID int64) (*VersionChange, error)
	UpdateVersionChange(ctx context.Context, c *VersionChange) error
	ListVersionChanges(ctx context.Context, monitorID int64, page, perPage int) ([]*VersionChange, int, error)

	// 检查结果落库（Checker 用）
	UpdateMonitorCheckResult(ctx context.Context, id int64, p CheckResult) error
	// 双轨对比（M6）
	UpdateMonitorCompare(ctx context.Context, id int64, compareState, compareRefVersion string, compareSince *time.Time, compareNotifiedRef string) error
	ListFollowerMonitors(ctx context.Context, refMonitorID int64) ([]*Monitor, error)
	ClearFollowerReferences(ctx context.Context, refMonitorID int64) error

	// 渠道
	CreateChannel(ctx context.Context, c *Channel) error
	GetChannel(ctx context.Context, id int64) (*Channel, error)
	// ListChannelsVisible 返回用户可见渠道：自己的 + 全局共享（ownerID=0 即全局）。
	ListChannelsVisible(ctx context.Context, ownerID int64) ([]*Channel, error)
	// ListChannelsAll 仅 admin：全部渠道。
	ListChannelsAll(ctx context.Context) ([]*Channel, error)
	UpdateChannel(ctx context.Context, c *Channel) error
	DeleteChannel(ctx context.Context, id int64) error
	ChannelUsageCount(ctx context.Context, channelID int64) (int, error)

	// 投递
	AddDelivery(ctx context.Context, d *Delivery) error
	UpdateDelivery(ctx context.Context, d *Delivery) error
	GetDelivery(ctx context.Context, id int64) (*Delivery, error)
	DueDeliveries(ctx context.Context, now time.Time, limit int) ([]*Delivery, error)
	ListDeliveries(ctx context.Context, monitorID, channelID int64, status DeliveryStatus, page, perPage int) ([]*Delivery, int, error)
	// CancelOpenDeliveries 作废某监控当前未确认变更的待投递记录（ack/停用/重发用）。
	CancelOpenDeliveries(ctx context.Context, monitorID int64, reason string) (int64, error)

	// 系统设置
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error

	// 审计
	AddAudit(ctx context.Context, a *AuditEntry) error

	// OIDC Provider
	CreateOIDCProvider(ctx context.Context, p *OIDCProvider) error
	ListOIDCProviders(ctx context.Context) ([]*OIDCProvider, error)
	GetOIDCProvider(ctx context.Context, id int64) (*OIDCProvider, error)
	OIDCProviderByName(ctx context.Context, name string) (*OIDCProvider, error)
	UpdateOIDCProvider(ctx context.Context, p *OIDCProvider) error
	DeleteOIDCProvider(ctx context.Context, id int64) error
	UserByOIDC(ctx context.Context, provider, subject string) (*User, error)
	SetUserOIDC(ctx context.Context, userID int64, provider, subject string) error
}

// Open 打开（或创建）SQLite 数据库并应用 PRAGMA。
func Open(path string) (Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := connect(dsn)
	if err != nil {
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

// Migrate 依次应用未执行的迁移文件。
func (s *sqliteStore) Migrate(ctx context.Context) error {
	if _, err := s.db.exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		// 文件名形如 0001_init.sql，取前导数字为版本号
		base := strings.TrimSuffix(name, ".sql")
		var version int64
		if _, err := fmt.Sscanf(base, "%d", &version); err != nil {
			return fmt.Errorf("migration file %s: bad version prefix", name)
		}
		var applied int
		err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied)
		if err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.db.begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.exec(ctx, string(sqlBytes)); err != nil {
			tx.rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.exec(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, time.Now().UTC()); err != nil {
			tx.rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}
