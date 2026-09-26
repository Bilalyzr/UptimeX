import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api, buildQuery } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { EndpointMetrics, HealthCheck } from '../types';
import { formatAge, formatMs, formatPct } from '../format';
import { DistributionBars, LatencyTrend } from '../components/charts';
import { Badge, ErrorBanner, Loading, StatCard, WindowPicker } from '../components/common';

const REFRESH_MS = 10_000;

export function EndpointDetail() {
  const { id } = useParams<{ id: string }>();
  const [window, setWindow] = useState('24h');

  const detail = usePolling<{
    endpoint: { id: number; name: string; url: string; method: string; interval_seconds: number; timeout_ms: number; failure_threshold: number; enabled: boolean };
    status?: { state: string; consecutive_failures: number; last_checked_at: string | null; last_success_at: string | null };
    open_incident?: { id: number; opened_at: string; failure_count: number; last_error: string };
  }>(() => api.get(`/api/v1/endpoints/${id}`), REFRESH_MS);

  const metrics = usePolling<EndpointMetrics>(
    () => api.get(`/api/v1/endpoints/${id}/metrics${buildQuery({ window })}`),
    REFRESH_MS,
  );
  const checks = usePolling<{ checks: HealthCheck[] }>(
    () => api.get(`/api/v1/endpoints/${id}/checks?limit=25`),
    REFRESH_MS,
  );

  if (detail.loading && !detail.data) return <Loading label="Loading endpoint…" />;
  if (detail.error) return <ErrorBanner message={detail.error} />;
  if (!detail.data) return null;

  const { endpoint: ep, status, open_incident } = detail.data;
  const m = metrics.data;

  return (
    <>
      <div className="page-header">
        <div>
          <div className="subtitle">
            <Link to="/endpoints">← Endpoints</Link>
          </div>
          <h1 className="page-title" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {ep.name} {status ? <Badge state={status.state} /> : null}
          </h1>
          <div className="subtitle mono">{ep.url}</div>
        </div>
        <WindowPicker value={window} onChange={setWindow} />
      </div>

      {open_incident ? (
        <div className="error-banner">
          <strong>Open incident</strong> since {formatAge(open_incident.opened_at)} · {open_incident.failure_count}{' '}
          consecutive failures · last error: <span className="mono">{open_incident.last_error || '—'}</span>
        </div>
      ) : null}

      <div className="grid grid-cards">
        <StatCard label="Uptime" value={formatPct(m?.uptime_pct)} hint={`${m?.total_checks ?? 0} checks in ${m?.window ?? window}`} />
        <StatCard label="P50" value={formatMs(m?.latency.p50_ms)} hint="median" />
        <StatCard label="P95" value={formatMs(m?.latency.p95_ms)} />
        <StatCard label="P99" value={formatMs(m?.latency.p99_ms)} color="var(--red)" hint={`n=${m?.latency.sample_count ?? 0}`} />
        <StatCard label="Fail streak" value={status?.consecutive_failures ?? 0} hint={`threshold ${ep.failure_threshold}`} />
        <StatCard label="Last check" value={formatAge(status?.last_checked_at)} hint={`interval ${ep.interval_seconds}s · timeout ${ep.timeout_ms}ms`} />
      </div>

      {metrics.error ? <ErrorBanner message={metrics.error} /> : null}

      <div className="grid" style={{ gridTemplateColumns: '2fr 1fr', marginTop: 14 }}>
        <div className="card">
          <h3 className="card-title">Response-time trend ({m?.window ?? window})</h3>
          {m ? <LatencyTrend trend={m.latency_trend} /> : <Loading />}
        </div>
        <div className="card">
          <h3 className="card-title">Status distribution</h3>
          {m ? <DistributionBars distribution={m.status_distribution} /> : <Loading />}
        </div>
      </div>

      <div className="card" style={{ marginTop: 14 }}>
        <h3 className="card-title">Recent checks (latest 25)</h3>
        {checks.data && checks.data.checks.length > 0 ? (
          <div className="checks-scroll">
            <table>
              <thead>
                <tr>
                  <th>Checked at</th>
                  <th className="num">Status</th>
                  <th className="num">Response</th>
                  <th>Result</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {checks.data.checks.map((c) => (
                  <tr key={c.id}>
                    <td>{new Date(c.checked_at).toLocaleString()}</td>
                    <td className="num">{c.status_code ?? '—'}</td>
                    <td className="num">{formatMs(c.response_time_ms)}</td>
                    <td>
                      {c.success ? (
                        <span className="badge badge-HEALTHY">OK</span>
                      ) : (
                        <span className="badge badge-DOWN">FAIL</span>
                      )}
                    </td>
                    <td className="muted mono">
                      {c.error_type ? `${c.error_type}: ${c.error_message ?? ''}` : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Loading label="No checks recorded yet…" />
        )}
      </div>
    </>
  );
}
