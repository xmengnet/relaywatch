package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// RegistrationCode 一条待消费的注册邮箱验证码。
type RegistrationCode struct {
	Email     string
	Code      string
	ExpiresAt time.Time
	SentAt    time.Time
}

// PutRegistrationCode 写入/覆盖某邮箱的验证码。
// expiredToZero 为 true 时跳过过期清理（测试用：直接写入一条已过期记录）。
func (s *sqliteStore) PutRegistrationCode(ctx context.Context, email, code string, expiresAt, sentAt time.Time) error {
	_, err := s.db.exec(ctx,
		`INSERT INTO registration_codes (email, code, expires_at, sent_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(email) DO UPDATE SET
		   code = excluded.code, expires_at = excluded.expires_at, sent_at = excluded.sent_at`,
		strings.ToLower(email), code, formatTime(expiresAt), formatTime(sentAt))
	return err
}

// SweepRegistrationCodes 清理已过期的验证码记录（写入路径调用）。
func (s *sqliteStore) SweepRegistrationCodes(ctx context.Context) error {
	_, err := s.db.exec(ctx, `DELETE FROM registration_codes WHERE expires_at < ?`, formatTime(time.Now()))
	return err
}

// GetRegistrationCode 按邮箱读取验证码；不存在时返回 ok=false。
func (s *sqliteStore) GetRegistrationCode(ctx context.Context, email string) (*RegistrationCode, bool, error) {
	var c RegistrationCode
	err := s.db.queryRow(ctx,
		`SELECT email, code, expires_at, sent_at FROM registration_codes WHERE email = ?`,
		strings.ToLower(email)).Scan(&c.Email, &c.Code, &c.ExpiresAt, &c.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// ConsumeRegistrationCode 原子消费：只有邮箱、验证码都匹配且未过期时才删除。
// 返回 true 表示本次调用成功消费；并发重复提交只有一次会成功。
func (s *sqliteStore) ConsumeRegistrationCode(ctx context.Context, email, code string, now time.Time) (bool, error) {
	res, err := s.db.exec(ctx,
		`DELETE FROM registration_codes WHERE email = ? AND code = ? AND expires_at > ?`,
		strings.ToLower(email), code, formatTime(now))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
