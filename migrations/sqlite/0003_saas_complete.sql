-- Phase 3 completion (SQLite development mode). Mirrors postgres/0003_saas_complete.sql.

ALTER TABLE endpoints ADD COLUMN headers_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE endpoints ADD COLUMN body TEXT NOT NULL DEFAULT '';
ALTER TABLE endpoints ADD COLUMN expected_body_contains TEXT NOT NULL DEFAULT '';

CREATE TABLE alert_channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id     INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('webhook','email')),
    target     TEXT NOT NULL,
    secret     TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX idx_alert_channels_org ON alert_channels(org_id);

CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id       INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at DATETIME,
    created_at   DATETIME NOT NULL
);
CREATE INDEX idx_api_keys_org ON api_keys(org_id);

CREATE TABLE password_reset_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX idx_password_resets_expiry ON password_reset_tokens(expires_at);
