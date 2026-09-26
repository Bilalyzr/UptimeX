# UptimeX — Load Benchmarks

Measured with `cmd/loadbench` on the development machine (Windows 11,
Docker Desktop for PostgreSQL). Each configuration ran 3 rounds of a full
monitoring cycle: every endpoint made due simultaneously, measurement until
all checks were persisted. 5% of endpoints respond slowly (400 ms) to keep
the pool honestly loaded. Database insert latency sampled over 200 sequential
`health_checks` INSERTs.

## Results (SQLite, in-process dev adapter, 20 workers unless noted)

| Endpoints | Best cycle | Throughput | Max active workers | Max queue | Heap | DB insert p50/p95/p99 |
|---:|---:|---:|---:|---:|---:|---|
| 50 | 391 ms | 128 checks/s | 3/20 | 0 | 1.5 MB | 1.6 / 4.4 / 6.0 ms |
| 100 | 376 ms | 266 checks/s | 0/20* | 0 | 2.3 MB | 4.1 / 73 / 300 ms** |
| 500 | 3,613 ms | 138 checks/s | 11/20 | 0 | 2.3 MB | 1.5 / 2.8 / 6.0 ms |
| 1,000 | 4,595 ms | 218 checks/s | 20/20 | 6 | 2.6 MB | 2.8 / 6.5 / 9.9 ms |
| 1,000 (50 workers) | 6,353 ms | 157 checks/s | 26/50 | 0 | 3.9 MB | 3.2 / 9.7 / 12.6 ms |

\* sampling artifact — the 25 ms utilization sampler missed the sub-second
burst.
\** one-off WAL checkpoint stall; later rounds were stable.

## PostgreSQL comparison (1,000 endpoints, 20 workers)

| Setup | Best cycle | Throughput | DB insert p50/p95/p99 |
|---|---:|---:|---|
| Postgres via host port-forward (Docker Desktop) | 11,537 ms | 87 checks/s | 6.2 / 9.6 / 11.3 ms |
| Postgres inside the compose network | 8,212 ms | 122 checks/s | 3.4 / 6.4 / 15.9 ms |

Through-Docker-Desktop port forwarding adds multiple milliseconds of
round-trip latency per statement, and the serialized result collector issues
several statements per check — so the forwarded setup is bound by network
RTT, not by PostgreSQL itself. Even in-network, this Docker-Desktop
PostgreSQL (running inside the Linux VM) pays cross-VM latency per
statement; on native Linux the gap to in-process SQLite (4.6 s) would close
substantially. The architectural conclusion is unaffected: per-check
database round trips, not probing or the pool, set the ceiling — batching
inserts is the first optimization lever at 10k-endpoint scale.

## Analysis

1. **The 50+ endpoint requirement is comfortably met.** A full 50-endpoint
   cycle completes in ~0.4 s with only 3 of 20 workers ever active; the
   engine is nowhere near its limits at the PRD's target scale.
2. **The worker pool is not the first bottleneck.** Worker utilization
   reaches the cap only at 1,000 endpoints, and raising workers 20 → 50
   made cycles *slower* (6.4 s vs 4.6 s): more concurrent workers means more
   concurrent database writes, and SQLite's single-writer WAL serializes
   them (busy_timeout waits grow). With SQLite the DB write path is the
   constraint — exactly the "determine whether the worker pool becomes the
   bottleneck" question the PRD asks to answer.
3. **Throughput stays flat-ish (~100–250 checks/s) because the result
   collector is serialized by design.** Every check costs 2–3 DB statements
   (insert check, upsert status, occasionally incident lookups) issued in
   order per endpoint. At 1,000 endpoints a cycle completes in ~4.6 s,
   i.e. ~220 checks/s sustained including 50 deliberately slow probes —
   well within the per-endpoint intervals a real fleet uses (a 1,000
   endpoint fleet at 30 s intervals needs only ~33 checks/s steady-state).
4. **Memory is not a concern at this scale** (2.6 MB heap at 1,000
   endpoints; goroutine count is worker-bound).
5. **Scaling headroom:** for 10k+ endpoints, the next steps (per
   docs/ARCHITECTURE.md §Scaling) are batching check inserts, per-endpoint
   partitioning of the collector, and histogram-based percentiles — the
   current contracts already allow them.

## Reproducing

```bash
go run ./cmd/loadbench -endpoints 50 -rounds 3
go run ./cmd/loadbench -endpoints 1000 -workers 50 -rounds 3
go run ./cmd/loadbench -endpoints 1000 -db postgres \
  -dsn "postgres://monitor:monitor@postgres:5432/healthmonitor?sslmode=disable"
```

The tool starts its own local target fleet (4 servers, `-slow-frac` of
endpoints respond slowly), registers endpoints, and measures full-cycle
completion, throughput, worker utilization (sampled), queue depth, heap and
DB insert latency.
