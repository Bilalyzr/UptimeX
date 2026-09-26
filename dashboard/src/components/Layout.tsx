import { NavLink, Outlet } from 'react-router-dom';
import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { Health } from '../types';
import { formatDuration } from '../format';

export function Layout() {
  const [health, setHealth] = useState<Health | null>(null);

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

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-dot" /> UptimeX
        </div>
        <NavLink to="/" end className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
          Overview
        </NavLink>
        <NavLink to="/endpoints" className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
          Endpoints
        </NavLink>
        <NavLink to="/incidents" className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
          Incidents
        </NavLink>
        <div className="sidebar-footer">
          {health ? (
            <>
              monitor <span style={{ color: 'var(--green)' }}>healthy</span>
              <br />
              up {formatDuration(health.uptime_seconds * 1000)}
              <br />
              v{health.version} · {health.monitor}
            </>
          ) : (
            <>
              monitor <span style={{ color: 'var(--red)' }}>unreachable</span>
              <br />
              (is the backend running?)
            </>
          )}
        </div>
      </aside>
      <main className="content">
        <Outlet />
      </main>
    </div>
  );
}
