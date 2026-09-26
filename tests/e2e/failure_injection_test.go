package e2e

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/logging"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

// waitForChecks waits until the endpoint has at least n persisted checks.
func waitForChecks(t *testing.T, h *harness, endpointID int64, n int) {
	t.Helper()
	pollUntil(t, "checks recorded", 8*time.Second, func() bool {
		return h.checkCount(t, endpointID) >= n
	})
}

// lastCheck returns the newest persisted check.
func lastCheck(t *testing.T, h *harness, endpointID int64) models.HealthCheck {
	t.Helper()
	checks, err := h.repo.ListChecks(context.Background(), endpointID, time.Time{}, 1)
	if err != nil || len(checks) == 0 {
		t.Fatalf("no checks: %v", err)
	}
	return checks[0]
}

func TestInjectionHTTP500IsFailureWithHTTPErrorType(t *testing.T) {
	h := newHarness(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	// 500 is outside the expected 200-299 range by policy (PRD §28).
	ep := h.addEndpoint(t, "err500", srv.URL, 2000, 3)

	h.drive(t, ep.ID, "500 failure recorded", 4, func() bool {
		return h.engine.Stats().ChecksFailed >= 1
	})

	c := lastCheck(t, h, ep.ID)
	if c.Success || c.StatusCode == nil || *c.StatusCode != 500 {
		t.Fatalf("check = %+v", c)
	}
	if c.ErrorType == nil || *c.ErrorType != models.ErrTypeHTTPError {
		t.Fatalf("error type = %v, want http_error", c.ErrorType)
	}
}

func TestInjectionHTTP503LeadsToDownIncident(t *testing.T) {
	h := newHarness(t, 0)
	srv, failing := toggleServer(t, 503)
	failing.Store(true)
	ep := h.addEndpoint(t, "err503", srv.URL, 2000, 3)

	h.drive(t, ep.ID, "503 x3 reaches DOWN", 8, func() bool {
		st, _ := h.repo.GetStatus(context.Background(), ep.ID)
		return st.State == models.StateDown
	})
	if _, err := h.repo.GetOpenIncident(context.Background(), ep.ID); err != nil {
		t.Fatalf("incident missing: %v", err)
	}
	if got := len(h.alerts.of(models.AlertDown)); got != 1 {
		t.Fatalf("DOWN alerts = %d", got)
	}
}

func TestInjectionTimeoutRecordedAsTimeoutError(t *testing.T) {
	h := newHarness(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(600 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ep := h.addEndpoint(t, "slowpoke", srv.URL, 150, 3) // 150ms timeout
	h.drive(t, ep.ID, "timeout recorded", 4, func() bool {
		return h.engine.Stats().ChecksFailed >= 1
	})

	c := lastCheck(t, h, ep.ID)
	if c.Success {
		t.Fatal("timeout must fail the check")
	}
	if c.ErrorType == nil || *c.ErrorType != models.ErrTypeTimeout {
		t.Fatalf("error type = %v, want timeout", c.ErrorType)
	}
	if c.StatusCode != nil {
		t.Fatalf("timeout must have NULL status code, got %d", *c.StatusCode)
	}
}

func TestInjectionDNSFailure(t *testing.T) {
	h := newHarness(t, 0)
	ep := h.addEndpoint(t, "bad-dns", "http://nonexistent-host-hm.invalid/", 1000, 3)
	// The very first check is due immediately; wait for one recorded result.
	waitForChecks(t, h, ep.ID, 1)

	c := lastCheck(t, h, ep.ID)
	if c.Success || c.ErrorType == nil || *c.ErrorType != models.ErrTypeDNS {
		t.Fatalf("dns failure = %+v", c)
	}
}

func TestInjectionConnectionRefused(t *testing.T) {
	h := newHarness(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	ep := h.addEndpoint(t, "refused", url, 1000, 3)
	waitForChecks(t, h, ep.ID, 1)

	c := lastCheck(t, h, ep.ID)
	if c.Success || c.ErrorType == nil || *c.ErrorType != models.ErrTypeConnection {
		t.Fatalf("connection refused = %+v", c)
	}
}

func TestInjectionBlockedTargetFailsClosed(t *testing.T) {
	// Strict SSRF guard: a private-IP target is rejected on every check and
	// flows through the normal failure pipeline.
	repo, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	rec := &recordingAlerts{}
	eng := monitor.New(repo, checker.NewHTTPChecker(checker.NewGuard(false)),
		alert.NewManager([]alert.Channel{rec}, 0, logging.NewForTest()),
		monitor.Config{WorkerCount: 4, JobQueueSize: 64, SchedulerTick: 40 * time.Millisecond}, logging.NewForTest())
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	h := &harness{repo: repo, engine: eng, alerts: rec}
	ep := h.addEndpoint(t, "internal", "http://192.168.33.10:9090/health", 1000, 3)

	waitForChecks(t, h, ep.ID, 1)
	c := lastCheck(t, h, ep.ID)
	if c.Success || c.ErrorType == nil || *c.ErrorType != models.ErrTypeBlocked {
		t.Fatalf("blocked target = %+v", c)
	}
}

// brokenCheckRepo simulates a database outage for check persistence while
// keeping reads working.
type brokenCheckRepo struct {
	storage.Repository
}

func (b brokenCheckRepo) InsertCheck(ctx context.Context, r models.CheckResult) (int64, error) {
	return 0, errors.New("simulated database unavailable")
}

func TestInjectionDatabaseUnavailableKeepsMonitorAlive(t *testing.T) {
	repo, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	broken := brokenCheckRepo{repo}
	rec := &recordingAlerts{}
	eng := monitor.New(broken, checker.NewHTTPChecker(checker.NewGuard(true)),
		alert.NewManager([]alert.Channel{rec}, 0, logging.NewForTest()),
		monitor.Config{WorkerCount: 4, JobQueueSize: 64, SchedulerTick: 40 * time.Millisecond}, logging.NewForTest())
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	h := &harness{repo: broken, engine: eng, alerts: rec}
	srv, failing := toggleServer(t, 503)
	failing.Store(true)
	ep := h.addEndpoint(t, "dbdown", srv.URL, 2000, 3)

	// The engine must keep probing and transition state in memory even
	// though check persistence fails (PRD §28: avoid losing process state).
	for i := 1; i <= 3; i++ {
		h.makeDue(t, ep.ID)
		n := int64(i)
		pollUntil(t, "failure processed despite DB outage", 5*time.Second, func() bool {
			return eng.Stats().ChecksFailed >= n
		})
	}

	// Status writes still succeed (only InsertCheck is broken), so DOWN is
	// visible in storage and an incident is opened.
	st, err := repo.GetStatus(context.Background(), ep.ID)
	if err != nil || st.State != models.StateDown {
		t.Fatalf("state under DB outage: %+v (%v)", st, err)
	}
	if got := len(rec.of(models.AlertDown)); got != 1 {
		t.Fatalf("DOWN alert under DB outage = %d", got)
	}
}

func TestInjectionAlertProviderUnavailableIsNonFatal(t *testing.T) {
	repo, _ := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	defer repo.Close()

	// A channel that always fails.
	failing := &failingChannel{}
	eng := monitor.New(repo, checker.NewHTTPChecker(checker.NewGuard(true)),
		alert.NewManager([]alert.Channel{failing}, 0, logging.NewForTest()),
		monitor.Config{WorkerCount: 4, JobQueueSize: 64, SchedulerTick: 40 * time.Millisecond}, logging.NewForTest())
	if err := eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer eng.Stop()

	h := &harness{repo: repo, engine: eng, alerts: &recordingAlerts{}}
	srv, fail := toggleServer(t, 503)
	fail.Store(true)
	ep := h.addEndpoint(t, "alertless", srv.URL, 2000, 3)

	// Engine still healthy: DOWN state persisted, incident opened, monitor alive.
	h.drive(t, ep.ID, "DOWN despite alert provider failure", 8, func() bool {
		st, _ := repo.GetStatus(context.Background(), ep.ID)
		return st.State == models.StateDown
	})
	if failing.calls() == 0 {
		t.Fatal("alert channel should have been attempted")
	}
}

type failingChannel struct {
	n atomic.Int64
}

func (f *failingChannel) Name() string { return "failing" }
func (f *failingChannel) Send(ctx context.Context, a models.Alert) error {
	f.n.Add(1)
	return errors.New("provider unavailable")
}
func (f *failingChannel) calls() int64 { return f.n.Load() }
