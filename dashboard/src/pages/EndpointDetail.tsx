import { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { api, ApiError, buildQuery } from '../api/client';
import { useAuth } from '../auth';
import { usePolling } from '../hooks/usePolling';
import type { CheckResult, Endpoint, EndpointMetrics, HealthCheck } from '../types';
import { formatAge, formatMs, formatPct } from '../format';
import { DistributionBars, LatencyTrend } from '../components/charts';
import { Badge, EmptyState, ErrorBanner, Loading, StatCard, WindowPicker } from '../components/common';
import { EndpointForm } from '../components/EndpointForm';

const REFRESH_MS = 10_000;

export function EndpointDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { refresh: refreshAuth } = useAuth();
  const [windowParam, setWindowParam] = useState('24h');
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState('');
  const [testResult, setTestResult] = useState<CheckResult | null>(null);

  const detail = usePolling<{
    endpoint: Endpoint;
    status?: {
      state: string;
      consecutive_failures: number;
      last_checked_at: string | null;
      last_success_at: string | null;
    };
    open_incident?: { id: number; opened_at: string; failure_count: number; last_error: string };
  }>(() => api.get(`/api/v1/endpoints/${id}`), REFRESH_MS);

  const metrics = usePolling<EndpointMetrics>(
    () => api.get(`/api/v1/endpoints/${id}/metrics${buildQuery({ window: windowParam })}`),
    REFRESH_MS,
  );
  const checks = usePolling<{ checks: HealthCheck[] }>(
    () =>
      api.get(
        `/api/v1/endpoints/${id}/checks${buildQuery({ limit: '25', since: windowParam })}`,
      ),
    REFRESH_MS,
  );

  if (detail.loading && !detail.data) return <Loading label="Loading endpoint…" />;
  if (detail.error) return <ErrorBanner message={detail.error} />;
  if (!detail.data) return null;

  const { endpoint: ep, status, open_incident } = detail.data;
  const m = metrics.data;

  const runTest = async () => {
    setBusy(true);
    setActionError('');
    setTestResult(null);
    try {
      const res = await api.post<CheckResult>(`/api/v1/endpoints/${ep.id}/test`);
      setTestResult(res);
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const toggleEnabled = async () => {
    setBusy(true);
    setActionError('');
    try {
      await api.patch(`/api/v1/endpoints/${ep.id}`, { enabled: !ep.enabled });
      await detail.refresh();
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!window.confirm(`Delete endpoint "${ep.name}" and all its history?`)) return;
    setBusy(true);
    setActionError('');
    try {
      await api.del(`/api/v1/endpoints/${ep.id}`);
      void refreshAuth();
      navigate('/app/endpoints');
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : String(err));
      setBusy(false);
    }
  };

  return (
    <>
      <div className="page-header">
        <div>
          <div className="subtitle">
            <Link to="/app/endpoints">← Endpoints</Link>
          </div>
          <h1 className="page-title" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {ep.name}{' '}
            {ep.enabled ? (
              status ? (
                <Badge state={status.state} />
              ) : null
            ) : (
              <span className="badge badge-disabled">PAUSED</span>
            )}
          </h1>
          <div className="subtitle mono">
            {ep.method} {ep.url}
          </div>
        </div>
        <div className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
          <WindowPicker value={windowParam} onChange={setWindowParam} />
          <button className="btn btn-sm" disabled={busy} onClick={() => void runTest()}>
            Test now
          </button>
          <button className="btn btn-sm" disabled={busy} onClick={() => setEditing(true)}>
            Edit
          </button>
          <button className="btn btn-sm" disabled={busy} onClick={() => void toggleEnabled()}>
            {ep.enabled ? 'Pause' : 'Resume'}
          </button>
          <button className="btn btn-sm btn-danger" disabled={busy} onClick={() => void remove()}>
            Delete
          </button>
        </div>
      </div>

      {actionError && <ErrorBanner message={actionError} />}

      {testResult && (
        <div className="card" style={{ marginBottom: 14 }}>
          <div className="row">
            <strong>On-demand check</strong>
            <span className={testResult.success ? 'badge badge-HEALTHY' : 'badge badge-DOWN'}>
              {testResult.success ? 'SUCCESS' : 'FAILURE'}
            </span>
            {testResult.status_code !== null && (
              <span className="muted">HTTP {testResult.status_code}</span>
            )}
            <span className="muted">{formatMs(testResult.response_time_ms)}</span>
            {testResult.error_type && (
              <span className="mono muted">
                {testResult.error_type}: {testResult.error_message}
              </span>
            )}
            <span className="spacer" />
            <button className="btn btn-sm" onClick={() => setTestResult(null)}>
              dismiss
            </button>
          </div>
        </div>
      )}

      {open_incident ? (
        <div className="error-banner">
          <strong>Open incident</strong> since {formatAge(open_incident.opened_at)} ·{' '}
          {open_incident.failure_count} consecutive failures · last error:{' '}
          <span className="mono">{open_incident.last_error || '—'}</span>
        </div>
      ) : null}

      <div className="grid grid-cards">
        <StatCard
          label="Uptime"
          value={formatPct(m?.uptime_pct)}
          hint={`${m?.total_checks ?? 0} checks in ${m?.window ?? windowParam}`}
        />
        <StatCard label="P50" value={formatMs(m?.latency.p50_ms)} hint="median" />
        <StatCard label="P95" value={formatMs(m?.latency.p95_ms)} />
        <StatCard
          label="P99"
          value={formatMs(m?.latency.p99_ms)}
          color="var(--red)"
          hint={`n=${m?.latency.sample_count ?? 0}`}
        />
        <StatCard
          label="Fail streak"
          value={status?.consecutive_failures ?? 0}
          hint={`threshold ${ep.failure_threshold}`}
        />
        <StatCard
          label="Last check"
          value={formatAge(status?.last_checked_at)}
          hint={`interval ${ep.interval_seconds}s · timeout ${ep.timeout_ms}ms`}
        />
      </div>

      {metrics.error ? <ErrorBanner message={metrics.error} /> : null}

      <div className="grid" style={{ gridTemplateColumns: '2fr 1fr', marginTop: 14 }}>
        <div className="card">
          <h3 className="card-title">Response-time trend ({m?.window ?? windowParam})</h3>
          {m ? <LatencyTrend trend={m.latency_trend} /> : <Loading />}
        </div>
        <div className="card">
          <h3 className="card-title">Status distribution</h3>
          {m ? <DistributionBars distribution={m.status_distribution} /> : <Loading />}
        </div>
      </div>

      <div className="card" style={{ marginTop: 14 }}>
        <h3 className="card-title">Recent checks (latest 25 in {windowParam})</h3>
        {checks.error ? (
          <ErrorBanner message={checks.error} />
        ) : checks.loading && !checks.data ? (
          <Loading label="Loading checks…" />
        ) : checks.data && checks.data.checks.length > 0 ? (
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
          <EmptyState
            title="No checks recorded in this window"
            hint="Scheduled probes will appear here automatically."
          />
        )}
      </div>

      {editing && (
        <EndpointForm
          endpoint={ep}
          onCancel={() => setEditing(false)}
          onDone={() => {
            setEditing(false);
            void detail.refresh();
            void metrics.refresh();
            void checks.refresh();
          }}
        />
      )}
    </>
  );
}
