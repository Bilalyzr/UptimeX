// Shared API contract types (mirrors the Go handlers' JSON).

export type EndpointState = 'HEALTHY' | 'FAILING' | 'DOWN';

export interface Endpoint {
  id: number;
  name: string;
  url: string;
  method: string;
  interval_seconds: number;
  timeout_ms: number;
  failure_threshold: number;
  expected_status_min: number;
  expected_status_max: number;
  enabled: boolean;
  org_id?: number | null;
  created_at: string;
  updated_at: string;
}

// --- SaaS tenancy -----------------------------------------------------------

export interface Organization {
  id: number;
  name: string;
  slug: string;
  plan: string;
  status_page_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface User {
  id: number;
  org_id: number;
  email: string;
  name: string;
  role: string;
  created_at: string;
}

export interface PlanUsage {
  endpoints: number;
  max_endpoints?: number;
  min_interval_seconds?: number;
}

export interface Me {
  user: User;
  org: Organization;
  usage: PlanUsage;
}

export interface PlanInfo {
  id: string;
  name: string;
  price_monthly_cents: number;
  currency: string;
  max_endpoints: number;
  min_interval_seconds: number;
  history_days: number;
  features: string[];
}

export interface OrgResponse {
  org: Organization;
  usage: { endpoints: number };
  plans: PlanInfo[];
  billing: { mode: string; provider: string };
}

export interface PublicService {
  id: number;
  name: string;
  state: EndpointState;
  uptime_pct: number | null;
  p95_ms: number | null;
  last_checked_at: string | null;
  open_incident_id: number | null;
}

export interface PublicStatus {
  organization: { name: string; slug: string };
  generated_at: string;
  window: string;
  overall: { state: EndpointState; uptime_pct?: number; p95_ms?: number };
  services: PublicService[];
  open_incidents: { id: number; endpoint_name: string; opened_at: string; status: string }[];
}

export interface EndpointStatus {
  endpoint_id: number;
  state: EndpointState;
  consecutive_failures: number;
  last_success_at: string | null;
  last_failure_at: string | null;
  last_checked_at: string | null;
  updated_at: string;
}

export interface EndpointSummary extends Endpoint {
  state: EndpointState;
  consecutive_failures: number;
  last_success_at: string | null;
  last_failure_at: string | null;
  last_checked_at: string | null;
  window: string | null;
  uptime_pct: number | null;
  p99_ms: number | null;
  avg_ms: number | null;
  total_checks: number | null;
  open_incident_id: number | null;
}

export interface LatencyStats {
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  avg_ms: number;
  min_ms: number;
  max_ms: number;
  sample_count: number;
}

export interface TrendPoint {
  time: string;
  avg_ms: number;
  p95_ms: number;
  p99_ms: number;
  checks: number;
  failures: number;
}

export type Distribution = Record<string, number>;

export interface OverviewMetrics {
  window: string;
  from: string;
  to: string;
  endpoints: {
    total: number;
    healthy: number;
    failing: number;
    down: number;
    disabled: number;
  };
  total_checks: number;
  successful_checks: number;
  failed_checks: number;
  uptime_pct: number;
  failure_rate_pct: number;
  latency: LatencyStats;
  status_distribution: Distribution;
  latency_trend: TrendPoint[];
  open_incidents: number;
}

export interface EndpointMetrics {
  endpoint_id: number;
  window: string;
  from: string;
  to: string;
  total_checks: number;
  successful_checks: number;
  failed_checks: number;
  uptime_pct: number;
  failure_rate_pct: number;
  latency: LatencyStats;
  status_distribution: Distribution;
  latency_trend: TrendPoint[];
}

export interface Incident {
  id: number;
  endpoint_id: number;
  endpoint_name: string;
  endpoint_url: string;
  opened_at: string;
  resolved_at: string | null;
  status: 'OPEN' | 'RESOLVED';
  failure_count: number;
  last_error: string;
}

export interface HealthCheck {
  id: number;
  endpoint_id: number;
  checked_at: string;
  status_code: number | null;
  response_time_ms: number;
  success: boolean;
  error_type: string | null;
  error_message: string | null;
  created_at: string;
}

export interface CheckResult {
  endpoint_id: number;
  checked_at: string;
  status_code: number | null;
  response_time_ms: number;
  success: boolean;
  error_type?: string;
  error_message?: string;
}

export interface EngineStats {
  started_at: string;
  uptime_seconds: number;
  checks_attempted: number;
  checks_succeeded: number;
  checks_failed: number;
  pool: { workers: number; active_workers: number; queue_depth: number; processed_total: number; in_flight: number };
  scheduler: { in_flight: number; duplicates_skipped: number };
}

export interface Health {
  status: string;
  service: string;
  version: string;
  monitor: string;
  started_at: string;
  uptime_seconds: number;
  heartbeat_at: string;
}
