// Account pages: login, signup, forgot-password and reset-password.
import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { useAuth } from '../auth';
import { Logo } from '../components/Logo';
import type { Me } from '../types';

function AuthShell({ title, sub, children }: { title: string; sub: string; children: ReactNode }) {
  return (
    <div className="auth-page">
      <div className="auth-card">
        <Link to="/" className="landing-brand auth-brand">
          <Logo size={30} wordmark />
        </Link>
        <h1>{title}</h1>
        <p className="auth-sub">{sub}</p>
        {children}
      </div>
    </div>
  );
}

function useAuthSubmit() {
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const navigate = useNavigate();
  const { me, refresh } = useAuth();

  // Already signed in? These pages are pointless — go to the app.
  useEffect(() => {
    if (me) navigate('/app', { replace: true });
  }, [me, navigate]);

  const submit = async (path: string, body: unknown) => {
    setBusy(true);
    setError('');
    try {
      await api.post<Me>(path, body);
      await refresh();
      navigate('/app');
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong — is the backend running?');
    } finally {
      setBusy(false);
    }
  };
  return { error, busy, submit };
}

export function Login() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const { error, busy, submit } = useAuthSubmit();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    void submit('/api/v1/auth/login', { email, password });
  };

  return (
    <AuthShell title="Welcome back" sub="Sign in to your workspace.">
      <form onSubmit={onSubmit} className="auth-form">
        <label>
          Email
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>
        <label>
          <span className="row" style={{ justifyContent: 'space-between' }}>
            <span>Password</span>
            <Link to="/forgot-password" style={{ fontSize: 12, fontWeight: 500 }}>
              Forgot password?
            </Link>
          </span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
        {error && <div className="error-banner">{error}</div>}
        <button className="btn btn-primary btn-lg" disabled={busy} type="submit">
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <p className="auth-alt">
        New here? <Link to="/signup">Create a workspace</Link> — free, no card.
      </p>
    </AuthShell>
  );
}

export function Signup() {
  const [name, setName] = useState('');
  const [orgName, setOrgName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const { error, busy, submit } = useAuthSubmit();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    void submit('/api/v1/auth/signup', { email, password, name, org_name: orgName });
  };

  return (
    <AuthShell
      title="Create your workspace"
      sub="Free plan: 2 endpoints, 60s checks, public status page included."
    >
      <form onSubmit={onSubmit} className="auth-form">
        <label>
          Your name
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </label>
        <label>
          Organization name
          <input
            value={orgName}
            onChange={(e) => setOrgName(e.target.value)}
            placeholder="Acme Corp"
            required
            minLength={2}
          />
        </label>
        <label>
          Email
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>
        <label>
          Password <span className="muted">(min 8 characters)</span>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            required
            minLength={8}
          />
        </label>
        {error && <div className="error-banner">{error}</div>}
        <button className="btn btn-primary btn-lg" disabled={busy} type="submit">
          {busy ? 'Creating…' : 'Create workspace'}
        </button>
      </form>
      <p className="auth-alt">
        Already have an account? <Link to="/login">Sign in</Link>
      </p>
    </AuthShell>
  );
}

export function ForgotPassword() {
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [message, setMessage] = useState('');

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    setMessage('');
    try {
      const res = await api.post<{ message: string }>('/api/v1/auth/forgot-password', { email });
      setMessage(
        res.message ||
          'If an account exists for that email, password reset instructions have been sent.',
      );
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Request failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell title="Reset your password" sub="Enter your account email to receive a reset link.">
      <form onSubmit={onSubmit} className="auth-form">
        <label>
          Email
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
            required
          />
        </label>
        {message && <div className="notice-banner">{message}</div>}
        {error && <div className="error-banner">{error}</div>}
        <button className="btn btn-primary btn-lg" disabled={busy} type="submit">
          {busy ? 'Sending…' : 'Send reset instructions'}
        </button>
      </form>
      <p className="auth-alt">
        Remembered it? <Link to="/login">Back to sign in</Link>
      </p>
    </AuthShell>
  );
}

export function ResetPassword() {
  const [params] = useSearchParams();
  const [token, setToken] = useState(() => params.get('token') ?? '');
  const [newPassword, setNewPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.post('/api/v1/auth/reset-password', {
        token,
        new_password: newPassword,
      });
      setDone(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Password reset failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell title="Choose a new password" sub="Reset links expire after 30 minutes.">
      {done ? (
        <div>
          <div className="notice-banner">
            Password updated. All previous sessions have been signed out.
          </div>
          <Link to="/login" className="btn btn-primary btn-lg" style={{ display: 'inline-flex' }}>
            Sign in with new password →
          </Link>
        </div>
      ) : (
        <form onSubmit={onSubmit} className="auth-form">
          {!params.get('token') && (
            <label>
              Reset token
              <input
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste token from reset email"
                required
              />
            </label>
          )}
          <label>
            New password <span className="muted">(min 8 characters)</span>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              autoComplete="new-password"
              required
              minLength={8}
            />
          </label>
          {error && <div className="error-banner">{error}</div>}
          <button className="btn btn-primary btn-lg" disabled={busy} type="submit">
            {busy ? 'Updating…' : 'Update password'}
          </button>
        </form>
      )}
      <p className="auth-alt">
        <Link to="/login">Back to sign in</Link>
      </p>
    </AuthShell>
  );
}
