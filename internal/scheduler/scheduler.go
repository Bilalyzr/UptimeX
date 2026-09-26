// Package scheduler decides when endpoints are due for a check and submits
// them to the worker pool. It never probes itself, never overlaps duplicate
// jobs for the same endpoint, and reloads endpoint configuration every tick
// so new endpoints are picked up without restarts.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"uptimex/internal/models"
	"uptimex/internal/storage"
)

// Submitter receives due endpoints (the worker pool).
type Submitter interface {
	Submit(ctx context.Context, ep models.Endpoint) error
}

// Scheduler runs a periodic cycle: load enabled endpoints + their state,
// find due ones, submit them exactly once.
type Scheduler struct {
	repo      storage.Repository
	tick      time.Duration
	submitter Submitter
	logger    *slog.Logger

	mu       sync.Mutex
	inFlight map[int64]bool // endpoint IDs with a job not yet resulted
	dropped  int64          // duplicate submissions prevented
}

// New builds a scheduler.
func New(repo storage.Repository, submitter Submitter, tick time.Duration, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		repo:      repo,
		submitter: submitter,
		tick:      tick,
		logger:    logger,
		inFlight:  make(map[int64]bool),
	}
}

// Run loops until ctx is cancelled. Each tick performs one scheduling cycle.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Cycle(ctx)
		}
	}
}

// Cycle executes one scheduling pass. Errors are logged, never fatal: the
// scheduler must survive individual probe/persistence failures (NFR).
func (s *Scheduler) Cycle(ctx context.Context) {
	endpoints, err := s.repo.ListEndpoints(ctx, true)
	if err != nil {
		s.logger.Error("scheduler_load_endpoints_failed", "error", err)
		return
	}
	statuses, err := s.repo.ListStatuses(ctx)
	if err != nil {
		s.logger.Error("scheduler_load_status_failed", "error", err)
		return
	}
	lastChecked := make(map[int64]*time.Time, len(statuses))
	for _, st := range statuses {
		lastChecked[st.EndpointID] = st.LastCheckedAt
	}

	now := time.Now().UTC()
	for _, ep := range endpoints {
		if !due(lastChecked[ep.ID], ep.IntervalSeconds, now) {
			continue
		}
		if !s.markInFlight(ep.ID) {
			// markInFlight counted the skipped duplicate under the lock.
			continue
		}
		if err := s.submitter.Submit(ctx, ep); err != nil {
			s.release(ep.ID)
			s.logger.Warn("scheduler_submit_failed", "endpoint_id", ep.ID, "error", err)
		}
	}
}

// due reports whether an endpoint should be checked now: never checked, or
// its interval has elapsed since the last recorded check.
func due(last *time.Time, intervalSeconds int, now time.Time) bool {
	if last == nil {
		return true
	}
	interval := time.Duration(intervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return now.Sub(*last) >= interval
}

// markInFlight atomically claims the scheduling slot for an endpoint.
// Returns false (and counts the skipped duplicate under the lock) when a
// job is already in flight.
func (s *Scheduler) markInFlight(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inFlight[id] {
		s.dropped++
		return false
	}
	s.inFlight[id] = true
	return true
}

// OnResult releases the in-flight marker for an endpoint so its next due
// cycle can schedule it again. The collector calls this for every result.
func (s *Scheduler) OnResult(endpointID int64) {
	s.release(endpointID)
}

func (s *Scheduler) release(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inFlight, id)
}

// Stats reports duplicate-prevention counters for observability.
type Stats struct {
	InFlight          int   `json:"in_flight"`
	DuplicatesSkipped int64 `json:"duplicates_skipped"`
}

// Stats snapshots current counters.
func (s *Scheduler) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{InFlight: len(s.inFlight), DuplicatesSkipped: s.dropped}
}
