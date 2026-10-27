package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ---------- 检查历史 ----------

func (s *sqliteStore) AddCheckHistory(ctx context.Context, h *CheckHistory) error {
	res, err := s.db.exec(ctx, `INSERT INTO check_history
		(monitor_id, checked_at, duration_ms, ok, version, error)
		VALUES (?, ?, ?, ?, ?, ?)`,
		h.MonitorID, formatTime(h.CheckedAt), h.DurationMS, boolInt(h.OK), nullString(h.Version), nullString(h.Error))
	if err != nil {
		return err
	}
	h.ID, err = res.LastInsertId()
	return err
}

const checkHistoryCols = `id, monitor_id, checked_at, duration_ms, ok, version, error`

func (s *sqliteStore) checkHistoryFromRow(scan func(dest ...any) error) (*CheckHistory, error) {
	h := &CheckHistory{}
	var checked, version, errStr sql.NullString
	var ok int
	err := scan(&h.ID, &h.MonitorID, &checked, &h.DurationMS, &ok, &version, &errStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h.CheckedAt, _ = time.Parse(time.RFC3339Nano, checked.String)
	h.OK = ok != 0
	h.Version = version.String
	h.Error = errStr.String
	return h, nil
}

func (s *sqliteStore) ListCheckHistory(ctx context.Context, monitorID int64, page, perPage int) ([]*CheckHistory, int, error) {
	var total int
	if err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM check_history WHERE monitor_id = ?`, monitorID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if perPage <= 0 {
		perPage = 20
	}
	if page <= 0 {
		page = 1
	}
	rows, err := s.db.query(ctx,
		`SELECT `+checkHistoryCols+` FROM check_history WHERE monitor_id = ? ORDER BY checked_at DESC LIMIT ? OFFSET ?`,
		monitorID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*CheckHistory
	for rows.Next() {
		h, err := s.checkHistoryFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ---------- 版本变更 ----------

func (s *sqliteStore) AddVersionChange(ctx context.Context, c *VersionChange) error {
	res, err := s.db.exec(ctx, `INSERT INTO version_changes
		(monitor_id, old_version, new_version, detected_at, acknowledged_at, acknowledged_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		c.MonitorID, nullString(c.OldVersion), c.NewVersion, formatTime(c.DetectedAt), nullTime(c.AcknowledgedAt), c.AcknowledgedBy)
	if err != nil {
		return err
	}
	c.ID, err = res.LastInsertId()
	return err
}

const versionChangeCols = `id, monitor_id, old_version, new_version, detected_at, acknowledged_at, acknowledged_by`

func (s *sqliteStore) versionChangeFromRow(scan func(dest ...any) error) (*VersionChange, error) {
	c := &VersionChange{}
	var oldV, detected, ackAt, ackBy sql.NullString
	err := scan(&c.ID, &c.MonitorID, &oldV, &c.NewVersion, &detected, &ackAt, &ackBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.OldVersion = oldV.String
	c.DetectedAt, _ = time.Parse(time.RFC3339Nano, detected.String)
	if ackAt.Valid && ackAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, ackAt.String); err == nil {
			c.AcknowledgedAt = &t
		}
	}
	if ackBy.Valid && ackBy.String != "" {
		var id int64
		if _, err := fmt.Sscanf(ackBy.String, "%d", &id); err == nil {
			c.AcknowledgedBy = &id
		}
	}
	return c, nil
}

// OpenVersionChange 返回该监控未 ack 的最新一条变更，没有则 ErrNotFound。
func (s *sqliteStore) OpenVersionChange(ctx context.Context, monitorID int64) (*VersionChange, error) {
	return s.versionChangeFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx,
			`SELECT `+versionChangeCols+` FROM version_changes
			 WHERE monitor_id = ? AND acknowledged_at IS NULL
			 ORDER BY detected_at DESC LIMIT 1`, monitorID).Scan(dest...)
	})
}

func (s *sqliteStore) UpdateVersionChange(ctx context.Context, c *VersionChange) error {
	_, err := s.db.exec(ctx, `UPDATE version_changes SET
		old_version = ?, new_version = ?, acknowledged_at = ?, acknowledged_by = ?
		WHERE id = ?`,
		nullString(c.OldVersion), c.NewVersion, nullTime(c.AcknowledgedAt), c.AcknowledgedBy, c.ID)
	return err
}

func (s *sqliteStore) ListVersionChanges(ctx context.Context, monitorID int64, page, perPage int) ([]*VersionChange, int, error) {
	var total int
	if err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM version_changes WHERE monitor_id = ?`, monitorID).Scan(&total); err != nil {
		return nil, 0, err
	}
	if perPage <= 0 {
		perPage = 20
	}
	if page <= 0 {
		page = 1
	}
	rows, err := s.db.query(ctx,
		`SELECT `+versionChangeCols+` FROM version_changes WHERE monitor_id = ? ORDER BY detected_at DESC LIMIT ? OFFSET ?`,
		monitorID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*VersionChange
	for rows.Next() {
		c, err := s.versionChangeFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ---------- 监控状态更新（Checker 使用） ----------

// UpdateMonitorCheckResult 写入一次检查后的监控行状态。
// 单条 UPDATE，避免 读-改-写 竞态。LastNotifiedAt 为 nil 时保持原值。
func (s *sqliteStore) UpdateMonitorCheckResult(ctx context.Context, id int64, p CheckResult) error {
	lastNotified := "last_notified_at"
	if p.LastNotifiedAt != nil {
		lastNotified = "?"
	}
	q := `UPDATE monitors SET
		state = ?, current_version = ?, latest_version = ?,
		last_checked_at = ?, last_success_at = ?, last_change_at = ?,
		last_error = ?, consecutive_failures = ?, updated_at = ?,` +
		` last_notified_at = COALESCE(` + lastNotified + `, last_notified_at)
		WHERE id = ?`
	args := []any{
		string(p.State), nullString(p.CurrentVersion), nullString(p.LatestVersion),
		formatTime(p.LastCheckedAt), nullTime(p.LastSuccessAt), nullTime(p.LastChangeAt),
		nullString(p.LastError), p.ConsecutiveFailures, formatTime(time.Now()),
	}
	if p.LastNotifiedAt != nil {
		args = append(args, formatTime(*p.LastNotifiedAt))
	}
	args = append(args, id)
	_, err := s.db.exec(ctx, q, args...)
	return err
}

// CheckResult 一次检查后监控行的增量状态。
type CheckResult struct {
	State               MonitorState
	CurrentVersion      string
	LatestVersion       string
	LastCheckedAt       time.Time
	LastSuccessAt       *time.Time
	LastChangeAt        *time.Time
	LastError           string
	ConsecutiveFailures int
	LastNotifiedAt      *time.Time // 零值字段用 nil 传入时不更新……简化：nil = 不变
}

// ListEnabledMonitors 返回所有启用的监控（调度器用）。
func (s *sqliteStore) ListEnabledMonitors(ctx context.Context) ([]*Monitor, error) {
	rows, err := s.db.query(ctx, `SELECT `+monitorCols+` FROM monitors WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Monitor
	for rows.Next() {
		m, err := s.monitorFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
