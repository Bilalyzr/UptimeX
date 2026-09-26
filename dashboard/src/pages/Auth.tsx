// Account pages: login and signup. Thin forms over the auth API.
import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { Link, useNavigate } from 'react-router-dom';
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
          Password
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
    <AuthShell title="Create your workspace" sub="Free plan: 5 endpoints, public status page included.">
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
