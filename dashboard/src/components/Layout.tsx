import { Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { api, getStoredApiKey, setStoredApiKey } from '../api/client';
import type { Health } from '../types';
import { formatDuration } from '../format';
import { useAuth } from '../auth';
import { Logo } from './Logo';
import { BranchedMenu, type BranchedSection } from './BranchedMenu';
import { IconAlert, IconBell, IconCard, IconGlobe, IconGrid, IconUser } from './icons';

export function Layout() {
  const [health, setHealth] = useState<Health | null>(null);
  const [apiKeyDraft, setApiKeyDraft] = useState(() => getStoredApiKey());
  const [editingKey, setEditingKey] = useState(false);
  const [hasStoredKey, setHasStoredKey] = useState(() => getStoredApiKey() !== '');
  const { me, signOut } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const sections: BranchedSection[] = [
    {
      label: 'Monitor',
      children: [
        { value: '/app', label: 'Overview', icon: <IconGrid size={15} /> },
        { value: '/app/endpoints', label: 'Endpoints', icon: <IconGlobe size={15} /> },
        { value: '/app/incidents', label: 'Incidents', icon: <IconAlert size={15} /> },
        ...(me ? [{ value: '/app/alerts', label: 'Alerts', icon: <IconBell size={15} /> }] : []),
      ],
    },
    ...(me
      ? [
          {
            label: 'Account',
            children: [
              { value: '/app/billing', label: 'Billing & plans', icon: <IconCard size={15} /> },
              { value: '/app/profile', label: 'Profile', icon: <IconUser size={15} /> },
            ],
          },
        ]
      : []),
  ];

  useEffect(() => {
    const load = () =>
      api
        .get<Health>('/health')
        .then(setHealth)
        .catch(() => setHealth(null));
    void load();
    const id = window.setInterval(load, 10_000);
    return () => window.clearInterval(id);
  }, []);

  const onSignOut = async () => {
    await signOut();
    navigate('/', { replace: true });
  };

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <Logo size={26} wordmark />
        </div>

        <BranchedMenu sections={sections} activePath={location.pathname} />

        <div className="sidebar-footer">
          {me ? (
            <div className="user-card">
              <div className="user-email" title={me.user.email}>
                {me.user.email}
              </div>
              <div className="row" style={{ gap: 6 }}>
                <span className="plan-chip">{me.org.plan}</span>
                <span className="muted mono" style={{ fontSize: 11 }}>
                  {me.usage.endpoints}/{me.usage.max_endpoints ?? '∞'} monitors
                </span>
              </div>
              <button className="btn btn-sm" style={{ width: '100%' }} onClick={() => void onSignOut()}>
                Sign out
              </button>
            </div>
          ) : (
            <div className="user-card" style={{ fontSize: 12 }}>
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <span className="muted">Operator mode</span>
                <button
                  type="button"
                  className="btn btn-sm"
                  onClick={() => {
                    setApiKeyDraft(getStoredApiKey());
                    setEditingKey((v) => !v);
                  }}
                >
                  {hasStoredKey ? 'API key ✓' : 'Set API key'}
                </button>
              </div>
              {editingKey && (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    setStoredApiKey(apiKeyDraft);
                    setHasStoredKey(apiKeyDraft.trim() !== '');
                    setEditingKey(false);
                  }}
                  style={{ display: 'flex', flexDirection: 'column', gap: 6 }}
                >
                  <input
                    type="password"
                    value={apiKeyDraft}
                    onChange={(e) => setApiKeyDraft(e.target.value)}
                    placeholder="X-API-Key header value"
                    aria-label="Operator API key"
                  />
                  <div className="row" style={{ gap: 6 }}>
                    <button type="submit" className="btn btn-sm btn-primary" style={{ flex: 1 }}>
                      Save
                    </button>
                    {hasStoredKey && (
                      <button
                        type="button"
                        className="btn btn-sm"
                        onClick={() => {
                          setStoredApiKey('');
                          setApiKeyDraft('');
                          setHasStoredKey(false);
                          setEditingKey(false);
                        }}
                      >
                        Clear
                      </button>
                    )}
                  </div>
                </form>
              )}
            </div>
          )}
          <div className="health-card">
            {health ? (
              <>
                <span className="live-dot" />
                <span>
                  monitor <span style={{ color: 'var(--green)' }}>healthy</span> · up{' '}
                  {formatDuration(health.uptime_seconds * 1000)} · v{health.version}
                </span>
              </>
            ) : (
              <>
                <span className="live-dot live-dot-down" />
                <span>
                  monitor <span style={{ color: 'var(--red)' }}>unreachable</span> — is the backend running?
                </span>
              </>
            )}
          </div>
        </div>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  );
}
