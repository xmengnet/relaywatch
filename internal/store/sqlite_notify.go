package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ---------- 渠道 ----------

const channelCols = `id, owner_id, name, type, config, template, enabled, created_at, updated_at`

func (s *sqliteStore) channelFromRow(scan func(dest ...any) error) (*Channel, error) {
	c := &Channel{}
	var template sql.NullString
	var ownerID sql.NullInt64
	var enabled int
	err := scan(&c.ID, &ownerID, &c.Name, &c.Type, &c.Config, &template, &enabled, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		id := ownerID.Int64
		c.OwnerID = &id
	}
	c.Template = template.String
	c.Enabled = enabled != 0
	c.Config = s.decField(c.Config)
	return c, nil
}

func (s *sqliteStore) CreateChannel(ctx context.Context, c *Channel) error {
	now := time.Now()
	storedConfig := s.encField(c.Config)
	res, err := s.db.exec(ctx, `INSERT INTO channels (owner_id, name, type, config, template, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.OwnerID, c.Name, c.Type, storedConfig, nullString(c.Template), boolInt(c.Enabled), formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	c.ID, err = res.LastInsertId()
	c.CreatedAt, c.UpdatedAt = now, now
	return err
}

func (s *sqliteStore) GetChannel(ctx context.Context, id int64) (*Channel, error) {
	return s.channelFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id = ?`, id).Scan(dest...)
	})
}

// ListChannelsVisible 用户可见渠道：自己的 + 全局共享（owner IS NULL）。
func (s *sqliteStore) ListChannelsVisible(ctx context.Context, ownerID int64) ([]*Channel, error) {
	return s.listChannelsWhere(ctx, `(owner_id = ? OR owner_id IS NULL)`, ownerID)
}

// ListChannelsAll 仅 admin：全部渠道。
func (s *sqliteStore) ListChannelsAll(ctx context.Context) ([]*Channel, error) {
	return s.listChannelsWhere(ctx, `1=1`)
}

func (s *sqliteStore) listChannelsWhere(ctx context.Context, where string, args ...any) ([]*Channel, error) {
	rows, err := s.db.query(ctx, `SELECT `+channelCols+` FROM channels WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Channel
	for rows.Next() {
		c, err := s.channelFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpdateChannel(ctx context.Context, c *Channel) error {
	storedConfig := s.encField(c.Config)
	_, err := s.db.exec(ctx, `UPDATE channels SET name = ?, type = ?, config = ?, template = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Type, storedConfig, nullString(c.Template), boolInt(c.Enabled), formatTime(time.Now()), c.ID)
	return err
}

func (s *sqliteStore) DeleteChannel(ctx context.Context, id int64) error {
	_, err := s.db.exec(ctx, `DELETE FROM channels WHERE id = ?`, id)
	return err
}

// ChannelUsageCount 渠道被多少条监控引用。
func (s *sqliteStore) ChannelUsageCount(ctx context.Context, channelID int64) (int, error) {
	var n int
	err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM monitor_channels WHERE channel_id = ?`, channelID).Scan(&n)
	return n, err
}

// ---------- 投递 ----------

func (s *sqliteStore) AddDelivery(ctx context.Context, d *Delivery) error {
	kind := d.Kind
	if kind == "" {
		kind = "update"
	}
	res, err := s.db.exec(ctx, `INSERT INTO deliveries
		(monitor_id, channel_id, change_id, payload, status, attempts, next_attempt_at, last_error, kind, context, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.MonitorID, d.ChannelID, d.ChangeID, nullString(d.Payload), string(d.Status),
		d.Attempts, nullTime(d.NextAttemptAt), nullString(d.LastError), kind, nullString(d.Context), formatTime(time.Now()))
	if err != nil {
		return err
	}
	d.ID, err = res.LastInsertId()
	return err
}

