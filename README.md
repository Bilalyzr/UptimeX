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
| SaaS multi-tenancy | Organizations, signup/login (PBKDF2 password hashing, HttpOnly cookie sessions), per-org data isolation, plan quotas, billing, public status pages |
| Storage | PostgreSQL (production) / SQLite (dev) behind one repository interface |
| Security | SSRF guard (scheme, credentials, private/link-local/metadata targets, dial-time enforcement), API-key auth, rate limiting |
| Infrastructure | Multi-stage Dockerfiles, healthchecked docker-compose, GitHub Actions CI |
| Testing | Unit, concurrency/race, integration (PostgreSQL), API, E2E, failure injection, load benchmarks |

## Running as a SaaS product

The same binary runs either as a single-tenant self-hosted monitor (default)
or as a multi-tenant SaaS. Set `SAAS_MODE=true` and the full product surface
turns on:

- **Accounts & tenancy** — `POST /api/v1/auth/signup` creates an organization
  (tenant) with its owner user; passwords are PBKDF2-HMAC-SHA256 hashed and
  sessions are random 256-bit tokens in HttpOnly cookies (only SHA-256 hashes
  are stored). Every endpoint, incident and metric is scoped to the caller's
  organization; one tenant can never read another's data.
- **Plans & quotas** — Free ($0: 5 endpoints, 60s interval), Pro ($20/mo: 50,
  30s) and Business ($99/mo: 250, 10s). Quotas and interval floors are
  enforced server-side on create/update (`internal/plans`).
- **Billing** — `GET/POST /api/v1/org*` serves the plan catalog, live usage
  and plan changes. Plan switching is immediate (demo billing); the handler
  is the integration point for a Stripe checkout webhook.
- **Public status pages** — every org gets an anonymous status page at
  `/status/{org-slug}` (API: `GET /api/v1/public/status/{slug}`) with live
  service states, 24h uptime, P95 and open incidents; orgs can disable theirs.
- **Marketing site** — the dashboard SPA serves a landing page with pricing
  at `/`, auth at `/login` & `/signup`, and the app at `/app/*`.

In SaaS mode anonymous API access is rejected; the operator `API_KEY`
retains full global access. Legacy (non-SaaS) behavior is byte-for-byte
unchanged — all pre-SaaS tests pass unmodified.

```bash
DB_DRIVER=sqlite SQLITE_PATH=./saas.db HTTP_ADDR=:8010 SAAS_MODE=true \
  go run ./cmd/monitor
```

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
>
> If another stack already publishes 8080/8090 on this machine, copy
> `.env.example` to `.env` and override the host ports there — compose reads
> `.env` automatically:
> `MONITOR_PORT=18080` / `DASHBOARD_PORT=18090`.

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
