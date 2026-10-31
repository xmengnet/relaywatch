package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

// ---------- 用户 ----------

func (s *sqliteStore) CreateUser(ctx context.Context, u *User) error {
	var permsArg any
	if u.Permissions != nil {
		b, _ := json.Marshal(u.Permissions)
		permsArg = string(b)
	}
	res, err := s.db.exec(ctx, `INSERT INTO users
		(username, email, display_name, password_hash, role, permissions, oidc_provider, oidc_subject, disabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, nullString(u.Email), u.DisplayName, u.PasswordHash, u.Role, permsArg,
		nullString(u.OIDCProvider), nullString(u.OIDCSubject), boolInt(u.Disabled), formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	u.ID, err = res.LastInsertId()
	return err
}

func (s *sqliteStore) userFromRow(scan func(dest ...any) error) (*User, error) {
	u := &User{}
	var passwordHash, email, displayName, oidcProvider, oidcSubject, permsJSON sql.NullString
	var lastLogin sql.NullString
	var disabled int
	err := scan(&u.ID, &u.Username, &email, &displayName, &passwordHash, &u.Role, &permsJSON,
		&oidcProvider, &oidcSubject, &disabled, &u.CreatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Email = email.String
	u.DisplayName = displayName.String
	u.PasswordHash = []byte(passwordHash.String)
	if permsJSON.Valid && permsJSON.String != "" {
		_ = json.Unmarshal([]byte(permsJSON.String), &u.Permissions)
	}
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

const userCols = `id, username, email, display_name, password_hash, role, permissions, oidc_provider, oidc_subject, disabled, created_at, last_login_at`

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

// ---------- 用户管理（M8） ----------

func (s *sqliteStore) ListUsers(ctx context.Context, f UserFilter) ([]*User, int, error) {
	where := []string{"1=1"}
	var args []any
	if f.Role != "" {
		where = append(where, "role = ?")
		args = append(args, f.Role)
	}
	if f.Search != "" {
		where = append(where, "(username LIKE ? OR email LIKE ? OR display_name LIKE ?)")
		pat := "%" + f.Search + "%"
		args = append(args, pat, pat, pat)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM users WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.PerPage <= 0 {
		f.PerPage = 25
	} else if f.PerPage > 100 {
		f.PerPage = 100
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PerPage
	rows, err := s.db.query(ctx,
		`SELECT `+userCols+` FROM users WHERE `+whereSQL+` ORDER BY id LIMIT ? OFFSET ?`,
		append(args, f.PerPage, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := s.userFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		u.PasswordHash = nil // 列表不带哈希
		out = append(out, u)
	}
	return out, int(total), rows.Err()
}

// UpdateUserProfile 更新资料字段；PasswordHash 为 nil 时不动密码。
func (s *sqliteStore) UpdateUserProfile(ctx context.Context, u *User) error {
	var hash any
	if u.PasswordHash != nil {
		hash = u.PasswordHash
	}
	var permsArg any
	if u.Permissions != nil {
		b, _ := json.Marshal(u.Permissions)
		permsArg = string(b)
	}
	_, err := s.db.exec(ctx, `UPDATE users SET
		email = ?, display_name = ?, role = ?, permissions = ?, password_hash = COALESCE(?, password_hash)
		WHERE id = ?`,
		nullString(u.Email), u.DisplayName, u.Role, permsArg, hash, u.ID)
	return err
}

func (s *sqliteStore) SetUserDisabled(ctx context.Context, id int64, disabled bool) error {
	_, err := s.db.exec(ctx, `UPDATE users SET disabled = ? WHERE id = ?`, boolInt(disabled), id)
	return err
}

func (s *sqliteStore) HasEnabledAdmin(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0`).Scan(&n)
	return n, err
}

