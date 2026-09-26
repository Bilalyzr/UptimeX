# UptimeX — Security

This platform performs arbitrary outbound HTTP requests based on user input,
which makes it SSRF-sensitive by nature. The mitigations below implement
PRD §26.

## SSRF protection (outbound probing)

Two enforcement layers in `internal/checker`:

1. **URL validation** (`Guard.ValidateURL`) at registration and probe time:
   - only `http`/`https` schemes
   - embedded credentials rejected (no secret leakage into logs/DB)
   - syntactic host/port checks
   - literal private/reserved IPs rejected in strict mode
2. **Dial-time control** (`Guard.Control`): a `net.Dialer` control hook
   invoked after DNS resolution, before the connection is established. Any
   resolved address in loopback, RFC1918/ULA, link-local (this covers cloud
   metadata endpoints like `169.254.169.254`), unspecified, multicast,
   CGNAT (100.64/10) or benchmarking (198.18/15) space is refused — so a
   hostname whose DNS later points inward is still blocked
   (rebinding-resistant at connect time).

`ALLOW_PRIVATE_TARGETS=true` disables these checks **only** for local
development and explicitly-trusted internal monitoring (the bundled
docker-compose demo monitors its own heartbeat endpoint inside the compose
network and sets this flag with a documented warning). It must remain
`false` for any internet-facing deployment.

Blocked probes are recorded as failed checks with `error_type=blocked` and
flow through the normal failure pipeline, so misconfiguration is visible on
the dashboard rather than silently ignored.

## Secrets

- No credentials, API keys or webhook secrets are stored in source control;
  `.env` is git-ignored and `.env.example` carries placeholders only.
- Everything is injected via environment variables (`API_KEY`,
  `ALERT_WEBHOOK_SECRET`, `HC_ALERT_WEBHOOK_SECRET`, `POSTGRES_PASSWORD`,
  SMTP credentials) — suitable for Docker secrets or any secret manager.
- Webhook payloads are HMAC-SHA256 signed (`X-UptimeX-Signature:
  sha256=<hex>`) so receivers can verify authenticity.
- Error messages persisted from probes are sanitized (query strings stripped,
  length-capped); database errors are logged server-side, never returned to
  clients.
- `/health` and `/ready` expose no internal details (no DSNs, no paths).

## API protection

- `API_KEY` (env): when set, all mutating routes require `X-API-Key` /
  `Authorization: Bearer`, compared in constant time. Reads stay open for
  dashboard consumption behind the proxy.
- Rate limiting: token bucket per client IP on administrative APIs
  (default 20 rps, burst 40) returning `429` with `Retry-After`.
- Strict JSON decoding (`DisallowUnknownFields`, 1 MiB body cap).
- Request logging records method/path/status/duration — never payloads or
  headers.
- CORS only enabled when `CORS_ALLOWED_ORIGIN` is set; the bundled
  deployment is same-origin (nginx proxies `/api`), so no CORS is needed.
- For internet-facing deployments, terminate TLS in front of the monitor and
  dashboard (PRD: HTTPS required externally); compose exposes plain HTTP on
  purpose for local use.

## Multi-tenancy note

The current API is single-tenant/trusted-operator. Before allowing
untrusted users to register endpoints, keep `ALLOW_PRIVATE_TARGETS=false`,
enforce per-user quotas, and consider an egress allow-list or dedicated
probe egress network so the SSRF surface stays bounded.
