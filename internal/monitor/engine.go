// Package monitor is the monitoring engine: it owns the scheduler, worker
// pool and result collector, applies the failure state machine, persists
// results, manages incidents, prunes expired history (PRD FR-14), and
// dispatches alerts without stalling check collection. All state transitions
// flow through a single collector goroutine, which keeps per-endpoint state
// consistent without distributed locks (PRD §14: "concurrent state updates
// must not corrupt counters").
package monitor

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/detector"
	"uptimex/internal/models"
	"uptimex/internal/plans"
	"uptimex/internal/scheduler"
	"uptimex/internal/storage"
	"uptimex/internal/worker"
)

const (
	alertWorkerCount  = 4
	alertQueueSize    = 256
	alertFastWait     = 25 * time.Millisecond
	alertDispatchTime = 30 * time.Second
)

// Config carries engine tuning from the application configuration.
type Config struct {
	WorkerCount        int
	JobQueueSize       int
	SchedulerTick      time.Duration
	HighLatencyMs      int64         // 0 disables HIGH_LATENCY alerts
	RetentionDays      int           // global raw check retention horizon (PRD FR-14; 0 disables)
	RetentionInterval  time.Duration // background pruning cadence (0 disables)
	SAASMode           bool          // enforce per-org Plan.HistoryDays retention
	PublicURL          string        // external dashboard URL for alert links (PRD §25)
	Guard              *checker.Guard
	WebhookTimeout     time.Duration
	AlertRetryAttempts int
	AlertRetryBackoff  time.Duration
	SMTP               alert.EmailConfig
}

type alertJob struct {
	orgID *int64
	alert models.Alert
	done  chan struct{}
}

// Engine wires the monitoring pipeline together.
type Engine struct {
	repo    storage.Repository
	checker checker.Checker
	alerts  *alert.Manager
	logger  *slog.Logger
	cfg     Config

	pool          *worker.Pool
	sched         *scheduler.Scheduler
	safeTransport http.RoundTripper

	alertCh chan alertJob
	alertWG sync.WaitGroup
	auxWG   sync.WaitGroup

	mu        sync.RWMutex
	statuses  map[int64]models.EndpointStatus
	endpoints map[int64]models.Endpoint

	attempted    atomic.Int64
	succeeded    atomic.Int64
	failed       atomic.Int64
	checksPruned atomic.Int64

	cancel    context.CancelFunc
	done      chan struct{}
	stopOnce  sync.Once
	startedAt time.Time
}

// New builds the engine (not started).
func New(repo storage.Repository, chk checker.Checker, alerts *alert.Manager, cfg Config, logger *slog.Logger) *Engine {
	var tr http.RoundTripper
	if cfg.Guard != nil {
		tr = cfg.Guard.SafeTransport()
	} else {
		tr = checker.NewGuard(false).SafeTransport()
	}
	return &Engine{
		repo:          repo,
		checker:       chk,
		alerts:        alerts,
		logger:        logger,
		cfg:           cfg,
		safeTransport: tr,
		statuses:      make(map[int64]models.EndpointStatus),
		endpoints:     make(map[int64]models.Endpoint),
		done:          make(chan struct{}),
		startedAt:     time.Now().UTC(),
	}
}

