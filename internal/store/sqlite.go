package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

// sqlDB 封装 *sql.DB，提供带 ctx 的小工具，隔离 database/sql 样板。
type sqlDB struct {
	raw *sql.DB
}

func connect(dsn string) (*sqlDB, error) {
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// modernc/sqlite 写并发受限，串行化写入由应用层保证，这里限制连接数避免 SQLITE_BUSY
	raw.SetMaxOpenConns(1)
	return &sqlDB{raw: raw}, nil
}

func (d *sqlDB) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.raw.ExecContext(ctx, query, args...)
}

func (d *sqlDB) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.raw.QueryContext(ctx, query, args...)
}

func (d *sqlDB) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.raw.QueryRowContext(ctx, query, args...)
}

func (d *sqlDB) begin(ctx context.Context) (*sqlTx, error) {
	tx, err := d.raw.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqlTx{tx: tx}, nil
}

func (d *sqlDB) close() error { return d.raw.Close() }

// SetSecretBox 注入加解密器。
func (s *sqliteStore) SetSecretBox(box SecretBox) { s.box = box }

// encField 落库前加密；box 未配置时原样。
func (s *sqliteStore) encField(plain string) string {
	if s.box == nil || plain == "" {
		return plain
	}
	enc, err := s.box.Encrypt(plain)
	if err != nil {
		// 加密失败回退明文必须留痕：静默降级等于加密形同虚设且无人知晓
		slog.Error("encField encrypt failed, storing plaintext", "err", err)
		return plain
	}
	return enc
}

// decField 读出后解密；box 未配置或非密文原样。
func (s *sqliteStore) decField(s2 string) string {
	if s.box == nil || s2 == "" {
		return s2
	}
	plain, err := s.box.Decrypt(s2)
	if err != nil {
		return s2
	}
	return plain
}

type sqlTx struct{ tx *sql.Tx }

func (t *sqlTx) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}
func (t *sqlTx) commit() error   { return t.tx.Commit() }
func (t *sqlTx) rollback() error { return t.tx.Rollback() }

// 时间列统一存 UTC RFC3339 文本，读取时容忍空值。
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
