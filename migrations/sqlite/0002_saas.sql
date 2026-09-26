-- SaaS multi-tenancy (SQLite development mode). Mirrors postgres/0002_saas.sql.

CREATE TABLE organizations (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    name                 TEXT    NOT NULL,
    slug                 TEXT    NOT NULL UNIQUE,
    plan                 TEXT    NOT NULL DEFAULT 'free',
    status_page_enabled  INTEGER NOT NULL DEFAULT 1,
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL
);

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id        INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    name          TEXT    NOT NULL DEFAULT '',
    role          TEXT    NOT NULL DEFAULT 'owner' CHECK (role IN ('owner','member')),
    created_at    DATETIME NOT NULL
);
CREATE INDEX idx_users_org ON users(org_id);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id     INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

ALTER TABLE endpoints ADD COLUMN org_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX idx_endpoints_org ON endpoints(org_id);
