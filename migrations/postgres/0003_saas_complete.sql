-- Phase 3 completion: custom HTTP probe payloads/assertions, per-tenant alert
-- channels, per-organization API keys, and single-use password reset tokens.

ALTER TABLE endpoints ADD COLUMN headers_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE endpoints ADD COLUMN body TEXT NOT NULL DEFAULT '';
ALTER TABLE endpoints ADD COLUMN expected_body_contains TEXT NOT NULL DEFAULT '';

CREATE TABLE alert_channels (
    id         BIGSERIAL PRIMARY KEY,
    org_id     BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('webhook','email')),
    target     TEXT NOT NULL,
    secret     TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_alert_channels_org ON alert_channels(org_id);

CREATE TABLE api_keys (
    id           BIGSERIAL PRIMARY KEY,
    org_id       BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_api_keys_org ON api_keys(org_id);

CREATE TABLE password_reset_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_password_resets_expiry ON password_reset_tokens(expires_at);