func (s *sqliteStore) UpdateDelivery(ctx context.Context, d *Delivery) error {
	kind := d.Kind
	if kind == "" {
		kind = "update"
	}
	_, err := s.db.exec(ctx, `UPDATE deliveries SET
		payload = ?, status = ?, attempts = ?, next_attempt_at = ?, last_error = ?, kind = ?, context = ?, sent_at = ?
		WHERE id = ?`,
		nullString(d.Payload), string(d.Status), d.Attempts, nullTime(d.NextAttemptAt),
		nullString(d.LastError), kind, nullString(d.Context), nullTime(d.SentAt), d.ID)
	return err
}

const deliveryCols = `id, monitor_id, channel_id, change_id, payload, status, attempts, next_attempt_at, last_error, kind, context, created_at, sent_at`

func (s *sqliteStore) deliveryFromRow(scan func(dest ...any) error) (*Delivery, error) {
	d := &Delivery{}
	var changeID, payload, nextAt, lastErr, kind, contextJSON, sentAt sql.NullString
	err := scan(&d.ID, &d.MonitorID, &d.ChannelID, &changeID, &payload, &d.Status, &d.Attempts, &nextAt, &lastErr, &kind, &contextJSON, &d.CreatedAt, &sentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Payload = payload.String
	d.LastError = lastErr.String
	d.Kind = kind.String
	if d.Kind == "" {
		d.Kind = "update"
	}
	d.Context = contextJSON.String
	if changeID.Valid && changeID.String != "" {
		var id int64
		if _, err := fmt.Sscanf(changeID.String, "%d", &id); err == nil {
			d.ChangeID = &id
		}
	}
	if nextAt.Valid && nextAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, nextAt.String); err == nil {
			d.NextAttemptAt = &t
		}
	}
	if sentAt.Valid && sentAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, sentAt.String); err == nil {
			d.SentAt = &t
		}
	}
	return d, nil
}

func (s *sqliteStore) GetDelivery(ctx context.Context, id int64) (*Delivery, error) {
	return s.deliveryFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE id = ?`, id).Scan(dest...)
	})
}

// DueDeliveries 取出到期待投递的记录（pending + deferred）。
func (s *sqliteStore) DueDeliveries(ctx context.Context, now time.Time, limit int) ([]*Delivery, error) {
	rows, err := s.db.query(ctx, `SELECT `+deliveryCols+` FROM deliveries
		WHERE status IN ('pending','deferred') AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?
		ORDER BY next_attempt_at LIMIT ?`, formatTime(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Delivery
	for rows.Next() {
		d, err := s.deliveryFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *sqliteStore) ListDeliveries(ctx context.Context, monitorID, channelID int64, status DeliveryStatus, ownerID int64, page, perPage int) ([]*Delivery, int, error) {
	where := "1=1"
	var args []any
	if monitorID > 0 {
		where += " AND monitor_id = ?"
		args = append(args, monitorID)
	}
	if channelID > 0 {
		where += " AND channel_id = ?"
		args = append(args, channelID)
	}
	if status != "" {
		where += " AND status = ?"
		args = append(args, string(status))
	}
	// 归属过滤在 SQL 层完成：COUNT 与分页都基于过滤后的集合，
	// 避免先取全局一页再在内存里删掉别人的记录导致分页错位。
	if ownerID > 0 {
		where += " AND monitor_id IN (SELECT id FROM monitors WHERE owner_id = ?)"
		args = append(args, ownerID)
	}
	var total int
	if err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM deliveries WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 25
	}
	if page <= 0 {
		page = 1
	}
	rows, err := s.db.query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, perPage, (page-1)*perPage)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Delivery
	for rows.Next() {
		d, err := s.deliveryFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// CancelOpenDeliveries 把监控未确认变更周期内的 pending/deferred 投递作废。
// 返回作废条数。用于 ack / 停用 / 手动重发场景。
func (s *sqliteStore) CancelOpenDeliveries(ctx context.Context, monitorID int64, reason string) (int64, error) {
	res, err := s.db.exec(ctx, `UPDATE deliveries SET status = 'canceled', last_error = ?
		WHERE monitor_id = ? AND status IN ('pending','deferred')
		  AND change_id IN (SELECT id FROM version_changes WHERE monitor_id = ? AND acknowledged_at IS NULL)`,
		"canceled: "+reason, monitorID, monitorID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
