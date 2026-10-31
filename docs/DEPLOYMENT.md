# UptimeX — Free Production Deployment Guide

Deploy the full platform (Go monitor API + React dashboard + PostgreSQL) to
the public internet for **$0**, with HTTPS, accounts, and public status pages.

Stack chosen for October 2026 free-tier reality (limits verified Oct 2026 —
re-check each provider before signing up, free tiers change often):

| Piece | Provider | Free tier we rely on | Runs |
|---|---|---|---|
| PostgreSQL | **Supabase** | 500 MB DB, 2 active projects, no credit card | Always-on compute while in use; pauses only after ~7 days of *inactivity* |
| Go API (monitor engine) | **Render** | 512 MB / 0.1 CPU web service, Docker builds | 24/7 *if* kept awake (Step 3) |
| Dashboard (React SPA) | **Cloudflare Pages** | Unlimited bandwidth, 500 builds/month | Global CDN |
| Keep-alive + watchdog | **cron-job.org** | Unlimited cron jobs, failure emails | Pings /health every 10 min |

```
 Visitors ──► Cloudflare Pages (https://uptimex.pages.dev)
                 │  static React bundle
                 └─ /api/*, /health proxied ──► Render (https://uptimex-api.onrender.com)
                                        │              ▲
                                        ▼              │ GET /health every 10 min
                 Supabase PostgreSQL ◄──────────── cron-job.org (keep-alive)
```

Why this exact shape:

- The dashboard calls the API with **same-origin relative URLs** (`/api/…`).
  Cloudflare Pages `_redirects` (already in `dashboard/public/_redirects`)
  proxies those paths to Render — same trick as `dashboard/nginx.conf` in the
  Docker setup. No CORS, session cookies stay first-party.
- **Neon was rejected**: its free plan now caps compute at ~100 CU-hours/month
  (~400 active hours). An uptime monitor writes check results constantly, so
  its database is *never* idle — Neon free would suspend mid-month.
- **Vercel Hobby was rejected** (non-commercial ToS), **Netlify** moved to a
  300-credit/month cap, **Fly.io / Railway** no longer have free tiers, and
  **Render's free Postgres expires after 30 days** — hence Supabase.
- The repo's `cmd/heartbeatchecker` (checker-of-checkers) is replaced here by
  cron-job.org — an external failure domain that also keeps Render awake.

Total time: **~45–60 minutes**. Everything below is click-by-click.

---

## Part 0 — Prerequisites

- The GitHub repo already exists and is current: `https://github.com/Bilalyzr/UptimeX`
  (CI runs gofmt/vet/tests on push; the tree is clean, so pushes pass).
