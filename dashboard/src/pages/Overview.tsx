import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, buildQuery } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { EngineStats, Incident, OverviewMetrics } from '../types';
import { formatAge, formatDuration, formatMs, formatPct } from '../format';
import { DistributionBars, LatencyTrend } from '../components/charts';
import { ErrorBanner, StatCard, WindowPicker } from '../components/common';

const REFRESH_MS = 10_000;

export function Overview() {
  const [window, setWindow] = useState('1h');
  const overview = usePolling<OverviewMetrics>(
    () => api.get(`/api/v1/metrics/overview${buildQuery({ window })}`),
    REFRESH_MS,
  );
  const incidents = usePolling<{ incidents: Incident[] }>(
    () => api.get('/api/v1/incidents?status=open&limit=8'),
    REFRESH_MS,
  );
  const stats = usePolling<EngineStats>(() => api.get('/api/v1/stats'), REFRESH_MS);

  const ov = overview.data;

  return (
    <>
      <div className="page-header">
        <div>
          <h1 className="page-title">Overview</h1>
          <div className="subtitle">
            Global monitoring health · window {ov?.window ?? window} · from {ov ? formatAge(ov.from) : '…'} to now
          </div>
        </div>
        <WindowPicker value={window} onChange={setWindow} />
      </div>

      {overview.error ? <ErrorBanner message={overview.error} /> : null}

      {ov ? (
        <>
          <div className="grid grid-cards">
            <StatCard label="Endpoints" value={ov.endpoints.total} hint={`${ov.endpoints.disabled} disabled`} />
            <StatCard label="Healthy" value={ov.endpoints.healthy} color="var(--green)" />
            <StatCard label="Failing" value={ov.endpoints.failing} color="var(--amber)" hint="below threshold" />
            <StatCard label="Down" value={ov.endpoints.down} color="var(--red)" hint="incident open" />
            <StatCard label="Uptime" value={formatPct(ov.uptime_pct)} hint={`${ov.total_checks} checks in window`} />
            <StatCard label="Open incidents" value={ov.open_incidents} color={ov.open_incidents > 0 ? 'var(--red)' : undefined} />
          </div>

          <div className="grid grid-cards" style={{ marginTop: 14 }}>
            <StatCard label="P50 latency" value={formatMs(ov.latency.p50_ms)} hint="median" />
            <StatCard label="P95 latency" value={formatMs(ov.latency.p95_ms)} hint="tail starts" />
            <StatCard label="P99 latency" value={formatMs(ov.latency.p99_ms)} color="var(--red)" hint={`worst 1% · n=${ov.latency.sample_count}`} />
            <StatCard label="Avg latency" value={formatMs(ov.latency.avg_ms)} hint={`min ${formatMs(ov.latency.min_ms)} / max ${formatMs(ov.latency.max_ms)}`} />
            <StatCard label="Failure rate" value={formatPct(ov.failure_rate_pct)} hint={`${ov.failed_checks} failed checks`} />
          </div>

          <div className="grid" style={{ gridTemplateColumns: '2fr 1fr', marginTop: 14 }}>
            <div className="card">
              <h3 className="card-title">Response-time trend (window {ov.window})</h3>
              <LatencyTrend trend={ov.latency_trend} />
            </div>
            <div className="card">
              <h3 className="card-title">Status-code distribution</h3>
              <DistributionBars distribution={ov.status_distribution} />
            </div>
          </div>

          <div className="card" style={{ marginTop: 14 }}>
            <div className="row">
              <h3 className="card-title">Open incidents</h3>
              <span className="spacer" />
              <Link to="/app/incidents">All incidents →</Link>
            </div>
            {incidents.loading ? (
              <div className="loading">Loading…</div>
            ) : (incidents.data?.incidents.length ?? 0) === 0 ? (
              <div className="empty">No open incidents. 🎉</div>
            ) : (
              <table>
                <thead>
                  <tr>
                    <th>Endpoint</th>
                    <th>Opened</th>
                    <th>Duration</th>
                    <th className="num">Failures</th>
                    <th>Last error</th>
                  </tr>
                </thead>
                <tbody>
                  {incidents.data!.incidents.map((inc) => (
                    <tr key={inc.id}>
                      <td>
                        <Link to={`/app/endpoints/${inc.endpoint_id}`} className="endpoint-name">
                          {inc.endpoint_name}
                        </Link>
                      </td>
                      <td>{formatAge(inc.opened_at)}</td>
                      <td>{formatDuration(Date.now() - new Date(inc.opened_at).getTime())}</td>
                      <td className="num">{inc.failure_count}</td>
                      <td className="muted mono">{inc.last_error || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          <div className="card" style={{ marginTop: 14 }}>
            <h3 className="card-title">Monitor self-observability</h3>
            {stats.data ? (
              <div className="grid grid-cards">
                <StatCard label="Checks attempted" value={stats.data.checks_attempted} hint={`since ${formatAge(stats.data.started_at)}`} />
                <StatCard label="Checks succeeded" value={stats.data.checks_succeeded} color="var(--green)" />
                <StatCard label="Checks failed" value={stats.data.checks_failed} color="var(--red)" />
                <StatCard label="Worker pool" value={`${stats.data.pool.active_workers}/${stats.data.pool.workers}`} hint="active / total workers" />
                <StatCard label="Queue depth" value={stats.data.pool.queue_depth} hint={`${stats.data.scheduler.duplicates_skipped} duplicate jobs prevented`} />
              </div>
            ) : (
              <div className="loading">Loading…</div>
            )}
          </div>
        </>
      ) : (
        <div className="loading">Loading metrics…</div>
      )}
    </>
  );
}
