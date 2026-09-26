// Session auth context: mirrors the backend's cookie session. `me` is null
// when logged out (or running in legacy API-key mode, where the dashboard is
// anonymous by design).
import { createContext, createElement, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';
import { api } from './api/client';
import type { Me } from './types';

interface AuthState {
  me: Me | null;
  ready: boolean;
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
}

const Ctx = createContext<AuthState>({ me: null, ready: false, refresh: async () => {}, signOut: async () => {} });

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [ready, setReady] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setMe(await api.get<Me>('/api/v1/auth/me'));
    } catch {
      setMe(null);
    } finally {
      setReady(true);
    }
  }, []);

  const signOut = useCallback(async () => {
    try {
      await api.post('/api/v1/auth/logout');
    } finally {
      setMe(null);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return createElement(Ctx.Provider, { value: { me, ready, refresh, signOut } }, children);
}

export const useAuth = (): AuthState => useContext(Ctx);
