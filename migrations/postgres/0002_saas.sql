-- SaaS multi-tenancy: organizations (tenants), users, login sessions and
-- org-scoped endpoints. NULL endpoints.org_id = pre-SaaS global endpoints
-- owned by the operator (visible in legacy API-key mode only).

CREATE TABLE organizations (
    id                   BIGSERIAL PRIMARY KEY,
    name                 TEXT       NOT NULL,
    slug                 TEXT       NOT NULL UNIQUE,
    plan                 TEXT       NOT NULL DEFAULT 'free',
    status_page_enabled  BOOLEAN    NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    org_id        BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email         TEXT   NOT NULL UNIQUE,
    password_hash TEXT   NOT NULL,
    name          TEXT   NOT NULL DEFAULT '',
    role          TEXT   NOT NULL DEFAULT 'owner' CHECK (role IN ('owner','member')),
    created_at    TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_users_org ON users(org_id);

CREATE TABLE sessions (
    token_hash TEXT   PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

ALTER TABLE endpoints ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
CREATE INDEX idx_endpoints_org ON endpoints(org_id);
