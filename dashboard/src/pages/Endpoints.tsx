import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, ApiError, buildQuery } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { CheckResult, EndpointSummary } from '../types';
import { formatAge, formatMs, formatPct } from '../format';
import { Badge, EmptyState, ErrorBanner, Loading, WindowPicker } from '../components/common';
import { EndpointForm } from '../components/EndpointForm';

const REFRESH_MS = 10_000;

export function Endpoints() {
  const [windowParam, setWindowParam] = useState('1h');
  const [editing, setEditing] = useState<EndpointSummary | null>(null);
  const [creating, setCreating] = useState(false);
  const [testResult, setTestResult] = useState<{ name: string; result: CheckResult } | null>(null);
  const [busy, setBusy] = useState<number | null>(null);

  const list = usePolling<{ endpoints: EndpointSummary[]; window?: string }>(
    () => api.get(`/api/v1/endpoints${buildQuery({ window: windowParam })}`),
    REFRESH_MS,
  );

  const runTest = async (ep: EndpointSummary) => {
    setBusy(ep.id);
    setTestResult(null);
    try {
      const result = await api.post<CheckResult>(`/api/v1/endpoints/${ep.id}/test`);
      setTestResult({ name: ep.name, result });
    } catch (err) {
      setTestResult({
        name: ep.name,
        result: {
          endpoint_id: ep.id,
          checked_at: new Date().toISOString(),
          status_code: null,
          response_time_ms: 0,
          success: false,
          error_type: 'api',
          error_message: err instanceof ApiError ? err.message : String(err),
        },
      });
    } finally {
      setBusy(null);
    }
  };

  const toggleEnabled = async (ep: EndpointSummary) => {
    setBusy(ep.id);
    try {
      await api.patch(`/api/v1/endpoints/${ep.id}`, { enabled: !ep.enabled });
      await list.refresh();
    } catch (err) {
      alert(`Update failed: ${err instanceof ApiError ? err.message : err}`);
    } finally {
      setBusy(null);
    }
  };

  const remove = async (ep: EndpointSummary) => {
    if (!window.confirm(`Delete endpoint "${ep.name}" and all its history?`)) return;
    setBusy(ep.id);
    try {
      await api.del(`/api/v1/endpoints/${ep.id}`);
      await list.refresh();
    } catch (err) {
      alert(`Delete failed: ${err instanceof ApiError ? err.message : err}`);
    } finally {
      setBusy(null);
    }
  };

  const endpoints = list.data?.endpoints ?? [];

  return (
    <>
      <div className="page-header">
        <div>
          <h1 className="page-title">Endpoints</h1>
          <div className="subtitle">
            {endpoints.length} registered · metrics window {list.data?.window ?? windowParam}
          </div>
        </div>
        <div className="row">
          <WindowPicker value={windowParam} onChange={setWindowParam} />
          <button className="btn-primary" onClick={() => setCreating(true)}>
            + Register endpoint
          </button>
        </div>
      </div>

      {list.error ? <ErrorBanner message={list.error} /> : null}
      {testResult ? (
        <div className="card" style={{ marginBottom: 14 }}>
          <div className="row">
            <strong>On-demand check: {testResult.name}</strong>
            <span className={testResult.result.success ? 'badge badge-HEALTHY' : 'badge badge-DOWN'}>
              {testResult.result.success ? 'SUCCESS' : 'FAILURE'}
            </span>
            {testResult.result.status_code !== null ? <span className="muted">HTTP {testResult.result.status_code}</span> : null}
            <span className="muted">{formatMs(testResult.result.response_time_ms)}</span>
            {testResult.result.error_type ? <span className="mono muted">{testResult.result.error_type}: {testResult.result.error_message}</span> : null}
            <span className="spacer" />
            <button className="btn-sm" onClick={() => setTestResult(null)}>
              dismiss
            </button>
          </div>
          <div className="hint muted" style={{ marginTop: 4, fontSize: 12 }}>
            Manual checks report the raw probe result; they do not affect the failure state machine.
          </div>
        </div>
      ) : null}

      <div className="card">
        {list.loading && endpoints.length === 0 ? (
          <Loading />
        ) : endpoints.length === 0 ? (
          <EmptyState
            title="No endpoints registered"
            hint='Click "+ Register endpoint" to start monitoring an HTTP/HTTPS target.'
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <thead>
                <tr>
                  <th>Endpoint</th>
                  <th>Status</th>
                  <th className="num">Uptime</th>
                  <th className="num">P99</th>
                  <th className="num">Avg</th>
                  <th>Last check</th>
                  <th className="num">Fail streak</th>
                  <th className="num">Actions</th>
                </tr>
              </thead>
              <tbody>
                {endpoints.map((ep) => (
                  <tr key={ep.id} style={{ opacity: ep.enabled ? 1 : 0.55 }}>
                    <td>
                      <Link to={`/endpoints/${ep.id}`} className="endpoint-name">
                        {ep.name}
                      </Link>
                      <span className="endpoint-url">{ep.url}</span>
                    </td>
                    <td>
                      {ep.enabled ? (
                        <Badge state={ep.state} />
                      ) : (
                        <span className="badge badge-disabled">PAUSED</span>
                      )}
                    </td>
                    <td className="num">{formatPct(ep.uptime_pct)}</td>
                    <td className="num">{formatMs(ep.p99_ms)}</td>
                    <td className="num">{formatMs(ep.avg_ms)}</td>
                    <td>{formatAge(ep.last_checked_at)}</td>
                    <td className="num">{ep.consecutive_failures}</td>
                    <td className="num" style={{ whiteSpace: 'nowrap' }}>
                      <button className="btn-sm" disabled={busy === ep.id} onClick={() => void runTest(ep)}>
                        Test
                      </button>{' '}
                      <button className="btn-sm" disabled={busy === ep.id} onClick={() => setEditing(ep)}>
                        Edit
                      </button>{' '}
                      <button className="btn-sm" disabled={busy === ep.id} onClick={() => void toggleEnabled(ep)}>
                        {ep.enabled ? 'Pause' : 'Resume'}
                      </button>{' '}
                      <button className="btn-sm btn-danger" disabled={busy === ep.id} onClick={() => void remove(ep)}>
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {creating ? (
        <EndpointForm
          onCancel={() => setCreating(false)}
          onDone={() => {
            setCreating(false);
            void list.refresh();
          }}
        />
      ) : null}
      {editing ? (
        <EndpointForm
          endpoint={editing}
          onCancel={() => setEditing(null)}
          onDone={() => {
            setEditing(null);
            void list.refresh();
          }}
        />
      ) : null}
    </>
  );
}
