-- 用户
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    email         TEXT,
    password_hash TEXT,
    role          TEXT    NOT NULL DEFAULT 'viewer',
    oidc_provider TEXT,
    oidc_subject  TEXT,
    totp_secret   TEXT,
    disabled      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    last_login_at DATETIME,
    UNIQUE (oidc_provider, oidc_subject)
);

-- 会话
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    user_agent TEXT,
    ip         TEXT,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- API Token
CREATE TABLE api_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL,
    expires_at  DATETIME,
    last_used_at DATETIME,
    revoked_at  DATETIME
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

-- 监控记录
CREATE TABLE monitors (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    source_type    TEXT NOT NULL,
    source_config  TEXT NOT NULL,
    version_prefix TEXT,
    version_suffix TEXT,
    version_include_regex TEXT,
    version_ignore_regex  TEXT,
    interval_sec   INTEGER NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    state          TEXT NOT NULL DEFAULT 'unknown',
    current_version TEXT,
    latest_version  TEXT,
    last_checked_at DATETIME,
    last_success_at DATETIME,
    last_change_at  DATETIME,
    last_error      TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    notify_cooldown_sec  INTEGER NOT NULL DEFAULT 0,
    last_notified_at     DATETIME,
    tags           TEXT,
    note           TEXT,
    created_at     DATETIME NOT NULL,
    updated_at     DATETIME NOT NULL
);
CREATE INDEX idx_monitors_state ON monitors(state);
CREATE INDEX idx_monitors_enabled ON monitors(enabled);

-- 监控 ↔ 渠道
CREATE TABLE monitor_channels (
    monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    PRIMARY KEY (monitor_id, channel_id)
);

-- 通知渠道（M3 使用，先建表保证外键完整）
CREATE TABLE channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL,
    config     TEXT NOT NULL,
    template   TEXT,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- 检查历史
CREATE TABLE check_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    checked_at  DATETIME NOT NULL,
    duration_ms INTEGER NOT NULL,
    ok          INTEGER NOT NULL,
    version     TEXT,
    error       TEXT
);
CREATE INDEX idx_check_history_monitor ON check_history(monitor_id, checked_at DESC);

-- 版本变更历史
CREATE TABLE version_changes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    old_version TEXT,
    new_version TEXT NOT NULL,
    detected_at DATETIME NOT NULL,
    acknowledged_at DATETIME,
    acknowledged_by INTEGER REFERENCES users(id)
);
CREATE INDEX idx_version_changes_monitor ON version_changes(monitor_id, detected_at DESC);

-- 通知投递记录
CREATE TABLE deliveries (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    monitor_id   INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    channel_id   INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    change_id    INTEGER REFERENCES version_changes(id) ON DELETE SET NULL,
    payload      TEXT,
    status       TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    next_attempt_at DATETIME,
    last_error   TEXT,
    created_at   DATETIME NOT NULL,
    sent_at      DATETIME
);
CREATE INDEX idx_deliveries_pending ON deliveries(status, next_attempt_at);

-- 审计日志
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER REFERENCES users(id),
    action     TEXT NOT NULL,
    target     TEXT,
    detail     TEXT,
    ip         TEXT,
    created_at DATETIME NOT NULL
);

-- 迁移版本
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL
);

-- 运行时系统设置
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL
);
