// Tenant alert channels: configure per-organization webhook (with Pro/Business
// HMAC-SHA256 signing) and email notification destinations, toggle delivery,
// and fire live test notifications.
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import type { AlertChannel } from '../types';
import { EmptyState, Loading } from '../components/common';
import { IconBell } from '../components/icons';

interface ChannelListResponse {
  channels: AlertChannel[];
  count: number;
  smtp_configured: boolean;
}

export function Alerts() {
  const { me } = useAuth();
  const [channels, setChannels] = useState<AlertChannel[] | null>(null);
  const [smtpConfigured, setSmtpConfigured] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busyId, setBusyId] = useState<number | null>(null);

  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [type, setType] = useState<'webhook' | 'email'>('webhook');
  const [target, setTarget] = useState('');
  const [secret, setSecret] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    api
      .get<ChannelListResponse>('/api/v1/alert-channels')
      .then((res) => {
        setChannels(res.channels);
        setSmtpConfigured(res.smtp_configured);
      })
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)));
  }, []);

  useEffect(() => {
    if (me) load();
  }, [me, load]);

  if (!me) {
    return (
      <div className="empty">
        <h2>Alert channels live with your workspace</h2>
        <p className="muted">
          Operator mode uses server environment variables (<code>ALERT_WEBHOOK_URL</code>,{' '}
          <code>SMTP_HOST</code>). <Link to="/login">Sign in</Link> to manage per-workspace channels.
        </p>
      </div>
    );
  }

  const canSignHmac = me.org.plan !== 'free';

  const onCreate = async (ev: FormEvent) => {
    ev.preventDefault();
    setSaving(true);
    setError('');
    setNotice('');
    try {
      await api.post<AlertChannel>('/api/v1/alert-channels', {
        name,
        type,
        target,
        ...(type === 'webhook' && secret.trim() ? { secret: secret.trim() } : {}),
        enabled: true,
      });
      setName('');
      setTarget('');
      setSecret('');
      setCreating(false);
      setNotice('Alert channel added.');
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  };

  const toggleChannel = async (ch: AlertChannel) => {
    setBusyId(ch.id);
    setError('');
    setNotice('');
    try {
      await api.patch(`/api/v1/alert-channels/${ch.id}`, { enabled: !ch.enabled });
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusyId(null);
    }
  };

  const testChannel = async (ch: AlertChannel) => {
    setBusyId(ch.id);
    setError('');
    setNotice('');
    try {
      await api.post(`/api/v1/alert-channels/${ch.id}/test`);
      setNotice(`Test notification delivered to "${ch.name}".`);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusyId(null);
    }
  };

  const deleteChannel = async (ch: AlertChannel) => {
    if (!window.confirm(`Delete alert channel "${ch.name}"?`)) return;
    setBusyId(ch.id);
    setError('');
    setNotice('');
    try {
      await api.del(`/api/v1/alert-channels/${ch.id}`);
      setNotice(`Deleted "${ch.name}".`);
      load();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">Alert channels</h1>
          <div className="subtitle">
            DOWN, RECOVERED and HIGH_LATENCY notifications for {me.org.name}
          </div>
        </div>
        <button className="btn btn-primary" onClick={() => setCreating((v) => !v)}>
          {creating ? 'Close' : '+ Add channel'}
        </button>
      </div>

      {notice && <div className="notice-banner">{notice}</div>}
      {error && <div className="error-banner">{error}</div>}

      {creating && (
        <div className="card" style={{ marginBottom: 16 }}>
          <h3 className="profile-sec-title">
            <span className="profile-sec-icon">
              <IconBell size={16} />
            </span>
            New notification channel
          </h3>
          <form onSubmit={onCreate} className="form-grid">
            <div className="form-field">
              <label htmlFor="ac-name">Channel name</label>
              <input
                id="ac-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="On-call Slack / PagerDuty"
                required
              />
            </div>
            <div className="form-field">
              <label htmlFor="ac-type">Type</label>
              <select
                id="ac-type"
                value={type}
                onChange={(e) => setType(e.target.value as 'webhook' | 'email')}
              >
                <option value="webhook">Webhook (HTTP POST JSON)</option>
                <option value="email">Email (SMTP)</option>
              </select>
            </div>
            <div className="form-field label-wide">
              <label htmlFor="ac-target">
                {type === 'webhook' ? 'Webhook URL (https://…)' : 'Recipient email address'}
              </label>
              <input
                id="ac-target"
                type={type === 'email' ? 'email' : 'url'}
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                placeholder={
                  type === 'webhook' ? 'https://hooks.example.com/uptimex' : 'oncall@example.com'
                }
                required
              />
            </div>
            {type === 'webhook' && (
              <div className="form-field label-wide">
                <label htmlFor="ac-secret">
                  HMAC-SHA256 signing secret{' '}
                  <span className="muted">
                    ({canSignHmac ? 'optional — signs X-Monitor-Signature' : 'Pro & Business plans'})
                  </span>
                </label>
                <input
                  id="ac-secret"
                  type="password"
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                  placeholder={
                    canSignHmac
                      ? 'Shared secret for X-Monitor-Signature verification'
                      : 'Upgrade to Pro to enable HMAC webhook signatures'
                  }
                  disabled={!canSignHmac}
                />
              </div>
            )}
            {type === 'email' && !smtpConfigured && (
              <div className="muted label-wide" style={{ fontSize: 12 }}>
                Note: SMTP is not currently configured on this server (<code>SMTP_HOST</code>). You
                can save the recipient now, and alerts will send once SMTP is enabled.
              </div>
            )}
            <div className="row label-wide" style={{ justifyContent: 'flex-end', gap: 8 }}>
              <button type="button" className="btn" onClick={() => setCreating(false)}>
                Cancel
              </button>
              <button type="submit" className="btn btn-primary" disabled={saving}>
                {saving ? 'Saving…' : 'Create channel'}
              </button>
            </div>
          </form>
        </div>
      )}

      <div className="card">
        {channels === null ? (
          <Loading />
        ) : channels.length === 0 ? (
          <EmptyState
            title="No alert channels configured"
            hint='Click "+ Add channel" to deliver outage and recovery alerts via webhook or email.'
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Destination</th>
                  <th>Security</th>
                  <th>Status</th>
                  <th className="num">Actions</th>
                </tr>
              </thead>
              <tbody>
                {channels.map((ch) => (
                  <tr key={ch.id} style={{ opacity: ch.enabled ? 1 : 0.6 }}>
                    <td>
                      <strong>{ch.name}</strong>
                    </td>
                    <td className="mono">{ch.type}</td>
                    <td className="mono">{ch.target}</td>
                    <td className="muted" style={{ fontSize: 12 }}>
                      {ch.type === 'webhook'
                        ? ch.has_secret
                          ? 'HMAC-SHA256 signed'
                          : 'Unsigned'
                        : 'TLS / SMTP'}
                    </td>
                    <td>
                      {ch.enabled ? (
                        <span className="badge badge-HEALTHY">ACTIVE</span>
                      ) : (
                        <span className="badge badge-disabled">PAUSED</span>
                      )}
                    </td>
                    <td className="num" style={{ whiteSpace: 'nowrap' }}>
                      <button
                        className="btn btn-sm"
                        disabled={busyId === ch.id}
                        onClick={() => void testChannel(ch)}
                      >
                        Test
                      </button>{' '}
                      <button
                        className="btn btn-sm"
                        disabled={busyId === ch.id}
                        onClick={() => void toggleChannel(ch)}
                      >
                        {ch.enabled ? 'Pause' : 'Resume'}
                      </button>{' '}
                      <button
                        className="btn btn-sm btn-danger"
                        disabled={busyId === ch.id}
                        onClick={() => void deleteChannel(ch)}
                      >
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
    </div>
  );
}
