// Billing & plans: current plan, usage against quotas, and plan switching.
// Plan changes apply immediately (demo billing); a production deployment
// swaps the switch button for a Stripe checkout session.
import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import type { OrgResponse } from '../types';

export function Billing() {
  const { me, refresh } = useAuth();
  const [data, setData] = useState<OrgResponse | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busyPlan, setBusyPlan] = useState('');

  const load = useCallback(() => {
    api
      .get<OrgResponse>('/api/v1/org')
      .then((d) => {
        setData(d);
        setError('');
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, []);

  useEffect(load, [load]);

  const changePlan = (planId: string) => {
    setBusyPlan(planId);
    setNotice('');
    api
      .post<{ org: OrgResponse['org'] }>('/api/v1/org/plan', { plan_id: planId })
      .then(() => {
        setNotice('Plan updated.');
        load();
        void refresh();
      })
      .catch((e) => setNotice(e instanceof ApiError ? e.message : String(e)))
      .finally(() => setBusyPlan(''));
  };

  if (!me) {
    return (
      <div className="empty">
        <h2>Billing lives with your account</h2>
        <p className="muted">
          This deployment runs without a signed-in user. <Link to="/login">Sign in</Link> to manage
          a workspace plan.
        </p>
      </div>
    );
  }
  if (error) return <div className="error-banner">Failed to load billing: {error}</div>;
  if (!data) return <div className="loading muted">Loading billing…</div>;

  const currentPlan = data.plans.find((p) => p.id === data.org.plan);
  const used = data.usage.endpoints;
  const limit = currentPlan?.max_endpoints ?? 0;
  const pct = Math.min(100, limit === 0 ? 0 : Math.round((used / limit) * 100));

  return (
    <div>
      <div className="page-header">
        <div>
          <h2 className="page-title">Billing &amp; plans</h2>
          <span className="subtitle">
            {data.org.name} · status page /status/{data.org.slug} · demo billing mode
          </span>
        </div>
      </div>

      <div className="card" style={{ marginBottom: 16 }}>
        <div className="row">
          <div>
            <div className="muted" style={{ fontSize: 12, textTransform: 'uppercase', letterSpacing: 0.6 }}>
              Current plan
            </div>
            <div style={{ fontSize: 22, fontWeight: 700 }}>{currentPlan?.name ?? data.org.plan}</div>
          </div>
          <div className="spacer" />
          <div style={{ minWidth: 260 }}>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <span className="muted">Endpoints</span>
              <span>
                <strong>{used}</strong> / {limit}
              </span>
            </div>
            <div className="usage-bar">
              <div className={`usage-fill${pct >= 100 ? ' usage-full' : ''}`} style={{ width: `${pct}%` }} />
            </div>
            {pct >= 80 && (
              <div className="muted" style={{ fontSize: 12 }}>
                {pct >= 100 ? 'Quota reached — upgrade to add more.' : 'Approaching your plan limit.'}
              </div>
            )}
          </div>
        </div>
        {notice && <div className="error-banner" style={{ marginTop: 12 }}>{notice}</div>}
      </div>

      <div className="pricing-grid pricing-inline">
        {data.plans.map((p) => {
          const isCurrent = p.id === data.org.plan;
          return (
            <div className={`price-card${isCurrent ? ' price-card-hot' : ''}`} key={p.id}>
              {isCurrent && <div className="price-flag">Current plan</div>}
              <h3>{p.name}</h3>
              <div className="price-amount">
                ₹{(p.price_monthly_cents / 100).toLocaleString('en-IN')}{' '}
                <span>{p.price_monthly_cents === 0 ? 'forever' : 'per month'}</span>
              </div>
              <ul>
                {p.features.map((f) => (
                  <li key={f}>{f}</li>
                ))}
              </ul>
              <button
                className={`btn ${isCurrent ? '' : 'btn-primary'}`}
                disabled={isCurrent || busyPlan !== ''}
                onClick={() => changePlan(p.id)}
              >
                {isCurrent ? 'Active' : busyPlan === p.id ? 'Switching…' : `Switch to ${p.name}`}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
