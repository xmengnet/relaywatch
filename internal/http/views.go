package httpapi

import (
	"encoding/json"

	"relaywatch/internal/store"
)

// userResponse 是 API 输出的用户视图，绝不包含密码哈希等内部字段。
type userResponse struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role"`
}

func toUserResponse(u *store.User) *userResponse {
	if u == nil {
		return nil
	}
	return &userResponse{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Role:        u.Role,
	}
}

// adminUserResponse 用户管理视图（admin 用），含来源/禁用/登录时间与资源计数。
type adminUserResponse struct {
	ID              int64    `json:"id"`
	Username        string   `json:"username"`
	Email           string   `json:"email,omitempty"`
	DisplayName     string   `json:"display_name,omitempty"`
	Role            string   `json:"role"`
	Permissions     []string `json:"permissions"` // null = 未配置（按角色默认）
	OIDCProvider    string   `json:"oidc_provider,omitempty"`
	Disabled        bool     `json:"disabled"`
	CreatedAt       string   `json:"created_at"`
	LastLoginAt     *string  `json:"last_login_at,omitempty"`
	MonitorCount    int64    `json:"monitor_count"`
}

func toAdminUserResponse(u *store.User, monitorCount int64) *adminUserResponse {
	if u == nil {
		return nil
	}
	r := &adminUserResponse{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		Role:         u.Role,
		Permissions:  u.Permissions, // nil 保留：前端显示"按角色默认"
		OIDCProvider: u.OIDCProvider,
		Disabled:     u.Disabled,
		CreatedAt:    u.CreatedAt.Format(timeRFC3339),
		MonitorCount: monitorCount,
	}
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.Format(timeRFC3339)
		r.LastLoginAt = &s
	}
	return r
}

// monitorResponse 是 API 输出的监控视图。
type monitorResponse struct {
	ID                  int64           `json:"id"`
	OwnerID             int64           `json:"owner_id,omitempty"`
	OwnerName           string          `json:"owner_name,omitempty"` // 仅 admin 列表填充
	Name                string          `json:"name"`
	SourceType          string          `json:"source_type"`
	SourceConfig        json.RawMessage `json:"source_config"`
	VersionPrefix       string          `json:"version_prefix,omitempty"`
	VersionSuffix       string          `json:"version_suffix,omitempty"`
	VersionIncludeRegex string          `json:"version_include_regex,omitempty"`
	VersionIgnoreRegex  string          `json:"version_ignore_regex,omitempty"`
	FromPattern         string          `json:"from_pattern,omitempty"`
	ToPattern           string          `json:"to_pattern,omitempty"`
	IntervalSec         int             `json:"interval_sec"`
	Enabled             bool            `json:"enabled"`
	State               string          `json:"state"`
	CurrentVersion      string          `json:"current_version,omitempty"`
	LatestVersion       string          `json:"latest_version,omitempty"`
	LastCheckedAt       *string         `json:"last_checked_at,omitempty"`
	LastSuccessAt       *string         `json:"last_success_at,omitempty"`
	LastChangeAt        *string         `json:"last_change_at,omitempty"`
	LastError           string          `json:"last_error,omitempty"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	NotifyCooldownSec   int             `json:"notify_cooldown_sec"`
	Tags                []string        `json:"tags"`
	Note                string          `json:"note,omitempty"`
	ChannelIDs          []int64         `json:"channel_ids"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`

	// 双轨对比（M6）
	ReferenceMonitorID   *int64  `json:"reference_monitor_id,omitempty"`
	ReferenceMonitorName string  `json:"reference_monitor_name,omitempty"`
	CompareState         string  `json:"compare_state,omitempty"`
	CompareRefVersion    string  `json:"compare_ref_version,omitempty"`
	CompareSince         *string `json:"compare_since,omitempty"`
}

func toMonitorResponse(m *store.Monitor) *monitorResponse {
	if m == nil {
		return nil
	}
	var cmpSince *string
	if m.CompareSince != nil {
		s := m.CompareSince.Format(timeRFC3339)
		cmpSince = &s
	}
	r := &monitorResponse{
		ID:                  m.ID,
		OwnerID:             m.OwnerID,
		Name:                m.Name,
		SourceType:          m.SourceType,
		VersionPrefix:       m.VersionPrefix,
		VersionSuffix:       m.VersionSuffix,
		VersionIncludeRegex: m.VersionIncludeRegex,
		VersionIgnoreRegex:  m.VersionIgnoreRegex,
		FromPattern:         m.FromPattern,
		ToPattern:           m.ToPattern,
		IntervalSec:         m.IntervalSec,
		Enabled:             m.Enabled,
		State:               string(m.State),
		CurrentVersion:      m.CurrentVersion,
		LatestVersion:       m.LatestVersion,
		LastError:           m.LastError,
		ConsecutiveFailures: m.ConsecutiveFailures,
		NotifyCooldownSec:   m.NotifyCooldownSec,
		Tags:                m.Tags,
		Note:                m.Note,
		ChannelIDs:          m.ChannelIDs,
		CreatedAt:           m.CreatedAt.Format(timeRFC3339),
		UpdatedAt:           m.UpdatedAt.Format(timeRFC3339),
		ReferenceMonitorID:  m.ReferenceMonitorID,
		CompareState:        m.CompareState,
		CompareRefVersion:   m.CompareRefVersion,
		CompareSince:        cmpSince,
	}
	if r.Tags == nil {
		r.Tags = []string{}
	}
	if r.ChannelIDs == nil {
		r.ChannelIDs = []int64{}
	}
	// source_config 存库为 JSON 字符串，API 直出为 JSON 对象；
	// 空或非法时输出空对象，前端表单按对象消费
	if raw := []byte(m.SourceConfig); len(raw) > 0 && json.Valid(raw) {
		r.SourceConfig = json.RawMessage(raw)
	} else {
		r.SourceConfig = json.RawMessage("{}")
	}
	if m.LastCheckedAt != nil {
		s := m.LastCheckedAt.Format(timeRFC3339)
		r.LastCheckedAt = &s
	}
	if m.LastSuccessAt != nil {
		s := m.LastSuccessAt.Format(timeRFC3339)
		r.LastSuccessAt = &s
	}
	if m.LastChangeAt != nil {
		s := m.LastChangeAt.Format(timeRFC3339)
		r.LastChangeAt = &s
	}
	return r
}

// tokenResponse 是 API 输出的 Token 视图，不暴露哈希。
type tokenResponse struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

func toTokenResponse(t *store.APIToken) *tokenResponse {
	if t == nil {
		return nil
	}
	r := &tokenResponse{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt.Format(timeRFC3339),
	}
	if t.ExpiresAt != nil {
		s := t.ExpiresAt.Format(timeRFC3339)
		r.ExpiresAt = &s
	}
	if t.LastUsedAt != nil {
		s := t.LastUsedAt.Format(timeRFC3339)
		r.LastUsedAt = &s
	}
	return r
}

// checkHistoryResponse 是检查历史的 API 视图。
type checkHistoryResponse struct {
	ID         int64  `json:"id"`
	MonitorID  int64  `json:"monitor_id"`
	CheckedAt  string `json:"checked_at"`
	DurationMS int64  `json:"duration_ms"`
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	Error      string `json:"error,omitempty"`
}

func toCheckHistoryResponse(h *store.CheckHistory) *checkHistoryResponse {
	return &checkHistoryResponse{
		ID:         h.ID,
		MonitorID:  h.MonitorID,
		CheckedAt:  h.CheckedAt.Format(timeRFC3339),
		DurationMS: h.DurationMS,
		OK:         h.OK,
		Version:    h.Version,
		Error:      h.Error,
	}
}

// versionChangeResponse 是版本变更的 API 视图。
type versionChangeResponse struct {
	ID             int64   `json:"id"`
	MonitorID      int64   `json:"monitor_id"`
	OldVersion     string  `json:"old_version,omitempty"`
	NewVersion     string  `json:"new_version"`
	DetectedAt     string  `json:"detected_at"`
	AcknowledgedAt *string `json:"acknowledged_at,omitempty"`
}

func toVersionChangeResponse(c *store.VersionChange) *versionChangeResponse {
	r := &versionChangeResponse{
		ID:         c.ID,
		MonitorID:  c.MonitorID,
		OldVersion: c.OldVersion,
		NewVersion: c.NewVersion,
		DetectedAt: c.DetectedAt.Format(timeRFC3339),
	}
	if c.AcknowledgedAt != nil {
		s := c.AcknowledgedAt.Format(timeRFC3339)
		r.AcknowledgedAt = &s
	}
	return r
}

// channelResponse 是渠道的 API 视图（config 原样返回；M4 加密后需掩码）。
type channelResponse struct {
	ID        int64  `json:"id"`
	OwnerID   *int64 `json:"owner_id,omitempty"` // nil = 全局共享
	Name      string `json:"name"`
	Type      string `json:"type"`
	Config    string `json:"config"`
	Template  string `json:"template,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toChannelResponse(c *store.Channel) *channelResponse {
	if c == nil {
		return nil
	}
	return &channelResponse{
		ID:        c.ID,
		OwnerID:   c.OwnerID,
		Name:      c.Name,
		Type:      c.Type,
		Config:    c.Config,
		Template:  c.Template,
		Enabled:   c.Enabled,
		CreatedAt: c.CreatedAt.Format(timeRFC3339),
		UpdatedAt: c.UpdatedAt.Format(timeRFC3339),
	}
}

