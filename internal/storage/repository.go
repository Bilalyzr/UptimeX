// Package storage defines the persistence contract for the monitoring
// platform and provides two implementations: PostgreSQL (production) and
// SQLite (lightweight local development / tests). The rest of the system
// depends only on this interface, never on a specific database.
package storage

import (
	"context"
	"errors"
	"time"

	"uptimex/internal/models"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("record not found")

// ErrDuplicateOpenIncident is returned when an OPEN incident already exists
// for an endpoint (guarded by a partial unique index at the database level).
var ErrDuplicateOpenIncident = errors.New("open incident already exists for endpoint")

// LatencySample is one raw observation used for metrics computation.
type LatencySample struct {
	EndpointID     int64
	CheckedAt      time.Time
	ResponseTimeMs int64
	Success        bool
}

// Repository is the complete persistence surface required by the engine and
// the REST API. All timestamps are UTC.
type Repository interface {
	Ping(ctx context.Context) error
	Close() error

	// Endpoints
	CreateEndpoint(ctx context.Context, e *models.Endpoint) error
	GetEndpoint(ctx context.Context, id int64) (*models.Endpoint, error)
	ListEndpoints(ctx context.Context, onlyEnabled bool) ([]models.Endpoint, error)
	UpdateEndpoint(ctx context.Context, e *models.Endpoint) error
	DeleteEndpoint(ctx context.Context, id int64) error
	CountEndpoints(ctx context.Context) (int64, error)

	// Checks
	InsertCheck(ctx context.Context, r models.CheckResult) (int64, error)
	ListChecks(ctx context.Context, endpointID int64, since time.Time, limit int) ([]models.HealthCheck, error)

	// Raw samples for metrics (most recent first, bounded per endpoint).
	SelectEndpointSamples(ctx context.Context, endpointID int64, since time.Time, limit int) ([]LatencySample, error)
	SelectAllSamples(ctx context.Context, since time.Time, perEndpointLimit int) (map[int64][]LatencySample, error)
	CountOutcomes(ctx context.Context, endpointID *int64, since time.Time) (total int64, success int64, err error)
	StatusCounts(ctx context.Context, endpointID *int64, since time.Time) ([]StatusCount, error)

	// Endpoint status (failure-detector state)
	GetStatus(ctx context.Context, endpointID int64) (*models.EndpointStatus, error)
	UpsertStatus(ctx context.Context, st *models.EndpointStatus) error
	ListStatuses(ctx context.Context) ([]models.EndpointStatus, error)

	// Incidents
	OpenIncident(ctx context.Context, endpointID int64, at time.Time, failureCount int, lastError string) (int64, error)
	ResolveIncident(ctx context.Context, incidentID int64, at time.Time) error
	UpdateIncident(ctx context.Context, incidentID int64, failureCount int, lastError string) error
	GetOpenIncident(ctx context.Context, endpointID int64) (*models.Incident, error)
	ListIncidents(ctx context.Context, status string, limit int) ([]models.IncidentDetail, error)
}
