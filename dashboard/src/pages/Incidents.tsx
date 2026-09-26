import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api, buildQuery } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import type { Incident } from '../types';
import { formatAge, formatDuration } from '../format';
import { Badge, EmptyState, ErrorBanner, Loading } from '../components/common';

const REFRESH_MS = 10_000;

export function Incidents() {
  const [status, setStatus] = useState('all');
  const list = usePolling<{ incidents: Incident[] }>(
    () => api.get(`/api/v1/incidents${buildQuery({ status, limit: '100' })}`),
    REFRESH_MS,
  );

  const incidents = list.data?.incidents ?? [];

  return (
    <>
      <div className="page-header">
        <div>
          <h1 className="page-title">Incidents</h1>
          <div className="subtitle">One incident per sustained outage; resolved automatically on recovery</div>
        </div>
        <label className="row" style={{ gap: 6 }}>
          <span className="muted" style={{ fontSize: 12 }}>
            Status
          </span>
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="all">All</option>
            <option value="open">Open</option>
            <option value="resolved">Resolved</option>
          </select>
        </label>
      </div>

      {list.error ? <ErrorBanner message={list.error} /> : null}

      <div className="card">
        {list.loading && incidents.length === 0 ? (
          <Loading />
        ) : incidents.length === 0 ? (
          <EmptyState title="No incidents" hint="Sustained outages will appear here." />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <thead>
                <tr>
                  <th>Endpoint</th>
                  <th>Status</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th className="num">Failures</th>
                  <th>Last error</th>
                </tr>
              </thead>
              <tbody>
                {incidents.map((inc) => {
                  const end = inc.resolved_at ? new Date(inc.resolved_at).getTime() : Date.now();
                  const duration = end - new Date(inc.opened_at).getTime();
                  return (
                    <tr key={inc.id}>
                      <td>
                        <Link to={`/app/endpoints/${inc.endpoint_id}`} className="endpoint-name">
                          {inc.endpoint_name}
                        </Link>
                        <span className="endpoint-url">{inc.endpoint_url}</span>
                      </td>
                      <td>
                        <Badge state={inc.status} />
                      </td>
                      <td>{formatAge(inc.opened_at)}</td>
                      <td>{inc.status === 'OPEN' ? `${formatDuration(duration)} (ongoing)` : formatDuration(duration)}</td>
                      <td className="num">{inc.failure_count}</td>
                      <td className="muted mono" style={{ maxWidth: 320, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {inc.last_error || '—'}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </>
  );
}
