// Package monitor is the monitoring engine: it owns the scheduler, worker
// pool and result collector, applies the failure state machine, persists
// results, manages incidents and dispatches alerts. All state transitions
// flow through a single collector goroutine, which keeps per-endpoint state
// consistent without distributed locks (PRD §14: "concurrent state updates
// must not corrupt counters").
package monitor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/detector"
	"uptimex/internal/models"
	"uptimex/internal/scheduler"
	"uptimex/internal/storage"
	"uptimex/internal/worker"
)

// Config carries engine tuning from the application configuration.
type Config struct {
	WorkerCount   int
	JobQueueSize  int
	SchedulerTick time.Duration
	HighLatencyMs int64 // 0 disables HIGH_LATENCY alerts
}

// Engine wires the monitoring pipeline together.
type Engine struct {
	repo    storage.Repository
	checker checker.Checker
	alerts  *alert.Manager
	logger  *slog.Logger
	cfg     Config

	pool  *worker.Pool
	sched *scheduler.Scheduler

	mu        sync.RWMutex
	statuses  map[int64]models.EndpointStatus
	endpoints map[int64]models.Endpoint

	attempted atomic.Int64
	succeeded atomic.Int64
	failed    atomic.Int64

	cancel    context.CancelFunc
	done      chan struct{}
	stopOnce  sync.Once
	startedAt time.Time
}

// New builds the engine (not started).
func New(repo storage.Repository, chk checker.Checker, alerts *alert.Manager, cfg Config, logger *slog.Logger) *Engine {
	return &Engine{
		repo:      repo,
		checker:   chk,
		alerts:    alerts,
		logger:    logger,
		cfg:       cfg,
		statuses:  make(map[int64]models.EndpointStatus),
		endpoints: make(map[int64]models.Endpoint),
		done:      make(chan struct{}),
		startedAt: time.Now().UTC(),
	}
}

// Start loads persisted state, then launches pool, collector and scheduler.
func (e *Engine) Start(ctx context.Context) error {
	ctx, e.cancel = context.WithCancel(ctx)

	if err := e.ReloadEndpoints(ctx); err != nil {
		return err
	}
	if err := e.loadStatuses(ctx); err != nil {
		return err
	}

	e.pool = worker.NewPool(e.checker, e.cfg.WorkerCount, e.cfg.JobQueueSize)
	e.pool.Start(e.logger)
	e.sched = scheduler.New(e.repo, e.pool, e.cfg.SchedulerTick, e.logger)

	go e.collect()
	go e.sched.Run(ctx)

	e.logger.Info("engine_started",
		"workers", e.cfg.WorkerCount,
		"queue", e.cfg.JobQueueSize,
		"tick", e.cfg.SchedulerTick.String(),
		"endpoints", len(e.endpoints))
	return nil
}

// ReloadEndpoints refreshes the endpoint cache after API mutations.
func (e *Engine) ReloadEndpoints(ctx context.Context) error {
	endpoints, err := e.repo.ListEndpoints(ctx, false)
	if err != nil {
		return err
	}
	m := make(map[int64]models.Endpoint, len(endpoints))
	for _, ep := range endpoints {
		m[ep.ID] = ep
	}
	e.mu.Lock()
	e.endpoints = m
	e.mu.Unlock()
	return nil
}

func (e *Engine) loadStatuses(ctx context.Context) error {
	statuses, err := e.repo.ListStatuses(ctx)
	if err != nil {
		return err
	}
	m := make(map[int64]models.EndpointStatus, len(statuses))
	for _, st := range statuses {
		m[st.EndpointID] = st
	}
	e.mu.Lock()
	e.statuses = m
	e.mu.Unlock()
	return nil
}

func (e *Engine) endpoint(id int64) (models.Endpoint, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ep, ok := e.endpoints[id]
	return ep, ok
}

func (e *Engine) status(id int64) models.EndpointStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if st, ok := e.statuses[id]; ok {
		return st
	}
	return models.EndpointStatus{EndpointID: id, State: models.StateHealthy}
}

func (e *Engine) setStatus(st models.EndpointStatus) {
	e.mu.Lock()
	e.statuses[st.EndpointID] = st
	e.mu.Unlock()
}