// Start loads persisted state, then launches pool, alert workers, collector,
// scheduler, and the FR-14 data-retention job.
func (e *Engine) Start(ctx context.Context) error {
	ctx, e.cancel = context.WithCancel(ctx)

	if err := e.ReloadEndpoints(ctx); err != nil {
		return err
	}
	if err := e.loadStatuses(ctx); err != nil {
		return err
	}

	e.alertCh = make(chan alertJob, alertQueueSize)
	for i := 0; i < alertWorkerCount; i++ {
		e.alertWG.Add(1)
		go e.alertWorker()
	}

	e.pool = worker.NewPool(e.checker, e.cfg.WorkerCount, e.cfg.JobQueueSize)
	e.pool.Start(e.logger)
	e.sched = scheduler.New(e.repo, e.pool, e.cfg.SchedulerTick, e.logger)

	go e.collect()
	go e.sched.Run(ctx)

	if e.cfg.RetentionInterval > 0 && (e.cfg.RetentionDays > 0 || e.cfg.SAASMode) {
		e.auxWG.Add(1)
		go e.runRetention(ctx)
	}

	e.logger.Info("engine_started",
		"workers", e.cfg.WorkerCount,
		"queue", e.cfg.JobQueueSize,
		"tick", e.cfg.SchedulerTick.String(),
		"retention_days", e.cfg.RetentionDays,
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
		e.enqueueAlert(ep.OrgID, models.Alert{
			Type:         models.AlertHighLatency,
			EndpointID:   ep.ID,
			EndpointName: ep.Name,
			URL:          ep.URL,
			DashboardURL: e.dashboardURL(ep.ID),
			LatencyMs:    res.ResponseTimeMs,
			Message:      "high latency: " + itoa(res.ResponseTimeMs) + "ms exceeds " + itoa(e.cfg.HighLatencyMs) + "ms",
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

	e.enqueueAlert(ep.OrgID, models.Alert{
		Type:         models.AlertDown,
		EndpointID:   ep.ID,
		EndpointName: ep.Name,
		URL:          ep.URL,
		DashboardURL: e.dashboardURL(ep.ID),
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

	e.enqueueAlert(ep.OrgID, models.Alert{
		Type:         models.AlertRecovered,
		EndpointID:   ep.ID,
		EndpointName: ep.Name,
		URL:          ep.URL,
		DashboardURL: e.dashboardURL(ep.ID),
		LastStatus:   res.StatusCode,
		ResolvedAt:   &now,
		Message:      "endpoint RECOVERED: healthy response restored",
	})
}

func (e *Engine) dashboardURL(endpointID int64) string {
	base := strings.TrimRight(e.cfg.PublicURL, "/")
	if base == "" || endpointID <= 0 {
		return ""
	}
	return base + "/app/endpoints/" + itoa(endpointID)
}

// enqueueAlert hands an alert to the bounded alert worker pool. Fast/local
// channels complete within alertFastWait; slow or retrying external webhooks
// and SMTP connections continue in the background without blocking check
// collection for the rest of the fleet.
func (e *Engine) enqueueAlert(orgID *int64, a models.Alert) {
	if e.alerts == nil {
		return
	}
	if e.alertCh == nil {
		e.dispatchAlert(orgID, a)
		return
	}
	var orgCopy *int64
	if orgID != nil {
		v := *orgID
		orgCopy = &v
	}
	job := alertJob{orgID: orgCopy, alert: a, done: make(chan struct{})}
	select {
	case e.alertCh <- job:
		select {
		case <-job.done:
		case <-time.After(alertFastWait):
		}
	default:
		e.logger.Warn("alert_queue_full_dropped", "type", a.Type, "endpoint_id", a.EndpointID)
	}
}

func (e *Engine) alertWorker() {
	defer e.alertWG.Done()
	for job := range e.alertCh {
		e.dispatchAlert(job.orgID, job.alert)
		close(job.done)
	}
}

func (e *Engine) dispatchAlert(orgID *int64, a models.Alert) {
	ctx, cancel := context.WithTimeout(context.Background(), alertDispatchTime)
	defer cancel()

	var extra []alert.Channel
	if orgID != nil && *orgID > 0 {
		extra = e.buildTenantChannels(ctx, *orgID)
	}
	e.alerts.DispatchWithExtra(ctx, a, extra)
}

func (e *Engine) buildTenantChannels(ctx context.Context, orgID int64) []alert.Channel {
	rows, err := e.repo.ListAlertChannelsByOrg(ctx, orgID, true)
	if err != nil {
		e.logger.Warn("tenant_alert_channels_lookup_failed", "org_id", orgID, "error", err)
		return nil
	}
	out := make([]alert.Channel, 0, len(rows))
	for _, row := range rows {
		if ch := e.ChannelFromModel(row); ch != nil {
			out = append(out, ch)
		}
	}
	return out
}

// ChannelFromModel builds an executable alert.Channel from a persisted
// tenant AlertChannel row (SSRF-guarded for webhooks; returns nil when an
// email channel is configured without a platform SMTP relay).
func (e *Engine) ChannelFromModel(row models.AlertChannel) alert.Channel {
	attempts := e.cfg.AlertRetryAttempts
	if attempts < 1 {
		attempts = 2
	}
	backoff := e.cfg.AlertRetryBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	switch row.Type {
	case models.AlertChannelWebhook:
		return alert.NewWebhookChannel(alert.WebhookConfig{
			Name:      "tenant_webhook",
			URL:       row.Target,
			Secret:    row.Secret,
			Timeout:   e.cfg.WebhookTimeout,
			Attempts:  attempts,
			Backoff:   backoff,
			Transport: e.safeTransport,
		})
	case models.AlertChannelEmail:
		if e.cfg.SMTP.Host == "" {
			return nil
		}
		cfg := e.cfg.SMTP
		cfg.Name = "tenant_email"
		cfg.To = row.Target
		cfg.Attempts = attempts
		cfg.Backoff = backoff
		return alert.NewEmailChannel(cfg)
	default:
		return nil
	}
}

// runRetention periodically prunes old health_checks, expired sessions, and
// expired password reset tokens (PRD §7 FR-14, §35).
func (e *Engine) runRetention(ctx context.Context) {
	defer e.auxWG.Done()
	// Run an initial pass shortly after boot, then every RetentionInterval.
	t := time.NewTicker(e.cfg.RetentionInterval)
	defer t.Stop()

	e.pruneQuietly(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.pruneQuietly(ctx)
		}
	}
}

func (e *Engine) pruneQuietly(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if n, err := e.RunRetentionOnce(pctx); err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Warn("retention_prune_failed", "error", err)
	} else if n > 0 {
		e.logger.Info("retention_pruned_checks", "deleted", n)
	}
}

// RunRetentionOnce executes one retention pruning pass immediately and returns
// the number of deleted health_checks rows.
func (e *Engine) RunRetentionOnce(ctx context.Context) (int64, error) {
	_ = e.repo.DeleteExpiredSessions(ctx)
	_ = e.repo.DeleteExpiredPasswordResetTokens(ctx)

	now := time.Now().UTC()
	var (
		deleted int64
		err     error
	)
	if e.cfg.SAASMode {
		cutoffs := make(map[string]time.Time)
		for _, p := range plans.All() {
			if p.HistoryDays > 0 {
				cutoffs[p.ID] = now.Add(-time.Duration(p.HistoryDays) * 24 * time.Hour)
			}
		}
		var defaultCutoff time.Time
		if e.cfg.RetentionDays > 0 {
			defaultCutoff = now.Add(-time.Duration(e.cfg.RetentionDays) * 24 * time.Hour)
		}
		deleted, err = e.repo.PruneChecksByOrgPlan(ctx, cutoffs, defaultCutoff)
	} else if e.cfg.RetentionDays > 0 {
		cutoff := now.Add(-time.Duration(e.cfg.RetentionDays) * 24 * time.Hour)
		deleted, err = e.repo.PruneChecksBefore(ctx, cutoff)
	}
	if deleted > 0 {
		e.checksPruned.Add(deleted)
	}
	return deleted, err
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

// Stop shuts the pipeline down gracefully: scheduler and retention stop first,
// then the pool drains queued and in-flight jobs, the collector finishes
// persisting and enqueuing alerts, and finally alert workers drain.
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		if e.cancel != nil {
			e.cancel() // stop scheduling new cycles + retention loop
		}
		e.auxWG.Wait()
		if e.pool != nil {
			e.pool.Stop() // close jobs, drain, close results
		}
		<-e.done // collector exits after results channel closes
		if e.alertCh != nil {
			close(e.alertCh)
			e.alertWG.Wait()
		}
		e.logger.Info("engine_stopped",
			"attempted", e.attempted.Load(),
			"succeeded", e.succeeded.Load(),
			"failed", e.failed.Load(),
			"checks_pruned", e.checksPruned.Load())
	})
}

// Stats is the self-observability payload (PRD §27).
type Stats struct {
	StartedAt       time.Time       `json:"started_at"`
	UptimeSeconds   int64           `json:"uptime_seconds"`
	ChecksAttempted int64           `json:"checks_attempted"`
	ChecksSucceeded int64           `json:"checks_succeeded"`
	ChecksFailed    int64           `json:"checks_failed"`
	ChecksPruned    int64           `json:"checks_pruned"`
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
		ChecksPruned:    e.checksPruned.Load(),
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
