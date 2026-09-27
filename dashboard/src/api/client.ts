// Thin fetch client: relative URLs (works in dev via Vite proxy and in
// production via nginx), JSON handling, structured errors.

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    credentials: 'same-origin', // cookie-based sessions
    headers: { 'Content-Type': 'application/json', ...apiKeyHeader(), ...(init?.headers ?? {}) },
    ...init,
  });
  if (resp.status === 204) {
    return undefined as T;
  }
  // Strict sessions: an expired/missing session inside the app means the
  // user must re-authenticate — bounce to /login instead of painting 401
  // error banners everywhere. Auth and public endpoints are exempt (they
  // legitimately run without a session).
  if (
    resp.status === 401 &&
    window.location.pathname.startsWith('/app') &&
    !path.startsWith('/api/v1/auth/') &&
    !path.startsWith('/api/v1/public/')
  ) {
    window.location.assign('/login');
    throw new ApiError(401, 'session expired');
  }
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(resp.status, (body as { error?: string }).error ?? `HTTP ${resp.status}`);
  }
  return body as T;
}

function apiKeyHeader(): Record<string, string> {
  const key = localStorage.getItem('uptimex_api_key');
  return key ? { 'X-API-Key': key } : {};
}

export const api = {
  get: <T>(path: string) => request<T>(path),

  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),

  put: <T>(path: string, body: unknown) =>
    request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),

  patch: <T>(path: string, body: unknown) =>
    request<T>(path, { method: 'PATCH', body: JSON.stringify(body) }),

  del: (path: string) => request<void>(path, { method: 'DELETE' }),
};

export const buildQuery = (params: Record<string, string | undefined>): string => {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) q.set(k, v);
  }
  const s = q.toString();
  return s ? `?${s}` : '';
};
