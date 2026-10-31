package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// OIDCProvider 一个外部登录源。
type OIDCProvider struct {
	ID           int64
	Name         string // 显示名与 URL 标识，如 "github"
	Issuer       string
	ClientID     string
	ClientSecret string // 存库前已加密
	Scopes       string
	ClaimRules   string // JSON：{"email_domains":[...],"groups":{"claim":"groups","any_of":[...]}}
	IconURL      string // 登录页按钮图标；空 = 按内置 name 映射
	Enabled      bool
	CreatedAt    time.Time
}

// ---------- OIDC Provider ----------

const oidcProviderCols = `id, name, issuer, client_id, client_secret, scopes, claim_rules, icon_url, enabled, created_at`

func (s *sqliteStore) oidcProviderFromRow(scan func(dest ...any) error) (*OIDCProvider, error) {
	p := &OIDCProvider{}
	var rules sql.NullString
	var enabled int
	err := scan(&p.ID, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.Scopes, &rules, &p.IconURL, &enabled, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.ClaimRules = rules.String
	p.Enabled = enabled != 0
	p.ClientSecret = s.decField(p.ClientSecret)
	return p, nil
}

func (s *sqliteStore) CreateOIDCProvider(ctx context.Context, p *OIDCProvider) error {
	storedSecret := s.encField(p.ClientSecret)
	res, err := s.db.exec(ctx, `INSERT INTO oidc_providers (name, issuer, client_id, client_secret, scopes, claim_rules, icon_url, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.Issuer, p.ClientID, storedSecret, p.Scopes, nullString(p.ClaimRules), p.IconURL, boolInt(p.Enabled), formatTime(time.Now()))
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

func (s *sqliteStore) ListOIDCProviders(ctx context.Context) ([]*OIDCProvider, error) {
	rows, err := s.db.query(ctx, `SELECT `+oidcProviderCols+` FROM oidc_providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OIDCProvider
	for rows.Next() {
		p, err := s.oidcProviderFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetOIDCProvider(ctx context.Context, id int64) (*OIDCProvider, error) {
	return s.oidcProviderFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+oidcProviderCols+` FROM oidc_providers WHERE id = ?`, id).Scan(dest...)
	})
}

func (s *sqliteStore) OIDCProviderByName(ctx context.Context, name string) (*OIDCProvider, error) {
	return s.oidcProviderFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+oidcProviderCols+` FROM oidc_providers WHERE name = ?`, name).Scan(dest...)
	})
}

func (s *sqliteStore) UpdateOIDCProvider(ctx context.Context, p *OIDCProvider) error {
	storedSecret := s.encField(p.ClientSecret)
	_, err := s.db.exec(ctx, `UPDATE oidc_providers SET name = ?, issuer = ?, client_id = ?, client_secret = ?, scopes = ?, claim_rules = ?, icon_url = ?, enabled = ? WHERE id = ?`,
		p.Name, p.Issuer, p.ClientID, storedSecret, p.Scopes, nullString(p.ClaimRules), p.IconURL, boolInt(p.Enabled), p.ID)
	return err
}

func (s *sqliteStore) DeleteOIDCProvider(ctx context.Context, id int64) error {
	_, err := s.db.exec(ctx, `DELETE FROM oidc_providers WHERE id = ?`, id)
	return err
}

// UserByOIDC 按 (provider, subject) 查用户。
func (s *sqliteStore) UserByOIDC(ctx context.Context, provider, subject string) (*User, error) {
	return s.userFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+userCols+` FROM users WHERE oidc_provider = ? AND oidc_subject = ?`, provider, subject).Scan(dest...)
	})
}

// SetUserOIDC 绑定 OIDC 身份（「个人资料」绑定时用）。
// 唯一性由 users 表的 UNIQUE (oidc_provider, oidc_subject) 兜底；
// 用户不存在（绑定期间被删除）返回 ErrNotFound，而非静默“成功”。
func (s *sqliteStore) SetUserOIDC(ctx context.Context, userID int64, provider, subject string) error {
	res, err := s.db.exec(ctx, `UPDATE users SET oidc_provider = ?, oidc_subject = ? WHERE id = ?`, provider, subject, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UnbindOIDCUser 解绑 OIDC 身份。置 NULL 而非空串：空串是非 NULL 值，
// 两个未绑定用户都会落 (” , ”) 而违反 UNIQUE 约束，NULL 互不冲突。
func (s *sqliteStore) UnbindOIDCUser(ctx context.Context, userID int64) error {
	_, err := s.db.exec(ctx, `UPDATE users SET oidc_provider = NULL, oidc_subject = NULL WHERE id = ?`, userID)
	return err
}
