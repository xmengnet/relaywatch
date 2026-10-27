package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ---------- 系统设置 ----------

func (s *sqliteStore) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.queryRow(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *sqliteStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.exec(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(time.Now()))
	return err
}
