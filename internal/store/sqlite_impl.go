package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"database/sql"
)

var ErrNotFound = errors.New("not found")

// ---------- 用户 ----------

func (s *sqliteStore) CreateUser(ctx context.Context, u *User) error {
	res, err := s.db.exec(ctx, `INSERT INTO users
		(username, email, password_hash, role, oidc_provider, oidc_subject, disabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, nullString(u.Email), u.PasswordHash, u.Role,
		nullString(u.OIDCProvider), nullString(u.OIDCSubject), boolInt(u.Disabled), formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	u.ID, err = res.LastInsertId()
	return err
}

func (s *sqliteStore) userFromRow(scan func(dest ...any) error) (*User, error) {
	u := &User{}
	var passwordHash, email, oidcProvider, oidcSubject sql.NullString
	var lastLogin sql.NullString
	var disabled int
	err := scan(&u.ID, &u.Username, &email, &passwordHash, &u.Role,
		&oidcProvider, &oidcSubject, &disabled, &u.CreatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.PasswordHash = []byte(passwordHash.String)
	u.OIDCProvider = oidcProvider.String
	u.OIDCSubject = oidcSubject.String
	u.Disabled = disabled != 0
	if lastLogin.Valid && lastLogin.String != "" {
		t, err := time.Parse(time.RFC3339Nano, lastLogin.String)
		if err == nil {
			u.LastLoginAt = &t
		}
	}
	return u, nil
}

const userCols = `id, username, email, password_hash, role, oidc_provider, oidc_subject, disabled, created_at, last_login_at`

func (s *sqliteStore) UserByUsername(ctx context.Context, name string) (*User, error) {
	return s.userFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, name).Scan(dest...)
	})
}

func (s *sqliteStore) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.userFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id).Scan(dest...)
	})
}

func (s *sqliteStore) UserCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *sqliteStore) UpdateUserPassword(ctx context.Context, id int64, hash []byte) error {
	_, err := s.db.exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *sqliteStore) UpdateUserLogin(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.exec(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, formatTime(at), id)
	return err
}

// ---------- 会话 ----------

func (s *sqliteStore) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.db.exec(ctx, `INSERT INTO sessions
		(id, user_id, csrf_token, user_agent, ip, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.CSRFToken, sess.UserAgent, sess.IP,
		formatTime(sess.CreatedAt), formatTime(sess.ExpiresAt))
	return err
}

func (s *sqliteStore) sessionFromRow(scan func(dest ...any) error) (*Session, error) {
	sess := &Session{}
	var expires, created, revoked sql.NullString
	err := scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.UserAgent, &sess.IP, &created, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
	sess.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires.String)
	if revoked.Valid && revoked.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, revoked.String)
		sess.RevokedAt = &t
	}
	return sess, nil
}

const sessionCols = `id, user_id, csrf_token, user_agent, ip, created_at, expires_at, revoked_at`

func (s *sqliteStore) SessionByID(ctx context.Context, id string) (*Session, error) {
	return s.sessionFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id).Scan(dest...)
	})
}

