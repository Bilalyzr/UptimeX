// Shared Cloudflare Pages Functions reverse-proxy helper.
// Forwards /api/*, /health, and /ready to the Go monitor service configured
// via the MONITOR_ORIGIN environment variable (e.g. https://uptimex-api.onrender.com),
// keeping browser session cookies first-party on the Pages domain.

export interface PagesEnv {
  MONITOR_ORIGIN?: string;
}

export interface PagesContext {
  request: Request;
  env: PagesEnv;
}

const HOP_BY_HOP_HEADERS = [
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'host',
];

export async function proxyToMonitor(context: PagesContext): Promise<Response> {
  const rawOrigin = (context.env.MONITOR_ORIGIN ?? '').trim().replace(/\/+$/, '');
  if (!rawOrigin) {
    return new Response(
      JSON.stringify({
        error: 'MONITOR_ORIGIN is not configured on Cloudflare Pages (set it to your Render monitor URL)',
      }),
      { status: 502, headers: { 'Content-Type': 'application/json; charset=utf-8' } },
    );
  }

  let upstreamURL: URL;
  try {
    const incoming = new URL(context.request.url);
    upstreamURL = new URL(`${incoming.pathname}${incoming.search}`, `${rawOrigin}/`);
  } catch {
    return new Response(JSON.stringify({ error: 'invalid MONITOR_ORIGIN URL' }), {
      status: 502,
      headers: { 'Content-Type': 'application/json; charset=utf-8' },
    });
  }

  const headers = new Headers(context.request.headers);
  for (const h of HOP_BY_HOP_HEADERS) {
    headers.delete(h);
  }
  const clientIP = context.request.headers.get('CF-Connecting-IP');
  if (clientIP) {
    headers.set('X-Real-IP', clientIP);
    headers.set('X-Forwarded-For', clientIP);
  }
  headers.set('X-Forwarded-Proto', 'https');

  const method = context.request.method.toUpperCase();
  const hasBody = method !== 'GET' && method !== 'HEAD';

  try {
    const upstream = await fetch(upstreamURL.toString(), {
      method,
      headers,
      body: hasBody ? context.request.body : undefined,
      redirect: 'manual',
    });

    const respHeaders = new Headers(upstream.headers);
    respHeaders.delete('transfer-encoding');

    return new Response(upstream.body, {
      status: upstream.status,
      statusText: upstream.statusText,
      headers: respHeaders,
    });
  } catch (err) {
    return new Response(
      JSON.stringify({
        error: `monitor upstream unreachable: ${err instanceof Error ? err.message : String(err)}`,
      }),
      { status: 502, headers: { 'Content-Type': 'application/json; charset=utf-8' } },
    );
  }
}
