import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { Health } from '../types';
import { formatDuration } from '../format';
import { useAuth } from '../auth';
import { Logo } from './Logo';

export function Layout() {
  const [health, setHealth] = useState<Health | null>(null);
  const { me, signOut } = useAuth();
  const navigate = useNavigate();

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

  const link = ({ isActive }: { isActive: boolean }) => `nav-link${isActive ? ' active' : ''}`;

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <Logo size={26} wordmark />
        </div>

        <div className="side-label">Menu</div>
        <nav className="side-nav">
          <NavLink to="/app" end className={link}>
            Overview
          </NavLink>
          <NavLink to="/app/endpoints" className={link}>
            Endpoints
          </NavLink>
          <NavLink to="/app/incidents" className={link}>
            Incidents
          </NavLink>
          {me && (
            <NavLink to="/app/billing" className={link}>
              Billing
            </NavLink>
          )}
        </nav>

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
            <div className="user-card muted" style={{ fontSize: 12 }}>
              Operator mode — no account session.
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
