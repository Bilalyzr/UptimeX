// Package models defines the domain entities shared across the monitoring
// engine, storage layer and REST API. All timestamps are UTC.
package models

import "time"

// EndpointState is the coarse health state of an endpoint as tracked by the
// failure detector state machine (PRD §15).
type EndpointState string

const (
	// StateHealthy means the endpoint is passing its health rule.
	StateHealthy EndpointState = "HEALTHY"
	// StateFailing means the endpoint has failed at least once but has not
	// yet crossed the configured failure threshold.
	StateFailing EndpointState = "FAILING"
	// StateDown means the endpoint has crossed the failure threshold and an
	// incident is open.
	StateDown EndpointState = "DOWN"
)

// Endpoint is a monitored target registered through the API.
type Endpoint struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	URL               string `json:"url"`
	Method            string `json:"method"`
	IntervalSeconds   int    `json:"interval_seconds"`
	TimeoutMs         int    `json:"timeout_ms"`
	FailureThreshold  int    `json:"failure_threshold"`
	ExpectedStatusMin int    `json:"expected_status_min"`
	ExpectedStatusMax int    `json:"expected_status_max"`
	Enabled           bool   `json:"enabled"`
	// OrgID owns the endpoint in SaaS mode; nil marks pre-SaaS global
	// endpoints owned by the operator (legacy API-key scope).
	OrgID     *int64    `json:"org_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EndpointStatus is the persisted, current state of an endpoint maintained by
// the failure detector. One row per endpoint.
type EndpointStatus struct {
	EndpointID          int64         `json:"endpoint_id"`
	State               EndpointState `json:"state"`
	ConsecutiveFailures int           `json:"consecutive_failures"`
	LastSuccessAt       *time.Time    `json:"last_success_at"`
	LastFailureAt       *time.Time    `json:"last_failure_at"`
	LastCheckedAt       *time.Time    `json:"last_checked_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

// CheckResult is the structured outcome of a single probe. StatusCode is nil
// when no HTTP response was received (timeout, DNS, connection errors).
type CheckResult struct {
	EndpointID     int64     `json:"endpoint_id"`
	CheckedAt      time.Time `json:"checked_at"`
	StatusCode     *int      `json:"status_code"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	Success        bool      `json:"success"`
	ErrorType      string    `json:"error_type,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

// HealthCheck is a persisted CheckResult row.
type HealthCheck struct {
	ID             int64     `json:"id"`
	EndpointID     int64     `json:"endpoint_id"`
	CheckedAt      time.Time `json:"checked_at"`
	StatusCode     *int      `json:"status_code"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	Success        bool      `json:"success"`
	ErrorType      *string   `json:"error_type"`
	ErrorMessage   *string   `json:"error_message"`
	CreatedAt      time.Time `json:"created_at"`
}

// Incident represents one outage window for an endpoint. A single outage must
// correspond to a single OPEN incident (PRD §14 / Phase 7).
type Incident struct {
	ID           int64      `json:"id"`
	EndpointID   int64      `json:"endpoint_id"`
	OpenedAt     time.Time  `json:"opened_at"`
	ResolvedAt   *time.Time `json:"resolved_at"`
	Status       string     `json:"status"` // OPEN | RESOLVED
	FailureCount int        `json:"failure_count"`
	LastError    string     `json:"last_error"`
}

// Incident statuses.
const (
	IncidentOpen     = "OPEN"
	IncidentResolved = "RESOLVED"
)

// Error types recorded on failed checks (PRD §18.2).
const (
	ErrTypeTimeout    = "timeout"
	ErrTypeDNS        = "dns"
	ErrTypeConnection = "connection"
	ErrTypeTLS        = "tls"
	ErrTypeHTTPError  = "http_error"
	ErrTypeBlocked    = "blocked"     // SSRF guard rejected the target
	ErrTypeInvalidURL = "invalid_url" // malformed or unsupported URL
	ErrTypeCanceled   = "canceled"
)

// AlertType classifies outbound alert notifications.
type AlertType string

const (
	AlertDown        AlertType = "DOWN"
	AlertRecovered   AlertType = "RECOVERED"
	AlertMonitorDown AlertType = "MONITOR_DOWN"
	AlertHighLatency AlertType = "HIGH_LATENCY"
)

// IncidentDetail is an incident joined with its endpoint for list views.
type IncidentDetail struct {
	Incident
	EndpointName string `json:"endpoint_name"`
	EndpointURL  string `json:"endpoint_url"`
}

// Alert is the payload handed to alert channels.
type Alert struct {
	Type         AlertType  `json:"type"`
	EndpointID   int64      `json:"endpoint_id,omitempty"`
	EndpointName string     `json:"endpoint_name,omitempty"`
	URL          string     `json:"url,omitempty"`
	FailureCount int        `json:"failure_count,omitempty"`
	LastStatus   *int       `json:"last_status_code,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	LatencyMs    int64      `json:"latency_ms,omitempty"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	DetectedAt   time.Time  `json:"detected_at"`
	Message      string     `json:"message"`
}
