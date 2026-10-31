import { useState } from 'react';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import type { Endpoint } from '../types';

// EndpointForm creates or edits an endpoint in a modal. Validation errors
// from the API (e.g. SSRF-blocked targets or plan limits) surface inline.
export function EndpointForm(props: {
  endpoint?: Endpoint;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { me } = useAuth();
  const e = props.endpoint;
  const minInterval = me?.usage.min_interval_seconds ?? 5;
  const defaultInterval = Math.max(30, minInterval);

  const [name, setName] = useState(e?.name ?? '');
  const [url, setUrl] = useState(e?.url ?? '');
  const [method, setMethod] = useState(e?.method ?? 'GET');
  const [interval, setInterval] = useState(String(e?.interval_seconds ?? defaultInterval));
  const [timeoutMs, setTimeoutMs] = useState(String(e?.timeout_ms ?? 5000));
  const [threshold, setThreshold] = useState(String(e?.failure_threshold ?? 3));
  const [min, setMin] = useState(String(e?.expected_status_min ?? 200));
  const [max, setMax] = useState(String(e?.expected_status_max ?? 299));
  const [enabled, setEnabled] = useState(e?.enabled ?? true);

  const hasInitialAdvanced = Boolean(
    e?.expected_body_contains ||
      e?.body ||
      (e?.headers && Object.keys(e.headers).length > 0),
  );
  const [showAdvanced, setShowAdvanced] = useState(hasInitialAdvanced);
  const [expectedBody, setExpectedBody] = useState(e?.expected_body_contains ?? '');
  const [headersJson, setHeadersJson] = useState(
    e?.headers && Object.keys(e.headers).length > 0 ? JSON.stringify(e.headers, null, 2) : '',
  );
  const [body, setBody] = useState(e?.body ?? '');

  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setError(null);

    let parsedHeaders: Record<string, string> = {};
    if (headersJson.trim() !== '') {
      try {
        const raw = JSON.parse(headersJson) as unknown;
        if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
          setError('Custom headers must be a JSON object of {"Header-Name": "value"}');
          return;
        }
        for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
          if (typeof v !== 'string') {
            setError(`Header "${k}" value must be a string`);
            return;
          }
          parsedHeaders[k] = v;
        }
      } catch {
        setError('Custom headers must be valid JSON (e.g. {"Accept": "application/json"})');
        return;
      }
    }

    setSaving(true);
    const payload = {
      name,
      url,
      method,
      headers: parsedHeaders,
      body: method === 'POST' ? body : '',
      expected_body_contains: expectedBody,
      interval_seconds: Number(interval),
      timeout_ms: Number(timeoutMs),
      failure_threshold: Number(threshold),
      expected_status_min: Number(min),
      expected_status_max: Number(max),
      enabled,
    };
    try {
      if (e) {
        await api.patch(`/api/v1/endpoints/${e.id}`, payload);
      } else {
        await api.post('/api/v1/endpoints', payload);
      }
      props.onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop">
      <div className="modal">
        <h3 className="card-title">{e ? `Edit endpoint: ${e.name}` : 'Register endpoint'}</h3>
        <form onSubmit={submit} className="form-grid">
          <div className="form-field label-wide">
            <label htmlFor="ef-name">Name</label>
            <input
              id="ef-name"
              value={name}
              onChange={(ev) => setName(ev.target.value)}
              placeholder="Payments API"
              required
            />
          </div>
          <div className="form-field label-wide">
            <label htmlFor="ef-url">URL (http/https)</label>
            <input
              id="ef-url"
              value={url}
              onChange={(ev) => setUrl(ev.target.value)}
              placeholder="https://api.example.com/health"
              required
            />
          </div>
          <div className="form-field">
            <label htmlFor="ef-method">Method</label>
            <select id="ef-method" value={method} onChange={(ev) => setMethod(ev.target.value)}>
              <option>GET</option>
              <option>HEAD</option>
              <option>POST</option>
            </select>
          </div>
          <div className="form-field">
            <label htmlFor="ef-interval">
              Interval (s){me ? ` · min ${minInterval}s` : ''}
            </label>
            <input
              id="ef-interval"
              type="number"
              min={minInterval}
              max={86400}
              value={interval}
              onChange={(ev) => setInterval(ev.target.value)}
              required
            />
          </div>
          <div className="form-field">
            <label htmlFor="ef-timeout">Timeout (ms)</label>
            <input
              id="ef-timeout"
              type="number"
              min={100}
              max={60000}
              value={timeoutMs}
              onChange={(ev) => setTimeoutMs(ev.target.value)}
              required
            />
          </div>
          <div className="form-field">
            <label htmlFor="ef-threshold">Failure threshold</label>
            <input
              id="ef-threshold"
              type="number"
              min={1}
              max={20}
              value={threshold}
              onChange={(ev) => setThreshold(ev.target.value)}
              required
            />
          </div>
          <div className="form-field">
            <label htmlFor="ef-min">Expected status min</label>
            <input
              id="ef-min"
              type="number"
              min={100}
              max={599}
              value={min}
              onChange={(ev) => setMin(ev.target.value)}
              required
            />
          </div>
          <div className="form-field">
            <label htmlFor="ef-max">Expected status max</label>
            <input
              id="ef-max"
              type="number"
              min={100}
              max={599}
              value={max}
              onChange={(ev) => setMax(ev.target.value)}
              required
            />
          </div>
          <div className="form-field label-wide">
            <label htmlFor="ef-enabled">Enabled</label>
            <select
              id="ef-enabled"
              value={String(enabled)}
              onChange={(ev) => setEnabled(ev.target.value === 'true')}
            >
              <option value="true">yes — schedule checks</option>
              <option value="false">no — paused</option>
            </select>
          </div>

          <div className="label-wide">
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => setShowAdvanced((v) => !v)}
            >
              {showAdvanced ? 'Hide advanced probe options' : 'Advanced probe options (headers, body, keyword)'}
            </button>
          </div>

          {showAdvanced && (
            <>
              <div className="form-field label-wide">
                <label htmlFor="ef-keyword">Expected response keyword (optional)</label>
                <input
                  id="ef-keyword"
                  value={expectedBody}
                  onChange={(ev) => setExpectedBody(ev.target.value)}
                  placeholder='e.g. "status":"ok"'
                />
              </div>
              <div className="form-field label-wide">
                <label htmlFor="ef-headers">Custom request headers JSON (optional)</label>
                <textarea
                  id="ef-headers"
                  rows={2}
                  value={headersJson}
                  onChange={(ev) => setHeadersJson(ev.target.value)}
                  placeholder='{"Accept": "application/json", "X-Env": "prod"}'
                />
              </div>
              {method === 'POST' && (
                <div className="form-field label-wide">
                  <label htmlFor="ef-body">POST request body (optional)</label>
                  <textarea
                    id="ef-body"
                    rows={3}
                    value={body}
                    onChange={(ev) => setBody(ev.target.value)}
                    placeholder='{"ping": true}'
                  />
                </div>
              )}
            </>
          )}

          {error ? <div className="form-error">{error}</div> : null}
          <div className="row label-wide" style={{ justifyContent: 'flex-end', marginTop: 6 }}>
            <button type="button" onClick={props.onCancel}>
              Cancel
            </button>
            <button type="submit" className="btn-primary" disabled={saving}>
              {saving ? 'Saving…' : e ? 'Save changes' : 'Create endpoint'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