// CountMonitorsByOwner 用户名下监控数（用户列表展示用）。
func (s *sqliteStore) CountMonitorsByOwner(ctx context.Context, ownerID int64) (int64, error) {
	var n int64
	err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM monitors WHERE owner_id = ?`, ownerID).Scan(&n)
	return n, err
}

// DeleteUserTransferResources 删除用户：监控/渠道转移给 toUserID，会话与 Token 级联删除。
func (s *sqliteStore) DeleteUserTransferResources(ctx context.Context, userID, toUserID int64) error {
	tx, err := s.db.begin(ctx)
	if err != nil {
		return err
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`UPDATE OR FAIL monitors SET owner_id = ? WHERE owner_id = ?`, []any{toUserID, userID}},
		{`UPDATE OR FAIL channels SET owner_id = ? WHERE owner_id = ?`, []any{toUserID, userID}},
		// 审计与 ack 记录保留历史，用户引用置 NULL（两列均可空且 FK 无 ON DELETE，
		// 不清掉会让最后的 DELETE FROM users 违反外键约束回滚整个事务）
		{`UPDATE audit_log SET user_id = NULL WHERE user_id = ?`, []any{userID}},
		{`UPDATE version_changes SET acknowledged_by = NULL WHERE acknowledged_by = ?`, []any{userID}},
		{`DELETE FROM sessions WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM api_tokens WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM users WHERE id = ?`, []any{userID}},
	}
	for _, st := range stmts {
		if _, err := tx.exec(ctx, st.q, st.args...); err != nil {
			tx.rollback()
			return fmt.Errorf("delete user transfer: %w", err)
		}
	}
	return tx.commit()
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

// RevokeUserSessions 吊销某用户全部未吊销且未过期的会话（禁用/重置密码用）。
func (s *sqliteStore) RevokeUserSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.exec(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?`,
		formatTime(time.Now()), userID, formatTime(time.Now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListSessionsByUser 用户全部未吊销且未过期的会话，创建时间倒序。
func (s *sqliteStore) ListSessionsByUser(ctx context.Context, userID int64) ([]*Session, error) {
	rows, err := s.db.query(ctx,
		`SELECT `+sessionCols+` FROM sessions WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
		 ORDER BY created_at DESC`, userID, formatTime(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		sess, err := s.sessionFromRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		sess.CSRFToken = "" // 列表不回传 CSRF token
		out = append(out, sess)
	}
	return out, rows.Err()
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

const monitorCols = `id, owner_id, name, source_type, source_config, version_prefix, version_suffix,
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
	err := scan(&m.ID, &m.OwnerID, &m.Name, &m.SourceType, &m.SourceConfig, &prefix, &suffix,
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
		(owner_id, name, source_type, source_config, version_prefix, version_suffix,
		 version_include_regex, version_ignore_regex, from_pattern, to_pattern, interval_sec, enabled, state,
		 current_version, latest_version, notify_cooldown_sec, tags, note, reference_monitor_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.OwnerID, m.Name, m.SourceType, storedConfig, nullString(m.VersionPrefix), nullString(m.VersionSuffix),
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
	if f.OwnerID > 0 {
		where = append(where, "owner_id = ?")
		args = append(args, f.OwnerID)
	}
	if f.State != "" {
		where = append(where, "state = ?")
		args = append(args, string(f.State))
	}
	if f.CompareState != "" {
		where = append(where, "compare_state = ?")
		args = append(args, f.CompareState)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM json_each(CASE WHEN tags IS NULL THEN '[]' ELSE tags END) WHERE json_each.value = ?)`)
		args = append(args, f.Tag)
	}
	whereSQL := strings.Join(where, " AND ")

	// 搜索不走 SQL LIKE：source_config 落库为密文（生产启用 secret_key），对密文
	// LIKE 永远搜不到。改为取回候选行后按名称与解密后的 config 内存过滤。
	if f.Search != "" {
		return s.listMonitorsSearch(ctx, whereSQL, args, f)
	}

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
	} else if f.PerPage > 100 {
		f.PerPage = 100
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
	s.fillMonitorChannels(ctx, out)
	return out, total, nil
}

// listMonitorsSearch 搜索路径：内存过滤（名称 + 解密后的 source_config，不区分
// 大小写、无通配符语义）+ 内存排序分页。监控量为个人部署规模，全量取回可接受；
// 过滤在分页之前完成，避免「先分页后过滤」的数量错位。
func (s *sqliteStore) listMonitorsSearch(ctx context.Context, whereSQL string, args []any, f MonitorFilter) ([]*Monitor, int, error) {
	rows, err := s.db.query(ctx, `SELECT `+monitorCols+` FROM monitors WHERE `+whereSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var all []*Monitor
	for rows.Next() {
		m, err := s.monitorFromRow(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		all = append(all, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	q := strings.ToLower(f.Search)
	filtered := make([]*Monitor, 0, len(all))
	for _, m := range all {
		if strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(strings.ToLower(m.SourceConfig), q) {
			filtered = append(filtered, m)
		}
	}
	total := len(filtered)
	sortMonitorsInMemory(filtered, f.SortBy, f.SortOrder)

	perPage := f.PerPage
	if perPage <= 0 {
		perPage = 25
	} else if perPage > 100 {
		perPage = 100
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * perPage
	var out []*Monitor
	switch {
	case offset >= len(filtered):
	case offset+perPage >= len(filtered):
		out = filtered[offset:]
	default:
		out = filtered[offset : offset+perPage]
	}
	s.fillMonitorChannels(ctx, out)
	return out, total, nil
}

// sortMonitorsInMemory 与 SQL 排序语义一致（白名单键，缺省 id DESC）。
func sortMonitorsInMemory(ms []*Monitor, sortBy, sortOrder string) {
	asc := strings.EqualFold(sortOrder, "asc")
	less := func(i, j int) bool { return ms[i].ID > ms[j].ID } // 缺省 id DESC
	switch sortBy {
	case "name":
		less = func(i, j int) bool {
			return ms[i].Name != ms[j].Name && (ms[i].Name < ms[j].Name) == asc
		}
	case "source_type":
		less = func(i, j int) bool {
			return ms[i].SourceType != ms[j].SourceType && (ms[i].SourceType < ms[j].SourceType) == asc
		}
	case "state":
		less = func(i, j int) bool {
			return ms[i].State != ms[j].State && (ms[i].State < ms[j].State) == asc
		}
	case "last_change_at":
		less = monitorTimeLess(ms, asc, func(m *Monitor) *time.Time { return m.LastChangeAt })
	case "last_checked_at":
		less = monitorTimeLess(ms, asc, func(m *Monitor) *time.Time { return m.LastCheckedAt })
	case "created_at":
		less = func(i, j int) bool {
			return !ms[i].CreatedAt.Equal(ms[j].CreatedAt) && ms[i].CreatedAt.Before(ms[j].CreatedAt) == asc
		}
	}
	sort.SliceStable(ms, less)
}

// monitorTimeLess 时间键排序；nil（从未检查/变更）排最后。
func monitorTimeLess(ms []*Monitor, asc bool, get func(*Monitor) *time.Time) func(i, j int) bool {
	return func(i, j int) bool {
		a, b := get(ms[i]), get(ms[j])
		switch {
		case a == nil && b == nil:
			return false
		case a == nil:
			return false
		case b == nil:
			return true
		}
		return !a.Equal(*b) && a.Before(*b) == asc
	}
}

// fillMonitorChannels 批量补齐渠道绑定。
func (s *sqliteStore) fillMonitorChannels(ctx context.Context, out []*Monitor) {
	if len(out) == 0 {
		return
	}
	ids := make([]string, 0, len(out))
	for _, m := range out {
		ids = append(ids, fmt.Sprint(m.ID))
	}
	rows2, err := s.db.query(ctx,
		`SELECT monitor_id, channel_id FROM monitor_channels WHERE monitor_id IN (`+strings.Join(ids, ",")+`)`)
	if err != nil {
		return
	}
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

// CountEnabledMonitors 启用的监控数（metrics 用，避免取回全部行）。
func (s *sqliteStore) CountEnabledMonitors(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.queryRow(ctx, `SELECT COUNT(*) FROM monitors WHERE enabled = 1`).Scan(&n)
	return n, err
}

// CountMonitorStates 一次 GROUP BY 完成仪表盘计数，避免按状态重复查询。
func (s *sqliteStore) CountMonitorStates(ctx context.Context, ownerID int64) (MonitorCounts, error) {
	where := "1=1"
	var args []any
	if ownerID > 0 {
		where = "owner_id = ?"
		args = append(args, ownerID)
	}
	rows, err := s.db.query(ctx,
		`SELECT state, compare_state, COUNT(*) FROM monitors WHERE `+where+` GROUP BY state, compare_state`, args...)
	if err != nil {
		return MonitorCounts{}, err
	}
	var counts MonitorCounts
	for rows.Next() {
		var state, compareState sql.NullString
		var n int
		if err := rows.Scan(&state, &compareState, &n); err != nil {
			rows.Close()
			return MonitorCounts{}, err
		}
		counts.Total += n
		switch MonitorState(state.String) {
		case StateUpToDate:
			counts.UpToDate += n
		case StateHasUpdate:
			counts.HasUpdate += n
		case StateError:
			counts.Error += n
		case StateMissing:
			counts.Missing += n
		case StateUnknown:
			counts.Unknown += n
		}
		if compareState.String == "lagging" {
			counts.Lagging += n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MonitorCounts{}, err
	}
	rows.Close()
	return counts, nil
}

// UpdateMonitor 更新监控的配置字段。不写 state/current_version/latest_version：
// 这些列由 Checker 增量回写，整行覆盖会丢掉并发检查刚落库的结果；
// 显式改基线走 SetMonitorBaseline。
func (s *sqliteStore) UpdateMonitor(ctx context.Context, m *Monitor) error {
	tagsJSON, _ := json.Marshal(m.Tags)
	storedConfig := s.encField(m.SourceConfig)
	_, err := s.db.exec(ctx, `UPDATE monitors SET
			name = ?, source_type = ?, source_config = ?, version_prefix = ?, version_suffix = ?,
			version_include_regex = ?, version_ignore_regex = ?, from_pattern = ?, to_pattern = ?,
			interval_sec = ?, enabled = ?, notify_cooldown_sec = ?, tags = ?, note = ?,
			reference_monitor_id = ?, updated_at = ?
			WHERE id = ?`,
		m.Name, m.SourceType, storedConfig, nullString(m.VersionPrefix), nullString(m.VersionSuffix),
		nullString(m.VersionIncludeRegex), nullString(m.VersionIgnoreRegex), nullString(m.FromPattern), nullString(m.ToPattern),
		m.IntervalSec, boolInt(m.Enabled), m.NotifyCooldownSec, string(tagsJSON), nullString(m.Note),
		m.ReferenceMonitorID, formatTime(time.Now()), m.ID)
	if err != nil {
		return err
	}
	return s.SetMonitorChannels(ctx, m.ID, m.ChannelIDs)
}

// SetMonitorEnabled 批量启停用：只动 enabled 列。
func (s *sqliteStore) SetMonitorEnabled(ctx context.Context, id int64, enabled bool) error {
	_, err := s.db.exec(ctx, `UPDATE monitors SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolInt(enabled), formatTime(time.Now()), id)
	return err
}

// SetMonitorBaseline 显式改基线（手动改 current_version/state、ack 提基线）。
// 只更新非 nil 的列。
func (s *sqliteStore) SetMonitorBaseline(ctx context.Context, id int64, state *MonitorState, currentVersion *string) error {
	if state == nil && currentVersion == nil {
		return nil
	}
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(time.Now())}
	if state != nil {
		sets = append(sets, "state = ?")
		args = append(args, string(*state))
	}
	if currentVersion != nil {
		sets = append(sets, "current_version = ?")
		args = append(args, nullString(*currentVersion))
	}
	args = append(args, id)
	_, err := s.db.exec(ctx, `UPDATE monitors SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// TouchMonitorNotified 投递成功后更新静默期基准，只动 last_notified_at。
func (s *sqliteStore) TouchMonitorNotified(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.exec(ctx, `UPDATE monitors SET last_notified_at = ?, updated_at = ? WHERE id = ?`,
		formatTime(at), formatTime(time.Now()), id)
	return err
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

// SetMonitorOwner 用户删除转移资源用；管理员改归属也可用。
func (s *sqliteStore) SetMonitorOwner(ctx context.Context, monitorID, ownerID int64) error {
	_, err := s.db.exec(ctx, `UPDATE monitors SET owner_id = ?, updated_at = ? WHERE id = ?`,
		ownerID, formatTime(time.Now()), monitorID)
	return err
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