- Free accounts to create (no credit card needed for any of them):
  1. [Supabase](https://supabase.com) — sign in with GitHub
  2. [Render](https://render.com) — sign in with GitHub
  3. [Cloudflare](https://dash.cloudflare.com/sign-up) (for Pages)
  4. [cron-job.org](https://cron-job.org)
- You will need the **Render service URL** in two later steps. It is
  predictable: `https://<service-name>.onrender.com`. This guide uses
  **`uptimex-api`** as the name, i.e. `https://uptimex-api.onrender.com` —
  substitute your own everywhere you see it.

---

## Part 1 — PostgreSQL on Supabase (free)

1. Dashboard → **New project**.
   - Name: `uptimex`
   - Database password: click **Generate**, then **save it somewhere safe**
     (you can't view it again later, only reset it).
   - Region: pick the one closest to where you'll create the Render service
     (Render Frankfurt ↔ Supabase eu-central, Render Oregon ↔ Supabase us-west,
     etc.). Same-region keeps check latency low.
2. Wait ~2 min for provisioning.
3. Open the project → top bar **Connect** (or Project Settings → Database).
   Under **Connection string → URI**, choose the **Session pooler** tab
   (host looks like `aws-0-<region>.pooler.supabase.com`, port **5432**).
   Session mode gives ~200 client slots and works fine with this app's pgx
   driver; the *direct* connection works too but is capped around 60 clients
   and the app opens up to 20.
4. Copy the URI. It looks like:
   ```
   postgresql://postgres.abcdefghijk:YOUR-PASSWORD@aws-0-eu-central-1.pooler.supabase.com:5432/postgres
   ```
   Make sure it ends with `?sslmode=require` (add it if the panel didn't):
   ```
   postgresql://postgres.abcdefghijk:YOUR-PASSWORD@aws-0-eu-central-1.pooler.supabase.com:5432/postgres?sslmode=require
   ```
   This string is your **`POSTGRES_DSN`** — keep it for Part 2. Treat it as a
   secret (it contains the DB password).

> Free-tier facts that matter later:
> - The project **pauses after ~7 days with zero database activity**. The
>   monitor queries Supabase continuously, so this never triggers while the
>   API is live. If you ever take Render down for over a week, resume the
>   project manually in Supabase → Settings → Infrastructure.
> - 500 MB storage is millions of check-result rows — far more than a
>   free-tier workload (2 endpoints/org) needs. Watch Project Settings →
>   Reports if you onboard many orgs.

---

## Part 2 — Go API on Render (free web service)

1. [dash.render.com](https://dash.render.com) → **New +** → **Web Service**.
2. Select the **Bilalyzr/UptimeX** repo. (If it doesn't appear, use
   "Configure account" next to GitHub and grant access to it.)
3. Settings:
   - **Name**: `uptimex-api` ← determines your URL, used below.
   - **Region**: same region you picked for Supabase.
   - **Runtime**: leave **Docker** selected — Render auto-detects the root
     `Dockerfile` (multi-stage Go build, listens on :8080, EXPOSE 8080).
   - **Instance type**: **Free**.
4. **Health Check Path** (advanced section): `/health`.
5. **Environment variables** — add these exactly (secret values via
   "Add secret file"? No — plain *Environment variables*):

   | Key | Value | Notes |
   |---|---|---|
   | `APP_ENV` | `production` | |
   | `HTTP_ADDR` | `:8080` | matches Dockerfile EXPOSE |
   | `DB_DRIVER` | `postgres` | |
   | `POSTGRES_DSN` | *(Supabase session-pooler URI from Part 1, with `sslmode=require`)* | the only required secret |
   | `SAAS_MODE` | `true` | enables signup/login, orgs, plans, public status pages |
   | `SESSION_COOKIE_SECURE` | `true` | HTTPS everywhere in this stack |
   | `SESSION_TTL` | `168h` | 7-day sessions (default, listed for clarity) |
   | `ALLOW_PRIVATE_TARGETS` | `false` | SSRF guard — **must** be false internet-facing |
   | `SEED_DEMO_ENDPOINTS` | `true` | seeds the public demo org `acme-corp` on first boot |
   | `SEED_SELF_URL` | `https://uptimex-api.onrender.com/health` | the demo org's self-check target — **must be the public Render URL**, not localhost (private targets are blocked in production) |
   | `MONITOR_NAME` | `uptimex-prod` | instance label in /health and alerts |
   | `WORKER_COUNT` | `20` | default, fine on 512 MB |
   | `RATE_LIMIT_RPS` | `20` | defaults, listed for visibility |
   | `RATE_LIMIT_BURST` | `40` | |

   Optional now or later (Settings → Environment → Add):
   - `API_KEY` — if set, mutating operator routes require `X-API-Key`
   - `ALERT_WEBHOOK_URL` + `ALERT_WEBHOOK_SECRET` — Slack/Discord/generic
     webhook for DOWN/UP/HIGH_LATENCY alerts (`ALERT_HIGH_LATENCY_MS` > 0
     enables latency alerts)
   - `SMTP_HOST/PORT/USERNAME/PASSWORD/FROM/TO` — email alert channel

6. **Deploy Web Service**. First build takes ~5–10 minutes (Go module
   download + static build in Docker).
7. Verify — open `https://uptimex-api.onrender.com/health`. You should get
   JSON with `status: "ok"`, your `monitor_name`, and an `uptime_seconds`
   counter. Also check the Render **Logs** tab for:
   ```
   storage_ready driver=postgres
   seeded_demo_org org_id=... slug=acme-corp endpoints=2
   http_server_started addr=:8080
   ```
   Migrations run automatically on boot (embedded SQL, `migrations/postgres/`)
   — no manual migration step exists or is needed.

> **Free-tier caveat:** Render sleeps any web service after **15 minutes
> without inbound traffic** — for an uptime monitor that would silently stop
> all checks. Part 3 fixes this permanently.

---

## Part 3 — Keep-alive + external watchdog on cron-job.org (free)

This single cron job (a) prevents Render's 15-minute spin-down, and (b)
emails you if the monitor itself dies — the same role `cmd/heartbeatchecker`
plays in the Docker topology, but hosted in a genuinely separate failure
domain (and it doesn't consume a second always-on Render service).

1. [cron-job.org](https://cron-job.org) → Create account → **Create cronjob**.
2. Settings:
   - **Title**: `UptimeX keep-alive`
   - **URL**: `https://uptimex-api.onrender.com/health`
   - **Schedule**: Every **10 minutes** (`*/10 * * * *`)
   - Request method `GET`, no body.
   - **Notifications → on failure** → enable email alerts (creates a
     "monitor the monitor" watchdog for free).
   - **Advanced**: enable "Follow redirects"; timeout 60 s is fine
     (the first request after a spin-down takes ~50 s to wake Render).
3. Save. Run it once manually ("Run now") — it should report success.

Every Render restart (redeploy, rare infra recycles) also automatically
re-runs migrations if needed and reseeds nothing (seeding is idempotent and
skips when data exists).

---

## Part 4 — Dashboard on Cloudflare Pages (free)

Cloudflare Pages `_redirects` only supports relative rewrites (not external
domains), so UptimeX includes lightweight **Cloudflare Pages Functions** in
`dashboard/functions/` (`api/[[path]].ts`, `health.ts`, `ready.ts`) scoped via
`dashboard/public/_routes.json` (static asset requests never invoke a Worker).

1. Cloudflare dashboard → **Workers & Pages** → **Create** → **Pages** tab →
   **Connect to Git** → authorize & select `Bilalyzr/UptimeX`.
2. Build configuration:
   - **Project name**: `uptimex` → final URL `https://uptimex.pages.dev`
   - **Production branch**: `main`
   - **Framework preset**: `Vite` (or None — values below matter, preset
     doesn't)
   - **Root directory (advanced)**: `dashboard` ← **critical**, the app lives
     in the subdirectory
   - **Build command**: `npm run build`
   - **Build output directory**: `dist`
   - **Environment variables (Production & Preview)**:
     - `MONITOR_ORIGIN` = `https://uptimex-api.onrender.com` *(your real Render URL from Part 2, no trailing slash)*
3. **Save and Deploy** (~2 min). Then verify the whole chain:
   ```bash
   curl https://uptimex.pages.dev/health        # proxied to Render → JSON
   curl https://uptimex.pages.dev/api/v1/...    # any API route answers
   ```
   Open `https://uptimex.pages.dev` in a browser — the landing page loads,
   and logging in / signing up works because `/api/*` is proxied server-side
   (cookies stay first-party, no CORS involved).

---

## Part 5 — First-run checklist (your live SaaS)

1. **Sign up**: landing page → Get started → create your account. This
   creates your organization in SaaS mode.
2. **Add a real endpoint**: Dashboard → Add endpoint → any **public** URL
   (e.g. `https://example.com`). Private/loopback/metadata IPs are rejected
   by the SSRF guard — that's by design in production.
3. Confirm checks arrive (fresh results each interval, uptime sparkline).
4. **Demo status page**: `https://uptimex.pages.dev/status/acme-corp` —
   seeded on first boot, proves public status pages work end-to-end.
5. **Your own status page**: enable the status page toggle for your org;
   it serves at `/status/<your-slug>`.
6. Optional: configure per-workspace webhook/email alerts directly under
   **Alerts** in the dashboard (or operator-level `ALERT_WEBHOOK_URL` on Render).
7. Bookmark for ops:
   - Render Logs — live API logs (`https://dashboard.render.com`)
   - Supabase Table Editor — peek at `endpoints`, `health_checks`, `organizations`
   - cron-job.org — keep-alive job history/failure emails

---

## Part 6 — Optional: custom domains (still $0)

- **Dashboard**: Cloudflare Pages → your project → **Custom domains** → add
  `status.yourdomain.com` (DNS on Cloudflare = one click; elsewhere, add a
  CNAME to `uptimex.pages.dev`).
- **API**: Render → Service Settings → **Add Custom Domain**
  (e.g. `api.yourdomain.com`, CNAME to `uptimex-api.onrender.com`). If you do
  this, update `SEED_SELF_URL`, `MONITOR_ORIGIN` in Cloudflare Pages Settings,
  and the cron-job.org URL to the new domain, then redeploy.
- Custom domains on both tiers are free; certificates are automatic.

---

## Troubleshooting

| Symptom | Cause → Fix |
|---|---|
| First `/health` request after idle takes ~50 s | Render cold start. cron-job.org (Part 3) makes this invisible; ensure the job is enabled and running every 10 min. |
| `pages.dev` loads but login/add-endpoint fails, network tab shows 502/504 on `/api/*` | `MONITOR_ORIGIN` is missing or wrong in Cloudflare Pages → Settings → Environment variables → set to your `https://<service>.onrender.com` URL and trigger a redeploy. Also check Render itself isn't crashed (its Logs tab). |
| `/health` returns 500 or app exits with `storage_open failed` | `POSTGRES_DSN` wrong/password reset. Supabase: reset DB password in Settings → Database, rebuild the session-pooler URI, update the Render env var. |
| Demo self-check (`Monitor Self (heartbeat)`) errors "private target" | `SEED_SELF_URL` isn't the public https URL. Set it to `https://<service>.onrender.com/health` and (once) delete/re-add the demo endpoint or just ignore — it's cosmetic on the demo org. |
| Everything worked for days, now all API calls fail | Supabase project paused (only after ~7 days of zero DB traffic) → resume in Supabase dashboard. Or Render free instance was recycled → check service status; cron-job.org emails tell you which. |
| Supabase connection limit errors in logs | You switched to the *direct* URI (60-conn cap). Use the **session pooler** URI from Part 1; app opens ≤ 20 conns (`DB_MAX_OPEN_CONNS`). |
| Deploys stop after ~10 frontend changes in a month | Cloudflare Pages free = 500 builds/month; batch changes or use "Direct Upload" for hotfixes. |
| Checks fire but dashboard shows nothing | Browser cache holding an old bundle — hard refresh; check the network tab is hitting same-origin `/api/*` (if someone changed the SPA to absolute URLs, revert — relative URLs are the design). |

---

## Alternatives & why not (as of Oct 2026)

| Option | Verdict |
|---|---|
| **Neon** free Postgres | Cap ~100 CU-h/month ≈ 400 active hours; always-active monitor DB gets suspended mid-month. Good for dev branches, not this. |
| **Netlify** frontend | Works (proxy rewrites supported) but free plan now = 300 credits/month (~15 GB), and proxied API traffic burns it. |
| **Vercel Hobby** | Proxies fine, but Hobby ToS is non-commercial — wrong for a SaaS. |
| **Fly.io / Railway** | No longer free (card + spend). |
| **Render free Postgres** | Deleted after 30 days on free tier. |
| **Google Cloud Run** | Scales to zero between requests — background scheduler (the monitoring engine) doesn't run. Wrong shape for this app. |
| **Koyeb** free instance | Also scale-to-zero; same cold-start dance as Render with a smaller ecosystem. Fine as a Render alternative if Render changes terms. |

## Appendix — fully self-hosted $0 variant (advanced)

If you'd rather own the whole box: **Oracle Cloud Always Free** gives real
always-on VMs (4 ARM cores / 24 GB) with no monthly fee — but signup requires
a credit card and regions are often "out of capacity". If you get one:

1. Install Docker, clone the repo, create `.env` from `.env.example`
   (`SAAS_MODE=true`, `SESSION_COOKIE_SECURE=true`,
   `ALLOW_PRIVATE_TARGETS=false`, strong `POSTGRES_PASSWORD`,
   `SEED_SELF_URL=https://<your-domain>/health`).
2. `docker compose up -d --build` runs Postgres + monitor + dashboard
   (nginx, same-origin proxy already wired) + heartbeatchecker.
3. Expose it safely with **Cloudflare Tunnel** (`cloudflared`, free) instead
   of opening firewall ports — you get HTTPS and a hostname with no inbound
   exposure. Point the tunnel at the dashboard container's port
   (`DASHBOARD_PORT`, default 8090). No keep-alive hack needed on a VM.

Everything else (env vars, seeding, verification) is identical to Parts 1–5.
