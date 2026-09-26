// Strict route guard for /app: only a signed-in session (or a legacy
// operator deployment with open access) may render the dashboard. Anything
// else is redirected to /login.
import { useEffect, useState, type ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../auth';

export function RequireAuth({ children }: { children: ReactNode }) {
  const { me, ready } = useAuth();
  // null = unknown yet, true = legacy deployment (anonymous API allowed).
  const [allowAnonymous, setAllowAnonymous] = useState<boolean | null>(null);

  useEffect(() => {
    if (!ready || me) return;
    let cancelled = false;
    // Raw fetch (not the api client) so the global 401 interceptor does not
    // fire during the probe itself.
    fetch('/api/v1/endpoints')
      .then((r) => {
        if (!cancelled) setAllowAnonymous(r.status !== 401);
      })
      .catch(() => {
        if (!cancelled) setAllowAnonymous(true); // backend down: let pages surface their errors
      });
    return () => {
      cancelled = true;
    };
  }, [ready, me]);

  if (!ready || (!me && allowAnonymous === null)) {
    return <div className="loading skeleton" role="status" aria-label="Checking session" />;
  }
  if (!me && allowAnonymous === false) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}
