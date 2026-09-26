// Public status page: /status/:slug — the tenant's shared health view.
// Auto-refreshes every 30 seconds; fully anonymous.
import { useCallback } from 'react';
import { useParams } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { PublicStatus } from '../types';
import { formatDuration } from '../format';
import { Logo } from '../components/Logo';

type StatusResult = { status: PublicStatus } | { notFound: true };

export function PublicStatus() {
  const { slug } = useParams<{ slug: string }>();

  const fetcher = useCallback(async (): Promise<StatusResult> => {
    if (!slug) return { notFound: true };
    try {
      return { status: await api.get<PublicStatus>(`/api/v1/public/status/${encodeURIComponent(slug)}`) };
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return { notFound: true };
      throw e;
    }
  }, [slug]);

  const { data, error } = usePolling(fetcher, 30_000);

  if (data && 'notFound' in data) {
    return (
      <div className="status-page">
        <div className="status-box">
          <div className="status-head">
            <h1>Status page not found</h1>
          </div>
          <p className="muted">This status page doesn&apos;t exist or has been disabled by its owner.</p>
        </div>
      </div>
    );
  }
  if (error) {
    return (
      <div className="status-page">
        <div className="status-box">
          <div className="error-banner">Failed to load status: {error}</div>
        </div>
      </div>
    );
  }
  if (!data) return <div className="status-page"><div className="loading muted">Loading status…</div></div>;

  const status = data.status;
  const overallClass =
    status.overall.state === 'HEALTHY' ? 'ok' : status.overall.state === 'FAILING' ? 'warn' : 'down';
  const downCount = status.services.filter((s) => s.state === 'DOWN').length;

  return (
    <div className="status-page">
      <div className="status-box">
        <div className="status-head">
          <div>
            <div className="status-brand">
              <Logo size={22} /> UptimeX
            </div>
            <h1>{status.organization.name}</h1>
          </div>
          <div className={`status-banner status-banner-${overallClass}`}>
            {status.overall.state === 'HEALTHY'
              ? downCount === 0
                ? 'All systems operational'
                : 'Degraded performance'
              : status.overall.state === 'FAILING'
                ? 'Partial degradation'
                : 'Service disruption'}
          </div>
        </div>

        <div className="status-meta muted">
          {typeof status.overall.uptime_pct === 'number' && (
            <span>24h uptime {status.overall.uptime_pct.toFixed(2)}%</span>
          )}
          {typeof status.overall.p95_ms === 'number' && <span>· P95 {Math.round(status.overall.p95_ms)}ms</span>}
          <span>· updated {new Date(status.generated_at).toLocaleTimeString()}</span>
        </div>

        {status.open_incidents.length > 0 && (
          <div className="status-incidents">
            <h2>Open incidents</h2>
            {status.open_incidents.map((i) => (
              <div className="status-incident" key={i.id}>
                <span className="badge badge-OPEN">OPEN</span>
                <strong>{i.endpoint_name}</strong>
                <span className="muted">since {new Date(i.opened_at).toLocaleString()}</span>
              </div>
            ))}
          </div>
        )}

        <table>
          <thead>
            <tr>
              <th>Service</th>
              <th>Status</th>
              <th className="num">24h uptime</th>
              <th className="num">P95</th>
              <th className="num">Last check</th>
            </tr>
          </thead>
          <tbody>
            {status.services.map((s) => (
              <tr key={s.id}>
                <td className="endpoint-name">{s.name}</td>
                <td>
                  <span className={`badge badge-${s.state}`}>{s.state}</span>
                </td>
                <td className="num">{s.uptime_pct != null ? `${s.uptime_pct.toFixed(2)}%` : '—'}</td>
                <td className="num">{s.p95_ms != null ? `${Math.round(s.p95_ms)}ms` : '—'}</td>
                <td className="num muted">
                  {s.last_checked_at
                    ? formatDuration(Date.now() - new Date(s.last_checked_at).getTime()) + ' ago'
                    : 'pending'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {status.services.length === 0 && (
          <div className="empty">
            <p>No services are being monitored yet.</p>
          </div>
        )}
      </div>
    </div>
  );
}
