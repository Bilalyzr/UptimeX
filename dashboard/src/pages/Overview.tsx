import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, buildQuery } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { EngineStats, Incident, OverviewMetrics } from '../types';
import { formatAge, formatDuration, formatMs, formatPct } from '../format';
import { DistributionBars, LatencyTrend } from '../components/charts';
import { ErrorBanner, WindowPicker } from '../components/common';
import { IconAlert, IconGrid, IconGlobe, IconMonitor } from '../components/icons';

const REFRESH_MS = 10_000;

function verdict(census: OverviewMetrics['endpoints']): { label: string; tone: 'ok' | 'warn' | 'down' } {
  if (census.down > 0) return { label: `Service disruption — ${census.down} endpoint${census.down > 1 ? 's' : ''} down`, tone: 'down' };
  if (census.failing > 0) return { label: `Degraded — ${census.failing} endpoint${census.failing > 1 ? 's' : ''} failing`, tone: 'warn' };
  return { label: 'All systems operational', tone: 'ok' };
}

function uptimeTone(pct: number): string {
  if (pct >= 99.5) return 'var(--green)';
  if (pct >= 95) return 'var(--amber)';
  return 'var(--red)';
}

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
          {/* Uptime hero: tick-ring | verdict + census | stat rail */}
          <div className="card ov-hero">
            <div
              className="uptime-ring"
              style={{
                background: `conic-gradient(${uptimeTone(ov.uptime_pct)} ${Math.max(ov.uptime_pct, 0) * 3.6}deg, var(--surface-2) 0deg)`,
              }}
              role="img"
              aria-label={`Uptime ${formatPct(ov.uptime_pct)} in the last ${ov.window}`}
            >
              <div className="uptime-ring-inner">
                <strong>{formatPct(ov.uptime_pct)}</strong>
                <span>uptime {ov.window}</span>
              </div>
            </div>

            <div className="ov-hero-main">
              <div className={`ov-verdict ov-verdict-${verdict(ov.endpoints).tone}`}>
                <span className="live-dot" />
                {verdict(ov.endpoints).label}
              </div>
              <div className="census">
                <div className="census-bar" aria-label="Endpoint health breakdown">
                  {ov.endpoints.total === 0 ? (
                    <span className="census-seg census-seg-none" style={{ width: '100%' }} />
                  ) : (
                    <>
                      {ov.endpoints.healthy > 0 && (
                        <span className="census-seg census-seg-healthy" style={{ width: `${(ov.endpoints.healthy / ov.endpoints.total) * 100}%` }} />
                      )}
                      {ov.endpoints.failing > 0 && (
                        <span className="census-seg census-seg-failing" style={{ width: `${(ov.endpoints.failing / ov.endpoints.total) * 100}%` }} />
                      )}
                      {ov.endpoints.down > 0 && (
                        <span className="census-seg census-seg-down" style={{ width: `${(ov.endpoints.down / ov.endpoints.total) * 100}%` }} />
                      )}
                      {ov.endpoints.disabled > 0 && (
                        <span className="census-seg census-seg-disabled" style={{ width: `${(ov.endpoints.disabled / ov.endpoints.total) * 100}%` }} />
                      )}
                    </>
                  )}
                </div>
                <div className="census-legend">
                  <span><i className="census-dot census-seg-healthy" /> {ov.endpoints.healthy} healthy</span>
                  <span><i className="census-dot census-seg-failing" /> {ov.endpoints.failing} failing</span>
                  <span><i className="census-dot census-seg-down" /> {ov.endpoints.down} down</span>
                  <span><i className="census-dot census-seg-disabled" /> {ov.endpoints.disabled} disabled</span>
                </div>
              </div>
            </div>

            <div className="ov-hero-stats">
              <div className="ov-stat">
                <span className="ov-stat-label">Checks</span>
                <strong>{ov.total_checks}</strong>
                <span className="ov-stat-sub">{ov.latency.sample_count} latency samples</span>
              </div>
              <div className="ov-stat">
                <span className="ov-stat-label">Failure rate</span>
                <strong style={{ color: ov.failure_rate_pct > 5 ? 'var(--red)' : ov.failure_rate_pct > 1 ? 'var(--amber)' : 'var(--green)' }}>
                  {formatPct(ov.failure_rate_pct)}
                </strong>
                <span className="ov-stat-sub">{ov.failed_checks} failed checks</span>
              </div>
              <div className="ov-stat">
                <span className="ov-stat-label">P95 latency</span>
                <strong>{formatMs(ov.latency.p95_ms)}</strong>
                <span className="ov-stat-sub">P99 {formatMs(ov.latency.p99_ms)}</span>
              </div>
            </div>
          </div>

          <div className="ov-charts">
            <div className="card">
              <h3 className="profile-sec-title">
                <span className="profile-sec-icon"><IconGlobe size={16} /></span>
                Response-time trend
                <span className="ov-window-chip mono">{ov.window}</span>
              </h3>
              <LatencyTrend trend={ov.latency_trend} />
            </div>
            <div className="card">
              <h3 className="profile-sec-title">
                <span className="profile-sec-icon"><IconGrid size={16} /></span>
                Status codes
              </h3>
              <DistributionBars distribution={ov.status_distribution} />
            </div>
          </div>

          {/* Latency percentiles as comparative bars */}
          <div className="card" style={{ marginTop: 16 }}>
            <h3 className="profile-sec-title">
              <span className="profile-sec-icon"><IconAlert size={16} /></span>
              Latency percentiles
              <span className="spacer" />
              <span className="muted mono" style={{ fontSize: 11 }}>
                min {formatMs(ov.latency.min_ms)} · max {formatMs(ov.latency.max_ms)}
              </span>
            </h3>
            <div className="pct-bars">
              {([
                ['P50 · median', ov.latency.p50_ms, 'var(--green)'],
                ['P95 · tail starts', ov.latency.p95_ms, 'var(--amber)'],
                ['P99 · worst 1%', ov.latency.p99_ms, 'var(--red)'],
                ['Average', ov.latency.avg_ms, 'rgba(53, 53, 57, 0.55)'],
              ] as Array<[string, number, string]>).map(([label, ms, color]) => {
                const max = Math.max(ov.latency.p99_ms, ov.latency.avg_ms, 1);
                return (
                  <div className="pct-row" key={label}>
                    <span className="pct-label">{label}</span>
                    <div className="pct-track">
                      <span
                        className="pct-fill"
                        style={{ width: `${Math.max((ms / max) * 100, ms > 0 ? 2 : 0)}%`, background: color }}
                      />
                    </div>
                    <span className="pct-value mono">{formatMs(ms)}</span>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Open incidents */}
          <div className="card" style={{ marginTop: 16 }}>
            <h3 className="profile-sec-title">
              <span className="profile-sec-icon"><IconAlert size={16} /></span>
              Open incidents
              {incidents.data && incidents.data.incidents.length > 0 && (
                <span className="count-chip">{incidents.data.incidents.length}</span>
              )}
              <span className="spacer" />
              <Link to="/app/incidents">All incidents →</Link>
            </h3>
            {incidents.loading ? (
              <div className="loading skeleton" />
            ) : (incidents.data?.incidents.length ?? 0) === 0 ? (
              <div className="empty">
                <span className="profile-sec-icon" style={{ margin: '0 auto 8px' }}><IconAlert size={16} /></span>
                No open incidents — every endpoint is past its failure threshold.
              </div>
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

          {/* Monitor self-observability (operator-only API) */}
          <div className="card" style={{ marginTop: 16 }}>
            <h3 className="profile-sec-title">
              <span className="profile-sec-icon"><IconMonitor size={16} /></span>
              Monitor self-observability
            </h3>
            {stats.error ? (
              <div className="empty">Engine stats span every tenant and are available to operators only.</div>
            ) : stats.data ? (
              <div className="grid grid-cards">
                <div className="ov-mini">
                  <span className="ov-mini-label">Checks attempted</span>
                  <strong className="mono">{stats.data.checks_attempted}</strong>
                  <span className="muted">since {formatAge(stats.data.started_at)}</span>
                </div>
                <div className="ov-mini">
                  <span className="ov-mini-label">Succeeded</span>
                  <strong className="mono" style={{ color: 'var(--green)' }}>{stats.data.checks_succeeded}</strong>
                </div>
                <div className="ov-mini">
                  <span className="ov-mini-label">Failed</span>
                  <strong className="mono" style={{ color: 'var(--red)' }}>{stats.data.checks_failed}</strong>
                </div>
                <div className="ov-mini">
                  <span className="ov-mini-label">Worker pool</span>
                  <strong className="mono">{stats.data.pool.active_workers}/{stats.data.pool.workers}</strong>
                  <span className="muted">active / total</span>
                </div>
                <div className="ov-mini">
                  <span className="ov-mini-label">Queue depth</span>
                  <strong className="mono">{stats.data.pool.queue_depth}</strong>
                  <span className="muted">{stats.data.scheduler.duplicates_skipped} duplicates prevented</span>
                </div>
              </div>
            ) : (
              <div className="loading skeleton" />
            )}
          </div>
        </>
      ) : (
        <div className="loading skeleton" />
      )}
    </>
  );
}
