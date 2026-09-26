// Pure formatting helpers (unit-tested).

export function formatDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return '—';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const mins = Math.floor(ms / 60_000);
  if (mins < 60) return `${mins}m ${Math.round((ms % 60_000) / 1000)}s`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ${mins % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

export function formatAge(iso: string | null | undefined): string {
  if (!iso) return 'never';
  const ms = Date.now() - new Date(iso).getTime();
  if (ms < 0) return 'just now';
  if (ms < 10_000) return 'just now';
  return `${formatDuration(ms)} ago`;
}

export function formatPct(value: number | null | undefined, digits = 2): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return `${value.toFixed(digits)}%`;
}

export function formatMs(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  if (value >= 1000) return `${(value / 1000).toFixed(2)}s`;
  return `${Math.round(value)}ms`;
}

// Distribution bucket display order and labels (mirrors the Go metrics
// package buckets; 200/201 shown exactly, families for the rest).
export const DISTRIBUTION_ORDER = [
  '200',
  '201',
  '2xx',
  '3xx',
  '4xx',
  '5xx',
  'timeout',
  'network_error',
  'blocked',
  'invalid_url',
  'other',
];

export const DISTRIBUTION_COLORS: Record<string, string> = {
  '200': '#22c55e',
  '201': '#4ade80',
  '2xx': '#86efac',
  '3xx': '#38bdf8',
  '4xx': '#fbbf24',
  '5xx': '#f87171',
  timeout: '#fb923c',
  network_error: '#a78bfa',
  blocked: '#f472b6',
  invalid_url: '#94a3b8',
  other: '#64748b',
};

export const DISTRIBUTION_LABELS: Record<string, string> = {
  '200': '200 OK',
  '201': '201 Created',
  '2xx': 'Other 2xx',
  '3xx': '3xx Redirect',
  '4xx': '4xx Client',
  '5xx': '5xx Server',
  timeout: 'Timeout',
  network_error: 'Network errors',
  blocked: 'Blocked (SSRF)',
  invalid_url: 'Invalid URL',
  other: 'Other',
};

export function stateColor(state: string): string {
  switch (state) {
    case 'HEALTHY':
      return '#22c55e';
    case 'FAILING':
      return '#f59e0b';
    case 'DOWN':
      return '#ef4444';
    case 'OPEN':
      return '#ef4444';
    case 'RESOLVED':
      return '#22c55e';
    default:
      return '#64748b';
  }
}