func (s *sqliteStore) RevokeSession(ctx context.Context, id string) error {
	_, err := s.db.exec(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ?`, formatTime(time.Now()), id)
	return err
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.exec(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- API Token ----------

func (s *sqliteStore) CreateAPIToken(ctx context.Context, t *APIToken) error {
	res, err := s.db.exec(ctx, `INSERT INTO api_tokens
		(user_id, name, token_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		t.UserID, t.Name, t.TokenHash, formatTime(t.CreatedAt), nullTime(t.ExpiresAt))
	if err != nil {
		return err
	}
	t.ID, err = res.LastInsertId()
	return err
}

const apiTokenCols = `id, user_id, name, token_hash, created_at, expires_at, last_used_at, revoked_at`

func (s *sqliteStore) apiTokenFromRow(scan func(dest ...any) error) (*APIToken, error) {
	t := &APIToken{}
	var expires, lastUsed, revoked sql.NullString
	err := scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.CreatedAt, &expires, &lastUsed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if expires.Valid && expires.String != "" {
		if v, err := time.Parse(time.RFC3339Nano, expires.String); err == nil {
			t.ExpiresAt = &v
		}
	}
	if lastUsed.Valid && lastUsed.String != "" {
		if v, err := time.Parse(time.RFC3339Nano, lastUsed.String); err == nil {
			t.LastUsedAt = &v
		}
	}
	if revoked.Valid && revoked.String != "" {
		if v, err := time.Parse(time.RFC3339Nano, revoked.String); err == nil {
			t.RevokedAt = &v
		}
	}
	return t, nil
}

func (s *sqliteStore) APITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	return s.apiTokenFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE token_hash = ?`, hash).Scan(dest...)
	})
}

func (s *sqliteStore) ListAPITokens(ctx context.Context, userID int64) ([]*APIToken, error) {
	rows, err := s.db.query(ctx, `SELECT `+apiTokenCols+` FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIToken
	for rows.Next() {
		t, err := s.apiTokenFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqliteStore) RevokeAPIToken(ctx context.Context, id int64) error {
	_, err := s.db.exec(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE id = ?`, formatTime(time.Now()), id)
	return err
}

func (s *sqliteStore) TouchAPIToken(ctx context.Context, id int64) error {
	_, err := s.db.exec(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, formatTime(time.Now()), id)
	return err
}

// ---------- 监控 ----------

const monitorCols = `id, name, source_type, source_config, version_prefix, version_suffix,
	version_include_regex, version_ignore_regex, from_pattern, to_pattern, interval_sec, enabled, state,
	current_version, latest_version, last_checked_at, last_success_at, last_change_at,
	last_error, consecutive_failures, notify_cooldown_sec, last_notified_at,
	tags, note, created_at, updated_at, reference_monitor_id, compare_state, compare_ref_version, compare_since, compare_notified_ref`

func (s *sqliteStore) monitorFromRow(scan func(dest ...any) error) (*Monitor, error) {
	m := &Monitor{}
	var prefix, suffix, incRe, ignRe, fromPat, toPat, current, latest, lastErr, tags, note sql.NullString
	var enabled int
	var lastChecked, lastSuccess, lastChange, lastNotified sql.NullString
	var state string
	var refID sql.NullInt64
	var cmpState, cmpRefVer, cmpSince, cmpNotifiedRef sql.NullString
	err := scan(&m.ID, &m.Name, &m.SourceType, &m.SourceConfig, &prefix, &suffix,
		&incRe, &ignRe, &fromPat, &toPat, &m.IntervalSec, &enabled, &state,
		&current, &latest, &lastChecked, &lastSuccess, &lastChange,
		&lastErr, &m.ConsecutiveFailures, &m.NotifyCooldownSec, &lastNotified,
		&tags, &note, &m.CreatedAt, &m.UpdatedAt,
		&refID, &cmpState, &cmpRefVer, &cmpSince, &cmpNotifiedRef)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.VersionPrefix = prefix.String
	m.VersionSuffix = suffix.String
	m.VersionIncludeRegex = incRe.String
	m.VersionIgnoreRegex = ignRe.String
	m.FromPattern = fromPat.String
	m.ToPattern = toPat.String
	m.Enabled = enabled != 0
	m.State = MonitorState(state)
	m.SourceConfig = s.decField(m.SourceConfig)
	m.CurrentVersion = current.String
	m.LatestVersion = latest.String
	m.LastError = lastErr.String
	m.Note = note.String
	if refID.Valid {
		idVal := refID.Int64
		m.ReferenceMonitorID = &idVal
	}
	m.CompareState = cmpState.String
	m.CompareRefVersion = cmpRefVer.String
	m.CompareNotifiedRef = cmpNotifiedRef.String
	if cmpSince.Valid && cmpSince.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, cmpSince.String); err == nil {
			m.CompareSince = &t
		}
	}
	for _, p := range []*sql.NullString{&lastChecked, &lastSuccess, &lastChange, &lastNotified} {
		if p.Valid && p.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, p.String); err == nil {
				switch p {
				case &lastChecked:
					m.LastCheckedAt = &t
				case &lastSuccess:
					m.LastSuccessAt = &t
				case &lastChange:
					m.LastChangeAt = &t
				case &lastNotified:
					m.LastNotifiedAt = &t
				}
			}
		}
	}
	if tags.Valid && tags.String != "" {
		_ = json.Unmarshal([]byte(tags.String), &m.Tags)
	}
	return m, nil
}

func (s *sqliteStore) CreateMonitor(ctx context.Context, m *Monitor) error {
	tagsJSON, _ := json.Marshal(m.Tags)
	now := time.Now()
	storedConfig := s.encField(m.SourceConfig)
	res, err := s.db.exec(ctx, `INSERT INTO monitors
		(name, source_type, source_config, version_prefix, version_suffix,
		 version_include_regex, version_ignore_regex, from_pattern, to_pattern, interval_sec, enabled, state,
		 current_version, latest_version, notify_cooldown_sec, tags, note, reference_monitor_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.SourceType, storedConfig, nullString(m.VersionPrefix), nullString(m.VersionSuffix),
		nullString(m.VersionIncludeRegex), nullString(m.VersionIgnoreRegex), nullString(m.FromPattern), nullString(m.ToPattern),
		m.IntervalSec, boolInt(m.Enabled), string(m.State),
		nullString(m.CurrentVersion), nullString(m.LatestVersion), m.NotifyCooldownSec, string(tagsJSON), nullString(m.Note),
		m.ReferenceMonitorID, formatTime(now), formatTime(now))
	if err != nil {
		return err
	}
	m.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	if len(m.ChannelIDs) > 0 {
		return s.SetMonitorChannels(ctx, m.ID, m.ChannelIDs)
	}
	return nil
}

func (s *sqliteStore) GetMonitor(ctx context.Context, id int64) (*Monitor, error) {
	m, err := s.monitorFromRow(func(dest ...any) error {
		return s.db.queryRow(ctx, `SELECT `+monitorCols+` FROM monitors WHERE id = ?`, id).Scan(dest...)
	})
	if err != nil {
		return nil, err
	}
	m.ChannelIDs, err = s.monitorChannelIDs(ctx, id)
	return m, err
}

func (s *sqliteStore) monitorChannelIDs(ctx context.Context, monitorID int64) ([]int64, error) {
	rows, err := s.db.query(ctx, `SELECT channel_id FROM monitor_channels WHERE monitor_id = ?`, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *sqliteStore) ListMonitors(ctx context.Context, f MonitorFilter) ([]*Monitor, int, error) {
	where := []string{"1=1"}
	var args []any
	if f.State != "" {
		where = append(where, "state = ?")
		args = append(args, string(f.State))
	}
	if f.CompareState != "" {
		where = append(where, "compare_state = ?")
		args = append(args, f.CompareState)
	}
	if f.Search != "" {
		where = append(where, "(name LIKE ? OR source_config LIKE ?)")
		pat := "%" + f.Search + "%"
		args = append(args, pat, pat)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM json_each(CASE WHEN tags IS NULL THEN '[]' ELSE tags END) WHERE json_each.value = ?)`)
		args = append(args, f.Tag)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM monitors WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := `id DESC`
	sortMap := map[string]bool{"name": true, "source_type": true, "state": true, "last_change_at": true, "last_checked_at": true, "created_at": true}
	if sortMap[f.SortBy] {
		dir := "DESC"
		if strings.EqualFold(f.SortOrder, "asc") {
			dir = "ASC"
		}
		order = f.SortBy + " " + dir
	}
	if f.PerPage <= 0 {
		f.PerPage = 25
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PerPage

	rows, err := s.db.query(ctx,
		`SELECT `+monitorCols+` FROM monitors WHERE `+whereSQL+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, f.PerPage, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*Monitor
	for rows.Next() {
		m, err := s.monitorFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	// 批量补齐渠道绑定
	if len(out) > 0 {
		ids := make([]string, 0, len(out))
		for _, m := range out {
			ids = append(ids, fmt.Sprint(m.ID))
		}
		rows2, err := s.db.query(ctx,
			`SELECT monitor_id, channel_id FROM monitor_channels WHERE monitor_id IN (`+strings.Join(ids, ",")+`)`)
		if err == nil {
			defer rows2.Close()
			byID := map[int64][]int64{}
			for rows2.Next() {
				var mid, cid int64
				if rows2.Scan(&mid, &cid) == nil {
					byID[mid] = append(byID[mid], cid)
				}
			}
			for _, m := range out {
				m.ChannelIDs = byID[m.ID]
			}
		}
	}
	return out, total, nil
}

	func (s *sqliteStore) UpdateMonitor(ctx context.Context, m *Monitor) error {
		tagsJSON, _ := json.Marshal(m.Tags)
		storedConfig := s.encField(m.SourceConfig)
		_, err := s.db.exec(ctx, `UPDATE monitors SET
			name = ?, source_type = ?, source_config = ?, version_prefix = ?, version_suffix = ?,
			version_include_regex = ?, version_ignore_regex = ?, from_pattern = ?, to_pattern = ?,
			interval_sec = ?, enabled = ?, state = ?,
			current_version = ?, latest_version = ?, notify_cooldown_sec = ?, tags = ?, note = ?,
			reference_monitor_id = ?, updated_at = ?
			WHERE id = ?`,
			m.Name, m.SourceType, storedConfig, nullString(m.VersionPrefix), nullString(m.VersionSuffix),
			nullString(m.VersionIncludeRegex), nullString(m.VersionIgnoreRegex), nullString(m.FromPattern), nullString(m.ToPattern),
			m.IntervalSec, boolInt(m.Enabled), string(m.State),
			nullString(m.CurrentVersion), nullString(m.LatestVersion), m.NotifyCooldownSec, string(tagsJSON), nullString(m.Note),
			m.ReferenceMonitorID, formatTime(time.Now()), m.ID)
		if err != nil {
			return err
		}
		return s.SetMonitorChannels(ctx, m.ID, m.ChannelIDs)
	}

func (s *sqliteStore) UpdateMonitorCompare(ctx context.Context, id int64, compareState, compareRefVersion string, compareSince *time.Time, compareNotifiedRef string) error {
	_, err := s.db.exec(ctx, `UPDATE monitors SET
		compare_state = ?, compare_ref_version = ?, compare_since = ?, compare_notified_ref = ?, updated_at = ?
		WHERE id = ?`,
		compareState, compareRefVersion, nullTime(compareSince), compareNotifiedRef, formatTime(time.Now()), id)
	return err
}

func (s *sqliteStore) ListFollowerMonitors(ctx context.Context, refMonitorID int64) ([]*Monitor, error) {
	rows, err := s.db.query(ctx, `SELECT `+monitorCols+` FROM monitors WHERE reference_monitor_id = ?`, refMonitorID)
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

func (s *sqliteStore) ClearFollowerReferences(ctx context.Context, refMonitorID int64) error {
	_, err := s.db.exec(ctx, `UPDATE monitors SET
		reference_monitor_id = NULL, compare_state = '', compare_ref_version = '', compare_since = NULL, compare_notified_ref = '', updated_at = ?
		WHERE reference_monitor_id = ?`, formatTime(time.Now()), refMonitorID)
	return err
}

func (s *sqliteStore) DeleteMonitor(ctx context.Context, id int64) error {
	_, err := s.db.exec(ctx, `DELETE FROM monitors WHERE id = ?`, id)
	return err
}

func (s *sqliteStore) SetMonitorChannels(ctx context.Context, monitorID int64, channelIDs []int64) error {
	tx, err := s.db.begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.exec(ctx, `DELETE FROM monitor_channels WHERE monitor_id = ?`, monitorID); err != nil {
		tx.rollback()
		return err
	}
	for _, cid := range channelIDs {
		if _, err := tx.exec(ctx, `INSERT INTO monitor_channels (monitor_id, channel_id) VALUES (?, ?)`, monitorID, cid); err != nil {
			tx.rollback()
			return err
		}
	}
	return tx.commit()
}

// ---------- 审计 ----------

func (s *sqliteStore) AddAudit(ctx context.Context, a *AuditEntry) error {
	_, err := s.db.exec(ctx, `INSERT INTO audit_log (user_id, action, target, detail, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.UserID, a.Action, nullString(a.Target), nullString(a.Detail), nullString(a.IP), formatTime(time.Now()))
	return err
}

// ---------- 其它 ----------

func (s *sqliteStore) Close() error { return s.db.close() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
