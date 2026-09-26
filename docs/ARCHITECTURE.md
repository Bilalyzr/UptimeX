# UptimeX — Architecture

This document maps the implemented system to the PRD's component model
(§10–§17) and explains the design decisions that matter operationally.

## Component map

```
                         ┌──────────────────────────────────────────────┐
                         │                cmd/monitor                  │
                         │                                              │
   Admin/API ────────────┤  api/routes ─► api/middleware ─► api/handlers│
                         │                                              │
                         │  internal/monitor.Engine                     │
                         │  ┌────────────┐    ┌──────────────────────┐ │
                         │  │ Scheduler  │───►│ Worker Pool (N)      │ │
                         │  │ (tick)     │jobs│  └─ checker.HTTP     │ │
                         │  └────────────┘    └──────────┬───────────┘ │
                         │                              │ results      │
                         │  ┌───────────────────────────▼───────────┐ │
                         │  │ Result Collector (single goroutine)   │ │
                         │  │  persist ─► detector ─► incidents ─►  │ │
                         │  │  status ─► alerts                     │ │
                         │  └───────────────────────────────────────┘ │
                         │  internal/metrics.Service (on-demand)      │
                         │  /health /ready  (heartbeat)               │
                         └───────┬───────────────────────┬────────────┘
                                 │                       │
                    ┌────────────▼──────────┐   ┌────────▼─────────┐
                    │ PostgreSQL / SQLite   │   │ Alert channels   │
                    │ (storage.Repository)  │   │ webhook / email  │
                    └───────────────────────┘   └──────────────────┘

   cmd/heartbeatchecker (separate process/container)
        │ GET /health every N s
        ▼
     monitor heartbeat — missing beats ⇒ MONITOR_DOWN alert (own webhook)
```

## Monitoring cycle (PRD §13)

1. **Scheduler** (`internal/scheduler`) ticks every `SCHEDULER_TICK` (1s
   default). Each cycle it loads enabled endpoints and their last-checked
   times, computes which are due (`last_checked + interval <= now`), and
   submits due endpoints to the job queue. Duplicate in-flight jobs per
   endpoint are prevented with an in-memory in-flight set released when the
   collector finishes processing that endpoint's result.
2. **Worker pool** (`internal/worker`) has a fixed `WORKER_COUNT` of
   goroutines consuming the bounded job channel. Concurrency is capped by
   construction — no unbounded goroutine creation.
3. **HTTP prober** (`internal/checker`) executes each probe with a hard
   `context.WithTimeout` from the endpoint's `timeout_ms`. It classifies
   failures into a stable taxonomy: `timeout`, `dns`, `connection`, `tls`,
   `http_error`, `blocked` (SSRF guard), `invalid_url`. A probe panic is
   recovered and converted into a structured failed check.
4. **Result collector** (in `internal/monitor`) is a single goroutine
   consuming the results channel. Serializing this stage is what keeps
   per-endpoint counters and incidents consistent without locks — order per
   endpoint is total.
5. **Failure detector** (`internal/detector`) is a pure function:
   `Evaluate(status, success, threshold, now) → (status, transition)`.
   HEALTHY → FAILING (1..threshold-1) → DOWN at threshold; any success
   resets the streak; success from DOWN emits RECOVERED.
6. **Incidents**: crossing the threshold opens an incident; continued
   failures update its failure count but never open a second incident —
   enforced at the database level by a partial unique index
   (`endpoint_id WHERE status='OPEN'`) on both PostgreSQL and SQLite.
   Recovery resolves it and stamps `resolved_at`.
7. **Alerts** (`internal/alert`) fan out DOWN / RECOVERED / HIGH_LATENCY
   notifications to configured channels with per-(endpoint, type) cooldown
   dedup. Webhook payloads are HMAC-SHA256 signed when a secret is set.
   Delivery failures are logged and counted, never fatal.

## Why these decisions

**Bounded pool, not one-goroutine-per-endpoint.** Sockets, file descriptors
and TLS handshakes are finite; at 1000 endpoints an unbounded fan-out would
destabilize the host. The pool bounds concurrent outbound work; benchmarks
(docs/BENCHMARKS.md) show the SQLite writer, not the pool, becomes the first
bottleneck at scale.

**Single collector goroutine.** Check results for one endpoint must be
applied in order. Rather than distributed locking, results flow through one
channel to one goroutine: per-endpoint ordering is free, races between state
transitions cannot exist, and throughput remains high because probing (the
slow part) is still fully parallel.

**DB-enforced single open incident.** Even if two transitions raced in a
future distributed deployment, the partial unique index makes a duplicate
OPEN incident impossible rather than merely unlikely.

**Liveness independent of readiness.** `/health` (liveness) never touches
the database — a broken DB must not silence the heartbeat the external
checker depends on. `/ready` reflects DB reachability for orchestrators.

**Checker-of-checkers outside the failure domain.** `cmd/heartbeatchecker`
is a separate process and container with its own webhook configuration. It
tolerates `HC_MISSES_THRESHOLD` consecutive misses (default 3) before
raising MONITOR_DOWN, and verifies the `heartbeat_at` timestamp is fresh so
a frozen proxy in front of a dead monitor still counts as dead.

**SSRF guard at two layers.** URL validation (scheme allow-list, no
credentials, literal-IP policy) at registration and probe time, plus a
dial-time `Control` hook that re-validates the resolved address before the
connection is established — so a hostname whose DNS later points inward is
still refused.

## Storage

One `storage.Repository` interface with two implementations sharing query
bodies (PostgreSQL placeholders are rebinding only). Migrations are embedded
SQL files per dialect, tracked in `schema_migrations`, applied in
transactions at startup. Schema (PRD §18):

- `endpoints` — configuration with CHECK constraints (interval ≥ 5s, timeout
  100ms–60s, threshold ≥ 1)
- `endpoint_status` — detector state, one row per endpoint (seeded on
  creation)
- `health_checks` — raw history, hot index `(endpoint_id, checked_at DESC)`
- `incidents` — outage windows, partial unique OPEN guard

Times are UTC everywhere. PostgreSQL uses native `TIMESTAMPTZ`; SQLite uses
DATETIME columns via database/sql.

## Metrics

Percentiles use the nearest-rank method over the most recent raw
observations in the requested window, bounded at 20,000 samples per
endpoint; every API response states the window, the interval and the sample
population (`sample_count`) — a P99 without its population is meaningless.
The seam for very large scale is `metrics.SummarizeLatency`, where
histogram-based aggregation would replace raw-sample scans.

## Graceful shutdown

SIGTERM → HTTP server drains in-flight requests → engine cancel stops
scheduling → pool closes the job channel, workers finish current probes →
collector persists remaining results and exits → DB closes. No checks are
dropped mid-flight; the shutdown is logged with final counters.

## Scaling path (PRD §33–§34)

The current design is one process. The extension path keeps the contracts:
- Split scheduler and workers into separate roles; the job queue becomes a
  broker topic; `CheckResult` already carries `endpoint_id` so regional
  workers can add `worker_id`/`region` fields without breaking storage.
- A coordinator assigns endpoint shards; the failure detector stays
  per-endpoint, so multi-region results converge through the same
  serialized collector (or per-endpoint partitioning in the DB).
- Historical aggregation into time buckets reduces dashboard query cost;
  retention jobs prune raw checks beyond the configured horizon.
