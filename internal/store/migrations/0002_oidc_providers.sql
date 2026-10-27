-- OIDC Provider 配置
CREATE TABLE oidc_providers (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    issuer        TEXT NOT NULL,
    client_id     TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    scopes        TEXT NOT NULL DEFAULT 'openid email profile',
    claim_rules   TEXT,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL
);
