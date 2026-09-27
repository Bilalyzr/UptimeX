// User profile: account identity, password rotation, active sessions (with
// revoke-others) and organization settings — the UI surface for the
// account-security API (PUT /auth/password, GET/DELETE /auth/sessions).
import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import type { OrgResponse } from '../types';
import { IconBuilding, IconLock, IconMonitor, IconUser } from '../components/icons';

interface SessionRow {
  created_at: string;
  expires_at: string;
  current: boolean;
}

function Section({
  icon,
  title,
  children,
}: {
  icon: ReactNode;
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="card profile-card">
      <h2 className="profile-sec-title">
        <span className="profile-sec-icon" aria-hidden="true">
          {icon}
        </span>
        {title}
      </h2>
      {children}
    </section>
  );
}

export function Profile() {
  const { me, refresh } = useAuth();
  const [org, setOrg] = useState<OrgResponse | null>(null);
  const [sessions, setSessions] = useState<SessionRow[] | null>(null);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');
  const [busy, setBusy] = useState(false);

  const loadSessions = useCallback(() => {
    api
      .get<{ sessions: SessionRow[] }>('/api/v1/auth/sessions')
      .then((r) => setSessions(r.sessions))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, []);

  const loadOrg = useCallback(() => {
    api
      .get<OrgResponse>('/api/v1/org')
      .then(setOrg)
      .catch(() => setOrg(null));
  }, []);

  useEffect(() => {
    loadSessions();
    loadOrg();
  }, [loadSessions, loadOrg]);

  const changePassword = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await api.put('/api/v1/auth/password', {
        current_password: currentPw,
        new_password: newPw,
      });
      setCurrentPw('');
      setNewPw('');
      setNotice('Password updated — other sessions were signed out.');
      loadSessions();
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Password change failed');
    } finally {
      setBusy(false);
    }
  };

  const revokeOthers = async () => {
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await api.del('/api/v1/auth/sessions');
      setNotice('All other sessions revoked.');
      loadSessions();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Revoke failed');
    } finally {
      setBusy(false);
    }
  };

  const toggleStatusPage = async () => {
    if (!org) return;
    setBusy(true);
    setError('');
    try {
      await api.patch('/api/v1/org', {
        status_page_enabled: !org.org.status_page_enabled,
      });
      loadOrg();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Update failed');
    } finally {
      setBusy(false);
    }
  };

  if (!me) {
    return (
      <div className="empty">
        <h2>Profile lives with your account</h2>
        <p className="muted">
          This deployment runs without a signed-in user. <Link to="/login">Sign in</Link> to manage
          your profile.
        </p>
      </div>
    );
  }

  const initials = (me.user.name || me.user.email)
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase())
    .join('');

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">Profile</h1>
          <div className="subtitle">Account, security and organization settings</div>
        </div>
      </div>

      {(notice || error) && (
        <div className={notice ? 'notice-banner' : 'error-banner'}>{notice || error}</div>
      )}

      <div className="profile-grid">
        <Section icon={<IconUser size={16} />} title="Profile">
          <div className="row" style={{ gap: 14, alignItems: 'flex-start' }}>
            <div className="profile-avatar" aria-hidden="true">
              {initials || 'U'}
            </div>
            <div className="profile-facts">
              <div className="profile-name">{me.user.name || me.user.email.split('@')[0]}</div>
              <div className="muted mono" style={{ fontSize: 12 }}>
                {me.user.email}
              </div>
              <div className="row" style={{ gap: 6, marginTop: 8 }}>
                <span className="plan-chip">{me.org.plan}</span>
                <span className="muted" style={{ fontSize: 12 }}>
                  {me.user.role} · {me.org.name}
                </span>
              </div>
            </div>
          </div>
        </Section>

        <Section icon={<IconLock size={16} />} title="Password">
          <form className="auth-form" onSubmit={changePassword}>
            <label>
              Current password
              <input
                type="password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
                autoComplete="current-password"
                required
              />
            </label>
            <label>
              New password <span className="muted">(min 8 characters)</span>
              <input
                type="password"
                value={newPw}
                onChange={(e) => setNewPw(e.target.value)}
                autoComplete="new-password"
                required
                minLength={8}
              />
            </label>
            <button className="btn btn-primary" disabled={busy} type="submit">
              {busy ? 'Updating…' : 'Change password'}
            </button>
          </form>
          <p className="muted" style={{ fontSize: 12, margin: '10px 0 0' }}>
            Changing your password signs out every other session.
          </p>
        </Section>

        <Section icon={<IconMonitor size={16} />} title="Active sessions">
          {sessions === null ? (
            <div className="loading skeleton" />
          ) : (
            <>
              <div className="session-list">
                {sessions.map((s, i) => (
                  <div className="session-row" key={i}>
                    <span className={`session-dot${s.current ? ' session-dot-now' : ''}`} />
                    <span className="mono" style={{ fontSize: 12 }}>
                      signed in {new Date(s.created_at).toLocaleString()}
                    </span>
                    <span className="spacer" />
                    {s.current ? (
                      <span className="session-chip">This device</span>
                    ) : (
                      <span className="muted mono" style={{ fontSize: 11 }}>
                        expires {new Date(s.expires_at).toLocaleDateString()}
                      </span>
                    )}
                  </div>
                ))}
              </div>
              <button
                className="btn btn-sm"
                style={{ marginTop: 10 }}
                disabled={busy || sessions.filter((s) => !s.current).length === 0}
                onClick={() => void revokeOthers()}
              >
                Revoke other sessions
              </button>
            </>
          )}
        </Section>

        <Section icon={<IconBuilding size={16} />} title="Organization">
          {org ? (
            <div className="profile-facts">
              <div className="profile-name" style={{ fontSize: 15 }}>
                {org.org.name}
              </div>
              <div className="muted mono" style={{ fontSize: 12 }}>
                /status/{org.org.slug}
              </div>
              <div className="row" style={{ marginTop: 12, justifyContent: 'space-between' }}>
                <div>
                  <div style={{ fontWeight: 600 }}>Public status page</div>
                  <div className="muted" style={{ fontSize: 12 }}>
                    {org.org.status_page_enabled ? 'Visible at your slug' : 'Hidden from visitors'}
                  </div>
                </div>
                <button
                  className={`btn btn-sm${org.org.status_page_enabled ? ' btn-danger' : ' btn-primary'}`}
                  disabled={busy}
                  onClick={() => void toggleStatusPage()}
                >
                  {org.org.status_page_enabled ? 'Disable' : 'Enable'}
                </button>
              </div>
              {org.org.status_page_enabled && (
                <Link
                  to={`/status/${org.org.slug}`}
                  className="muted"
                  style={{ fontSize: 12, display: 'inline-block', marginTop: 8 }}
                >
                  View status page →
                </Link>
              )}
            </div>
          ) : (
            <div className="loading skeleton" />
          )}
        </Section>
      </div>
    </div>
  );
}
