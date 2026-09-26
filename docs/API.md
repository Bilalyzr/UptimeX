# UptimeX — REST API Reference

Base URL: `http://<monitor-host>:8080`. All responses are JSON. Timestamps
are RFC3339 UTC.

When `API_KEY` is configured, mutating routes (`POST`, `PATCH`, `DELETE`)
require `X-API-Key: <key>` (or `Authorization: Bearer <key>`). Read routes
stay open for the dashboard. All administrative routes are rate-limited per
client IP (`RATE_LIMIT_RPS` / `RATE_LIMIT_BURST`; 429 on excess).

## Service health

### `GET /health` — liveness / heartbeat
Never touches the database. Returns 200 while the process serves.

```json
{
  "status": "healthy",
  "service": "uptimex",
  "version": "1.0.0",
  "monitor": "uptimex-1",
  "started_at": "2026-09-26T09:00:00Z",
  "uptime_seconds": 3600,
  "heartbeat_at": "2026-09-26T10:00:00Z"
}
```

### `GET /ready` — readiness
200 with `{"status":"ready","database":"ok"}` when the DB is reachable,
503 `"degraded"` otherwise.

## Endpoints

### `GET /api/v1/endpoints?window=1h`
Endpoint table rows: configuration + live state + windowed metrics.
Window: `30m`, `1h` (default), `6h`, `24h`, `7d` (max `30d`).

```json
{
  "endpoints": [
    {
      "id": 12, "name": "Payments API", "url": "https://api.example.com/health",
      "method": "GET", "interval_seconds": 30, "timeout_ms": 5000,
      "failure_threshold": 3, "expected_status_min": 200, "expected_status_max": 299,
      "enabled": true, "created_at": "...", "updated_at": "...",
      "state": "HEALTHY", "consecutive_failures": 0,
      "last_success_at": "...", "last_failure_at": null, "last_checked_at": "...",
      "window": "1h", "uptime_pct": 99.87, "p99_ms": 412.5, "avg_ms": 128.3,
      "total_checks": 118, "open_incident_id": null
    }
  ],
  "window": "1h"
}
```

### `POST /api/v1/endpoints` — register (auth)
```json
{
  "name": "Payments API",
  "url": "https://api.example.com/health",
  "method": "GET",
  "interval_seconds": 30,
  "timeout_ms": 5000,
  "failure_threshold": 3,
  "expected_status_min": 200,
  "expected_status_max": 299,
  "enabled": true
}
```
`201 Created` with the stored endpoint (defaults: interval 30s, timeout
5000ms, threshold 3, range 200–299, enabled). Validation errors return
`400 {"error": "..."}` naming every violated field; SSRF-blocked targets
(when `ALLOW_PRIVATE_TARGETS=false`) are rejected here.

### `GET /api/v1/endpoints/{id}`
`{"endpoint": {...}, "status": {...}, "open_incident": {...}?}` or 404.

### `PATCH /api/v1/endpoints/{id}` — partial update (auth)
Send only changed fields; unspecified fields keep their values.

### `DELETE /api/v1/endpoints/{id}` (auth)
`204 No Content`. Cascades to checks, status, incidents. 404 if absent.

### `GET /api/v1/endpoints/{id}/checks?since=6h&limit=200`
Historical raw checks, newest first. `since` accepts durations (`6h`) or
RFC3339 timestamps. `limit` 1–1000 (default 200).

```json
{"checks": [{"id": 991, "endpoint_id": 12, "checked_at": "...",
  "status_code": 503, "response_time_ms": 87, "success": false,
  "error_type": "http_error", "error_message": "status 503 outside expected range 200-299",
  "created_at": "..."}], "count": 1}
```
`status_code` and `error_type` are `null` for network-level failures.

### `GET /api/v1/endpoints/{id}/metrics?window=24h`
```json
{
  "endpoint_id": 12, "window": "24h", "from": "...", "to": "...",
  "total_checks": 2871, "successful_checks": 2860, "failed_checks": 11,
  "uptime_pct": 99.62, "failure_rate_pct": 0.38,
  "latency": {"p50_ms": 92, "p95_ms": 210, "p99_ms": 680, "avg_ms": 118,
              "min_ms": 41, "max_ms": 1240, "sample_count": 2860},
  "status_distribution": {"200": 2855, "201": 5, "5xx": 11},
  "latency_trend": [{"time": "...", "avg_ms": 95, "p95_ms": 190, "p99_ms": 540,
                     "checks": 119, "failures": 0}]
}
```
The population of every percentile is explicit: window + `sample_count`.

### `POST /api/v1/endpoints/{id}/test` (auth)
One on-demand probe; returns the raw `CheckResult` without touching the
failure state machine or history (manual diagnostics must not distort the
incident record).

## Analytics

### `GET /api/v1/metrics/overview?window=1h`
Global totals, endpoint census (healthy/failing/down/disabled), uptime,
failure rate, latency distribution, status distribution, latency trend and
open-incident count. Shape mirrors endpoint metrics plus `endpoints` and
`open_incidents`.

### `GET /api/v1/stats`
Live engine self-observability:

```json
{
  "started_at": "...", "uptime_seconds": 3600,
  "checks_attempted": 5122, "checks_succeeded": 5098, "checks_failed": 24,
  "pool": {"workers": 20, "active_workers": 3, "queue_depth": 0,
           "processed_total": 5122, "in_flight": 2},
  "scheduler": {"in_flight": 2, "duplicates_skipped": 17}
}
```

## Incidents

### `GET /api/v1/incidents?status=open|resolved|all&limit=100`
Newest first, joined with endpoint name/URL:

```json
{"incidents": [{"id": 8, "endpoint_id": 12, "endpoint_name": "Payments API",
  "endpoint_url": "https://api.example.com/health", "opened_at": "...",
  "resolved_at": null, "status": "OPEN", "failure_count": 5,
  "last_error": "status 503 outside expected range 200-299"}],
 "count": 1}
```

## Error responses

All errors use `{"error": "<message>"}` with an appropriate status:
`400` validation/malformed input, `401` missing/invalid API key, `404`
unknown resource, `429` rate limit (with `Retry-After: 1`), `500` internal
(details are logged server-side, never leaked).
