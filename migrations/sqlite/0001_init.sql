-- Initial schema for the health monitoring platform (SQLite development mode).

CREATE TABLE endpoints (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT    NOT NULL,
    url                 TEXT    NOT NULL,
    method              TEXT    NOT NULL DEFAULT 'GET',
    interval_seconds    INTEGER NOT NULL DEFAULT 30  CHECK (interval_seconds >= 5),
    timeout_ms          INTEGER NOT NULL DEFAULT 5000 CHECK (timeout_ms BETWEEN 100 AND 60000),
    failure_threshold   INTEGER NOT NULL DEFAULT 3    CHECK (failure_threshold >= 1),
    expected_status_min INTEGER NOT NULL DEFAULT 200,
    expected_status_max INTEGER NOT NULL DEFAULT 299,
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL
);

CREATE TABLE endpoint_status (
    endpoint_id          INTEGER PRIMARY KEY REFERENCES endpoints(id) ON DELETE CASCADE,
    state                TEXT    NOT NULL DEFAULT 'HEALTHY'
                         CHECK (state IN ('HEALTHY','FAILING','DOWN')),
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_success_at      DATETIME,
    last_failure_at      DATETIME,
    last_checked_at      DATETIME,
    updated_at           DATETIME NOT NULL
);

CREATE TABLE health_checks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id      INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    checked_at       DATETIME NOT NULL,
    status_code      INTEGER,
    response_time_ms INTEGER NOT NULL CHECK (response_time_ms >= 0),
    success          INTEGER NOT NULL,
    error_type       TEXT,
    error_message    TEXT,
    created_at       DATETIME NOT NULL
);

CREATE INDEX idx_health_checks_endpoint_time ON health_checks (endpoint_id, checked_at DESC);
CREATE INDEX idx_health_checks_time ON health_checks (checked_at DESC);

CREATE TABLE incidents (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id   INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    opened_at     DATETIME NOT NULL,
    resolved_at   DATETIME,
    status        TEXT    NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','RESOLVED')),
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT    NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX idx_incidents_one_open ON incidents (endpoint_id) WHERE status = 'OPEN';
CREATE INDEX idx_incidents_endpoint_time ON incidents (endpoint_id, opened_at DESC);
CREATE INDEX idx_incidents_status_opened ON incidents (status, opened_at DESC);