// collect is the single-threaded result processor. Serializing here is what
// keeps per-endpoint failure counters and incidents correct.
func (e *Engine) collect() {
	defer close(e.done)
	for res := range e.pool.Results() {
		e.handleResult(res)
	}
}

// handleResult applies the full pipeline to one probe result.
func (e *Engine) handleResult(res models.CheckResult) {
	ep, ok := e.endpoint(res.EndpointID)
	if !ok {
		// Endpoint was deleted while its job was in flight; drop the result.
		e.sched.OnResult(res.EndpointID)
		return
	}

	now := time.Now().UTC()
	threshold := ep.FailureThreshold
	if threshold < 1 {
		threshold = detector.DefaultThreshold
	}

	prev := e.status(ep.ID)
	next, tr := detector.Evaluate(prev, res.Success, threshold, now)

	// Persist the raw check first (history is evidence). On DB failure we log
	// critically and continue with in-memory state (PRD §28).
	if err := e.persistCheck(res); err != nil {
		e.logger.Error("check_persist_failed", "endpoint_id", ep.ID, "error", err)
	}

	// Incident lifecycle.
	if tr.WentDown {
		e.handleWentDown(ep, res, next, now)
	} else if next.State == models.StateDown {
		// Still down: keep the open incident's record current, but never
		// create a second incident for the same outage.
		if inc, err := e.repo.GetOpenIncident(context.Background(), ep.ID); err == nil {
			if err := e.repo.UpdateIncident(context.Background(), inc.ID, next.ConsecutiveFailures, resultError(res)); err != nil {
				e.logger.Warn("incident_update_failed", "incident_id", inc.ID, "error", err)
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			e.logger.Error("incident_lookup_failed", "endpoint_id", ep.ID, "error", err)
		}
	}
	if tr.Recovered {
		e.handleRecovered(ep, res, now)
	}

	next.UpdatedAt = now
	if err := e.repo.UpsertStatus(context.Background(), &next); err != nil {
		e.logger.Error("status_persist_failed", "endpoint_id", ep.ID, "error", err)
	}
	e.setStatus(next)

	// Optional high-latency signal on otherwise-successful probes.
	if res.Success && e.cfg.HighLatencyMs > 0 && res.ResponseTimeMs > e.cfg.HighLatencyMs {
		e.alerts.Dispatch(context.Background(), models.Alert{
			Type:       models.AlertHighLatency,
			EndpointID: ep.ID, EndpointName: ep.Name, URL: ep.URL,
			LatencyMs: res.ResponseTimeMs,
			Message:   "high latency: " + itoa(res.ResponseTimeMs) + "ms exceeds " + itoa(e.cfg.HighLatencyMs) + "ms",
		})
	}

	if tr.WentDown || tr.Recovered ||
		(next.State != prev.State) {
		e.logger.Info("state_transition",
			"endpoint_id", ep.ID, "endpoint", ep.Name,
			"from", prev.State, "to", next.State,
			"consecutive_failures", next.ConsecutiveFailures,
			"success", res.Success,
			"error_type", res.ErrorType)
	}

	// Counters update only after every write for this result has settled:
	// an observer that sees the counter has advanced can rely on persisted
	// state being current for this endpoint.
	e.attempted.Add(1)
	if res.Success {
		e.succeeded.Add(1)
	} else {
		e.failed.Add(1)
	}
	e.sched.OnResult(ep.ID)
}

func (e *Engine) handleWentDown(ep models.Endpoint, res models.CheckResult, next models.EndpointStatus, now time.Time) {
	incidentID, err := e.repo.OpenIncident(context.Background(), ep.ID, now, next.ConsecutiveFailures, resultError(res))
	if errors.Is(err, storage.ErrDuplicateOpenIncident) {
		// Another path opened it first (belt-and-braces unique index).
		if existing, err2 := e.repo.GetOpenIncident(context.Background(), ep.ID); err2 == nil {
			incidentID = existing.ID
		}
	} else if err != nil {
		e.logger.Error("incident_open_failed", "endpoint_id", ep.ID, "error", err)
	}

	e.logger.Error("endpoint_down",
		"endpoint_id", ep.ID, "endpoint", ep.Name,
		"consecutive_failures", next.ConsecutiveFailures,
		"status_code", codePtr(res.StatusCode),
		"error_type", res.ErrorType,
		"incident_id", incidentID)

	e.alerts.Dispatch(context.Background(), models.Alert{
		Type:       models.AlertDown,
		EndpointID: ep.ID, EndpointName: ep.Name, URL: ep.URL,
		FailureCount: next.ConsecutiveFailures,
		LastStatus:   res.StatusCode,
		LastError:    resultError(res),
		OpenedAt:     &now,
		Message:      "endpoint DOWN after " + itoa(int64(next.ConsecutiveFailures)) + " consecutive failures",
	})
}

func (e *Engine) handleRecovered(ep models.Endpoint, res models.CheckResult, now time.Time) {
	inc, err := e.repo.GetOpenIncident(context.Background(), ep.ID)
	if err == nil {
		if err := e.repo.ResolveIncident(context.Background(), inc.ID, now); err != nil && !errors.Is(err, storage.ErrNotFound) {
			e.logger.Warn("incident_resolve_failed", "incident_id", inc.ID, "error", err)
		}
	} else if !errors.Is(err, storage.ErrNotFound) {
		e.logger.Warn("incident_lookup_failed", "endpoint_id", ep.ID, "error", err)
	}

	e.logger.Info("endpoint_recovered", "endpoint_id", ep.ID, "endpoint", ep.Name,
		"status_code", codePtr(res.StatusCode))

	e.alerts.Dispatch(context.Background(), models.Alert{
		Type:       models.AlertRecovered,
		EndpointID: ep.ID, EndpointName: ep.Name, URL: ep.URL,
		LastStatus: res.StatusCode,
		ResolvedAt: &now,
		Message:    "endpoint RECOVERED: healthy response restored",
	})
}

// persistCheck writes the raw result with one bounded retry.
func (e *Engine) persistCheck(res models.CheckResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := e.repo.InsertCheck(ctx, res)
	if err != nil {
		time.Sleep(200 * time.Millisecond)
		_, err = e.repo.InsertCheck(ctx, res)
	}
	return err
}

// RunCheckNow executes an immediate on-demand probe (POST /endpoints/:id/test).
// It reports the raw result and does not feed the failure state machine:
// manual diagnostics must not distort the incident record.
func (e *Engine) RunCheckNow(ctx context.Context, ep models.Endpoint) models.CheckResult {
	return e.checker.Check(ctx, ep)
}

// Stop shuts the pipeline down gracefully: scheduler stops first, then the
// pool drains queued and in-flight jobs, the collector finishes persisting,
// and finally we stop accepting work.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		if e.cancel != nil {
			e.cancel() // stop scheduling new cycles
		}
		if e.pool != nil {
			e.pool.Stop() // close jobs, drain, close results
		}
		<-e.done // collector exits after results channel closes
		e.logger.Info("engine_stopped",
			"attempted", e.attempted.Load(), "succeeded", e.succeeded.Load(), "failed", e.failed.Load())
	})
}

