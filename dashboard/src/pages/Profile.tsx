// User profile: account identity & display name editing, password rotation,
// active sessions (with revoke-others), organization settings (name, slug,
// status page toggle) and programmatic API keys (upx_...).
import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import type { APIKeyItem, OrgResponse } from '../types';
import { IconBuilding, IconKey, IconLock, IconMonitor, IconUser } from '../components/icons';

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
  const [apiKeys, setApiKeys] = useState<APIKeyItem[] | null>(null);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [displayName, setDisplayName] = useState('');
  const [orgName, setOrgName] = useState('');
  const [orgSlug, setOrgSlug] = useState('');

  const [currentPw, setCurrentPw] = useState('');
  const [newPw, setNewPw] = useState('');

  const [newKeyName, setNewKeyName] = useState('');
  const [revealedToken, setRevealedToken] = useState('');

  useEffect(() => {
    if (me) {
      setDisplayName(me.user.name ?? '');
    }
  }, [me]);

  const loadSessions = useCallback(() => {
    api
      .get<{ sessions: SessionRow[] }>('/api/v1/auth/sessions')
      .then((r) => setSessions(r.sessions))
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, []);

  const loadOrg = useCallback(() => {
    api
      .get<OrgResponse>('/api/v1/org')
      .then((res) => {
        setOrg(res);
        setOrgName(res.org.name);
        setOrgSlug(res.org.slug);
      })
      .catch(() => setOrg(null));
  }, []);

  const loadApiKeys = useCallback(() => {
    api
      .get<{ api_keys: APIKeyItem[] }>('/api/v1/api-keys')
      .then((res) => setApiKeys(res.api_keys))
      .catch(() => setApiKeys([]));
  }, []);

  useEffect(() => {
    if (!me) return;
    loadSessions();
    loadOrg();
    loadApiKeys();
  }, [me, loadSessions, loadOrg, loadApiKeys]);

  const saveDisplayName = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await api.patch('/api/v1/auth/me', { name: displayName });
      await refresh();
      setNotice('Profile updated.');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Profile update failed');
    } finally {
      setBusy(false);
    }
  };

  const saveOrgSettings = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await api.patch('/api/v1/org', { name: orgName, slug: orgSlug });
      loadOrg();
      await refresh();
      setNotice('Organization settings saved.');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Organization update failed');
    } finally {
      setBusy(false);
    }
  };

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
    setNotice('');
    try {
      await api.patch('/api/v1/org', {
        status_page_enabled: !org.org.status_page_enabled,
      });
      loadOrg();
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Update failed');
    } finally {
      setBusy(false);
    }
  };

  const createApiKey = async (e: FormEvent) => {
    e.preventDefault();
    if (!newKeyName.trim()) return;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      const res = await api.post<{ api_key: APIKeyItem; token: string }>('/api/v1/api-keys', {
        name: newKeyName.trim(),
      });
      setNewKeyName('');
      setRevealedToken(res.token);
      setNotice(`API key "${res.api_key.name}" created. Copy the secret token below — it won't be shown again.`);
      loadApiKeys();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create API key');
    } finally {
      setBusy(false);
    }
  };

  const deleteApiKey = async (k: APIKeyItem) => {
    if (!window.confirm(`Revoke API key "${k.name}" (${k.key_prefix}…)?`)) return;
    setBusy(true);
    setNotice('');
    setError('');
    try {
      await api.del(`/api/v1/api-keys/${k.id}`);
      setNotice(`Revoked API key "${k.name}".`);
      loadApiKeys();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to revoke API key');
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
          <div className="subtitle">Account, security, API keys and organization settings</div>
        </div>
      </div>

      {notice && <div className="notice-banner">{notice}</div>}
      {error && <div className="error-banner">{error}</div>}

      <div className="profile-grid">
        <Section icon={<IconUser size={16} />} title="Profile">
          <div className="row" style={{ gap: 14, alignItems: 'flex-start', marginBottom: 14 }}>
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
          <form onSubmit={saveDisplayName} className="auth-form">
            <label>
              Display name
              <input
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="Your name"
                maxLength={100}
              />
            </label>
            <button className="btn btn-sm btn-primary" disabled={busy} type="submit">
              Save name
            </button>
          </form>
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
              <form onSubmit={saveOrgSettings} className="auth-form" style={{ marginBottom: 14 }}>
                <label>
                  Organization name
                  <input
                    value={orgName}
                    onChange={(e) => setOrgName(e.target.value)}
                    minLength={2}
                    maxLength={100}
                    required
                  />
                </label>
                <label>
                  Status page slug <span className="muted mono">(/status/{orgSlug || '…'})</span>
                  <input
                    value={orgSlug}
                    onChange={(e) => setOrgSlug(e.target.value.toLowerCase())}
                    pattern="[a-z0-9]+(-[a-z0-9]+)*"
                    minLength={2}
                    maxLength={60}
                    required
                  />
                </label>
                <button className="btn btn-sm btn-primary" disabled={busy} type="submit">
                  Save organization
                </button>
              </form>

              <div className="row" style={{ marginTop: 8, justifyContent: 'space-between' }}>
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

      <div style={{ marginTop: 16 }}>
        <Section icon={<IconKey size={16} />} title="Programmatic API keys">
          <p className="muted" style={{ fontSize: 13, marginTop: 0 }}>
            Authenticate scripts and CI pipelines with <code>X-API-Key: upx_…</code> or{' '}
            <code>Authorization: Bearer upx_…</code>. Keys are scoped strictly to {me.org.name}.
          </p>

          {revealedToken && (
            <div className="notice-banner" style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
              <span>Secret token (shown once):</span>
              <code className="mono" style={{ fontWeight: 700, wordBreak: 'break-all' }}>
                {revealedToken}
              </code>
              <span className="spacer" />
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => {
                  void navigator.clipboard?.writeText(revealedToken);
                }}
              >
                Copy
              </button>
              <button type="button" className="btn btn-sm" onClick={() => setRevealedToken('')}>
                Dismiss
              </button>
            </div>
          )}

          <form onSubmit={createApiKey} className="row" style={{ gap: 8, marginBottom: 14, flexWrap: 'wrap' }}>
            <input
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder="Key label (e.g. GitHub Actions, Terraform)"
              style={{ flex: 1, minWidth: 220 }}
              maxLength={100}
              required
            />
            <button type="submit" className="btn btn-primary" disabled={busy}>
              + Generate API key
            </button>
          </form>

          {apiKeys === null ? (
            <div className="loading skeleton" />
          ) : apiKeys.length === 0 ? (
            <div className="muted" style={{ fontSize: 13 }}>
              No API keys created yet.
            </div>
          ) : (
            <div className="session-list">
              {apiKeys.map((k) => (
                <div className="session-row" key={k.id}>
                  <strong>{k.name}</strong>
                  <code className="mono muted" style={{ fontSize: 12 }}>
                    {k.key_prefix}…
                  </code>
                  <span className="muted mono" style={{ fontSize: 11 }}>
                    {k.last_used_at
                      ? `used ${new Date(k.last_used_at).toLocaleString()}`
                      : `created ${new Date(k.created_at).toLocaleDateString()}`}
                  </span>
                  <span className="spacer" />
                  <button
                    type="button"
                    className="btn btn-sm btn-danger"
                    disabled={busy}
                    onClick={() => void deleteApiKey(k)}
                  >
                    Revoke
                  </button>
                </div>
              ))}
            </div>
          )}
        </Section>
      </div>
    </div>
  );
}
