# UptimeX — Distributed Health & Uptime Monitoring Platform

A production-oriented observability platform that continuously probes HTTP/HTTPS
endpoints, records health and latency, distinguishes transient failures from
real incidents via a consecutive-failure threshold, raises and resolves
incidents, computes P50/P95/P99 analytics, and protects itself against
monitoring blind spots with an external checker-of-checkers.

Built to the specification in
`Distributed_Health_and_Uptime_Monitoring_E2E_PRD.docx` (the authoritative PRD
for this project).

```
Scheduler ──► Job Queue ──► Bounded Worker Pool ──► HTTP Prober (timeouts)
                                 │
                                 ▼
                          Result Collector ──► Failure State Machine
                                 │                  (threshold, recovery)
                                 ▼                         │
                        PostgreSQL / SQLite        Incident + Alerts
                                 │                         │
                                 ▼                         ▼
                        Metrics Engine  ◄────────────  Webhook / Email
                        (P50/P95/P99, uptime, status distribution)
                                 │
                                 ▼
                        REST API ◄──► React Dashboard
External Checker ──heartbeat──► /health  (monitor-level alerting if dead)
```

## Feature summary

| Capability | Implementation |
|---|---|
| Concurrent monitoring | Bounded worker pool (goroutines + channels), 50–1000+ endpoints |
| Slow endpoint isolation | Per-probe context deadline; one slow target never blocks others (tested) |
| Failure accuracy | Consecutive-failure threshold (default 3); single failures never page |
| Incidents | One OPEN incident per outage (DB-enforced), auto-resolved on recovery |
| Recovery detection | DOWN → success ⇒ RECOVERED + incident resolution + alert |
| Latency analytics | P50/P95/P99 (nearest-rank) over explicit windows, bounded populations |
| Status distribution | 200/201/2xx/3xx/4xx/5xx/timeout/network/blocked buckets |
| Alerting | Webhook (HMAC-signed) + optional SMTP email; cooldown dedup; HIGH_LATENCY |
| Self-monitoring | `/health` heartbeat + independent external checker container |
| Storage | PostgreSQL (production) / SQLite (dev) behind one repository interface |
| Security | SSRF guard (scheme, credentials, private/link-local/metadata targets, dial-time enforcement), API-key auth, rate limiting |
| Infrastructure | Multi-stage Dockerfiles, healthchecked docker-compose, GitHub Actions CI |
| Testing | Unit, concurrency/race, integration (PostgreSQL), API, E2E, failure injection, load benchmarks |

## Quick start (Docker)

```bash
cp .env.example .env        # adjust if needed
docker compose up --build
```

| Service | URL |
|---|---|
| Dashboard | http://localhost:8090 |
| Monitor API | http://localhost:8080/api/v1 |
| Heartbeat | http://localhost:8080/health |

The stack seeds one demo endpoint (the monitor's own heartbeat) so the
dashboard shows live data immediately. Register more via the dashboard or:

```bash
curl -X POST http://localhost:8080/api/v1/endpoints \
  -H 'Content-Type: application/json' \
  -d '{"name":"Example API","url":"https://example.com","interval_seconds":30,"timeout_ms":5000,"failure_threshold":3}'
```

## Quick start (local development)

Backend (SQLite, no database server needed):

```bash
DB_DRIVER=sqlite SQLITE_PATH=./dev.db WORKER_COUNT=20 ALLOW_PRIVATE_TARGETS=true \
  go run ./cmd/monitor
```

Dashboard dev server (proxies /api to :8080):

```bash
cd dashboard && npm install && npm run dev
```

External checker against the local monitor:

```bash
MONITOR_HEALTH_URL=http://localhost:8080/health go run ./cmd/heartbeatchecker
```

> Note for Windows hosts: if port 8080 is taken, set `HTTP_ADDR=:8010` (and
> `VITE_MONITOR_URL=http://localhost:8010` for the dashboard dev proxy).

## Testing

```bash
go test ./...                                   # unit + E2E + API (SQLite)
TEST_POSTGRES_DSN=postgres://monitor:monitor@localhost:5432/healthmonitor?sslmode=disable \
  go test ./tests/integration/                  # PostgreSQL integration
go test -race ./internal/... ./tests/e2e/...    # race detection (needs cgo)
cd dashboard && npm test                        # frontend unit tests
```

Load benchmarks (see `docs/BENCHMARKS.md` for measured results):

```bash
go run ./cmd/loadbench -endpoints 50 -rounds 3
go run ./cmd/loadbench -endpoints 1000 -workers 50 -rounds 3
```

## Configuration

All configuration comes from environment variables — see [.env.example](.env.example)
for the complete annotated list (storage, workers, alerts, security, ports).
Secrets are never hardcoded; the API key, webhook secrets and database
credentials must be injected via the environment or a secret manager.

## Project layout

```
cmd/monitor/            service entrypoint (engine + API + heartbeat)
cmd/heartbeatchecker/   external checker-of-checkers (separate failure domain)
cmd/loadbench/          load benchmark tool
internal/config         env configuration + validation
internal/models         domain entities
internal/checker        HTTP prober + SSRF guard
internal/scheduler      due-check scheduling, duplicate prevention
internal/worker         bounded worker pool
internal/detector       failure state machine (pure function)
internal/monitor        engine wiring + result collector
internal/metrics        percentiles, uptime, distributions, trends
internal/alert          webhook/email channels + cooldown manager
internal/storage        repository interface, PostgreSQL + SQLite, migrations
internal/heartbeatchecker  heartbeat polling + miss threshold logic
api/handlers            REST handlers
api/middleware          logging, recovery, CORS, auth, rate limiting
api/routes              route table + middleware chain
migrations/             embedded SQL migrations (postgres/ + sqlite/)
dashboard/              React + TypeScript + Recharts SPA
tests/                  e2e, failure injection, PostgreSQL integration
docs/                   architecture, API, benchmarks, security
.github/workflows/      CI pipeline
```

## Documentation

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — component map, data flow,
  concurrency design, failure model, scaling path
- [docs/API.md](docs/API.md) — REST API reference with examples
- [docs/BENCHMARKS.md](docs/BENCHMARKS.md) — 50/100/500/1000 endpoint
  benchmark results and analysis
- [docs/SECURITY.md](docs/SECURITY.md) — SSRF protection, secrets, auth

## Operational narrative

The platform answers the four questions from the PRD continuously: are
services reachable, how fast do they respond, are failures persistent enough
to be incidents, and is the monitor itself alive. The failure state machine
filters transient blips, one outage maps to exactly one incident, P99 exposes
tail latency that averages hide, and an external checker closes the
"who monitors the monitor" gap through a heartbeat with a missed-beat
tolerance before alerting.