// deliveryResponse 是投递记录的 API 视图。
type deliveryResponse struct {
	ID            int64   `json:"id"`
	MonitorID     int64   `json:"monitor_id"`
	MonitorName   string  `json:"monitor_name,omitempty"`
	ChannelID     int64   `json:"channel_id"`
	ChannelName   string  `json:"channel_name,omitempty"`
	Kind          string  `json:"kind"`
	ChangeID      *int64  `json:"change_id,omitempty"`
	Payload       string  `json:"payload,omitempty"`
	Status        string  `json:"status"`
	Attempts      int     `json:"attempts"`
	NextAttemptAt *string `json:"next_attempt_at,omitempty"`
	LastError     string  `json:"last_error,omitempty"`
	CreatedAt     string  `json:"created_at"`
	SentAt        *string `json:"sent_at,omitempty"`
}

func toDeliveryResponse(d *store.Delivery) *deliveryResponse {
	if d == nil {
		return nil
	}
	kind := d.Kind
	if kind == "" {
		kind = "update"
	}
	r := &deliveryResponse{
		ID:        d.ID,
		MonitorID: d.MonitorID,
		ChannelID: d.ChannelID,
		Kind:      kind,
		ChangeID:  d.ChangeID,
		Payload:   d.Payload,
		Status:    string(d.Status),
		Attempts:  d.Attempts,
		LastError: d.LastError,
		CreatedAt: d.CreatedAt.Format(timeRFC3339),
	}
	if d.NextAttemptAt != nil {
		s := d.NextAttemptAt.Format(timeRFC3339)
		r.NextAttemptAt = &s
	}
	if d.SentAt != nil {
		s := d.SentAt.Format(timeRFC3339)
		r.SentAt = &s
	}
	return r
}

const timeRFC3339 = "2006-01-02T15:04:05.999999999Z07:00"
