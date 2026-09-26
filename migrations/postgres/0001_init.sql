-- Initial schema for the health monitoring platform (PostgreSQL).

CREATE TABLE endpoints (
    id                  BIGSERIAL PRIMARY KEY,
    name                VARCHAR(255) NOT NULL,
    url                 TEXT         NOT NULL,
    method              VARCHAR(10)  NOT NULL DEFAULT 'GET',
    interval_seconds    INT          NOT NULL DEFAULT 30  CHECK (interval_seconds >= 5),
    timeout_ms          INT          NOT NULL DEFAULT 5000 CHECK (timeout_ms BETWEEN 100 AND 60000),
    failure_threshold   INT          NOT NULL DEFAULT 3    CHECK (failure_threshold >= 1),
    expected_status_min INT          NOT NULL DEFAULT 200,
    expected_status_max INT          NOT NULL DEFAULT 299,
    enabled             BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE endpoint_status (
    endpoint_id          BIGINT PRIMARY KEY REFERENCES endpoints(id) ON DELETE CASCADE,
    state                VARCHAR(16) NOT NULL DEFAULT 'HEALTHY'
                         CHECK (state IN ('HEALTHY','FAILING','DOWN')),
    consecutive_failures INT         NOT NULL DEFAULT 0,
    last_success_at      TIMESTAMPTZ,
    last_failure_at      TIMESTAMPTZ,
    last_checked_at      TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE health_checks (
    id               BIGSERIAL PRIMARY KEY,
    endpoint_id      BIGINT NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    checked_at       TIMESTAMPTZ NOT NULL,
    status_code      INT,
    response_time_ms BIGINT NOT NULL CHECK (response_time_ms >= 0),
    success          BOOLEAN NOT NULL,
    error_type       VARCHAR(32),
    error_message    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Historical queries filter by endpoint + time; this is the hot index.
CREATE INDEX idx_health_checks_endpoint_time ON health_checks (endpoint_id, checked_at DESC);
CREATE INDEX idx_health_checks_time ON health_checks (checked_at DESC);

CREATE TABLE incidents (
    id            BIGSERIAL PRIMARY KEY,
    endpoint_id   BIGINT NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    opened_at     TIMESTAMPTZ NOT NULL,
    resolved_at   TIMESTAMPTZ,
    status        VARCHAR(16) NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED')),
    failure_count INT NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL DEFAULT ''
);

-- One OPEN incident per endpoint, enforced by the database itself.
CREATE UNIQUE INDEX idx_incidents_one_open ON incidents (endpoint_id) WHERE status = 'OPEN';
CREATE INDEX idx_incidents_endpoint_time ON incidents (endpoint_id, opened_at DESC);
CREATE INDEX idx_incidents_status_opened ON incidents (status, opened_at DESC);