// Stats is the self-observability payload (PRD §27).
type Stats struct {
	StartedAt       time.Time       `json:"started_at"`
	UptimeSeconds   int64           `json:"uptime_seconds"`
	ChecksAttempted int64           `json:"checks_attempted"`
	ChecksSucceeded int64           `json:"checks_succeeded"`
	ChecksFailed    int64           `json:"checks_failed"`
	Pool            worker.Stats    `json:"pool"`
	Scheduler       scheduler.Stats `json:"scheduler"`
}

// Stats snapshots engine counters.
func (e *Engine) Stats() Stats {
	st := Stats{
		StartedAt:       e.startedAt,
		UptimeSeconds:   int64(time.Since(e.startedAt).Seconds()),
		ChecksAttempted: e.attempted.Load(),
		ChecksSucceeded: e.succeeded.Load(),
		ChecksFailed:    e.failed.Load(),
	}
	if e.pool != nil {
		st.Pool = e.pool.Stats()
	}
	if e.sched != nil {
		st.Scheduler = e.sched.Stats()
	}
	return st
}

func resultError(res models.CheckResult) string {
	if res.ErrorMessage != "" {
		return res.ErrorMessage
	}
	if res.ErrorType != "" {
		return res.ErrorType
	}
	return ""
}

func codePtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
