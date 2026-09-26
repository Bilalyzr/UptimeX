// Package e2e verifies the complete monitoring pipeline end to end against a
// real (SQLite) store and real HTTP targets: registration -> scheduling ->
// concurrent probing -> persistence -> failure threshold -> incident ->
// alert -> recovery -> resolution, plus explicit failure injection
// (500/503, timeout, DNS, connection refused, blocked target, DB outage).
package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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

// recordingAlerts captures every dispatched alert.
type recordingAlerts struct {
	mu sync.Mutex
	a  []models.Alert
}

func (r *recordingAlerts) Name() string { return "recording" }
func (r *recordingAlerts) Send(ctx context.Context, a models.Alert) error {
	r.mu.Lock()
	r.a = append(r.a, a)
	r.mu.Unlock()
	return nil
}
func (r *recordingAlerts) all() []models.Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.Alert, len(r.a))
	copy(out, r.a)
	return out
}
func (r *recordingAlerts) of(t models.AlertType) []models.Alert {
	var out []models.Alert
	for _, a := range r.all() {
		if a.Type == t {
			out = append(out, a)
		}
	}
	return out
}

// toggleServer returns a target whose behavior flips between healthy and
// failing, plus the switch.
func toggleServer(t *testing.T, failCode int) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	failing := &atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(failCode)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv, failing
}

type harness struct {
	repo   storage.Repository
	engine *monitor.Engine
	alerts *recordingAlerts
}

func newHarness(t *testing.T, highLatencyMs int64) *harness {
	t.Helper()
	repo, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	logger := logging.NewForTest()
	if os.Getenv("HM_TEST_DEBUG") != "" {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	rec := &recordingAlerts{}
	manager := alert.NewManager([]alert.Channel{rec}, 0, logging.NewForTest())

	eng := monitor.New(repo, checker.NewHTTPChecker(checker.NewGuard(true)), manager,
		monitor.Config{WorkerCount: 8, JobQueueSize: 256, SchedulerTick: 40 * time.Millisecond, HighLatencyMs: highLatencyMs}, logger)
	if err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(eng.Stop)
	return &harness{repo: repo, engine: eng, alerts: rec}
}

func (h *harness) addEndpoint(t *testing.T, name, url string, timeoutMs, threshold int) models.Endpoint {
	t.Helper()
	e := models.Endpoint{Name: name, URL: url, Method: "GET",
		IntervalSeconds: 5, TimeoutMs: timeoutMs, FailureThreshold: threshold,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true}
	if err := h.repo.CreateEndpoint(context.Background(), &e); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := h.engine.ReloadEndpoints(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	return e
}

// makeDue backdates the persisted last-checked time so the next scheduler
// cycle picks the endpoint up immediately (tests run faster than the 5s
// production interval floor). It is race-aware: success means "a new check
// is guaranteed" — either one already fired (DB row or engine counter grew,
// important when check persistence itself is broken) or the backdated row
// survived long enough for the scheduler tick to pick it up.
func (h *harness) makeDue(t *testing.T, endpointID int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		beforeCount := h.checkCount(t, endpointID)
		beforeDone := h.engine.Stats().ChecksAttempted
		cutoff := time.Now().UTC()

		st, err := h.repo.GetStatus(context.Background(), endpointID)
		if err != nil {
			t.Fatalf("get status: %v", err)
		}
		old := cutoff.Add(-time.Minute)
		st.LastCheckedAt = &old
		if err := h.repo.UpsertStatus(context.Background(), st); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		ok := pollQuiet(500*time.Millisecond, func() bool {
			return h.checkCount(t, endpointID) > beforeCount ||
				h.engine.Stats().ChecksAttempted > beforeDone
		})
		if ok {
			return // a check fired off the backdate
		}
		cur, err := h.repo.GetStatus(context.Background(), endpointID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.LastCheckedAt != nil && cur.LastCheckedAt.Before(cutoff.Add(-30*time.Second)) {
			return // backdate intact; the next scheduler tick will fire
		}
		// Clobbered by a racing write without a new check: retry.
	}
	t.Fatalf("makeDue: could not schedule endpoint %d", endpointID)
}

// drive repeatedly forces checks until cond holds. Inner waits never fail
// the test; only exhausting all tries does.
func (h *harness) drive(t *testing.T, endpointID int64, what string, tries int, cond func() bool) {
	t.Helper()
	for i := 0; i < tries; i++ {
		if cond() {
			return
		}
		h.makeDue(t, endpointID)
		pollQuiet(2*time.Second, cond)
	}
	if !cond() {
		t.Fatalf("condition not reached after %d forced checks: %s", tries, what)
	}
}

func pollUntil(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) checkCount(t *testing.T, endpointID int64) int {
	t.Helper()
	checks, err := h.repo.ListChecks(context.Background(), endpointID, time.Time{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return len(checks)
}

// pollQuiet waits for cond without failing the test.
func pollQuiet(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestFullPipelineDownAlertRecovery is the PRD §32 example scenario.
func TestFullPipelineDownAlertRecovery(t *testing.T) {
	h := newHarness(t, 0)
	ctx := context.Background()
	srv, failing := toggleServer(t, 503)

	ep := h.addEndpoint(t, "Payments API", srv.URL, 2000, 3)

	// Phase 1: healthy probe recorded.
	pollUntil(t, "first check", 5*time.Second, func() bool {
		st, err := h.repo.GetStatus(ctx, ep.ID)
		return err == nil && st.LastCheckedAt != nil && st.State == models.StateHealthy
	})

	// Phase 2: three consecutive failures -> DOWN + incident + alert.
	failing.Store(true)
	h.drive(t, ep.ID, "state DOWN with 3 consecutive failures", 8, func() bool {
		st, _ := h.repo.GetStatus(ctx, ep.ID)
		return st.State == models.StateDown && st.ConsecutiveFailures >= 3
	})

	st, _ := h.repo.GetStatus(ctx, ep.ID)
	if st.ConsecutiveFailures != 3 {
		t.Fatalf("consecutive failures = %d, want exactly 3 at transition", st.ConsecutiveFailures)
	}

	inc, err := h.repo.GetOpenIncident(ctx, ep.ID)
	if err != nil || inc.Status != models.IncidentOpen || inc.FailureCount != 3 {
		t.Fatalf("incident = %+v (%v)", inc, err)
	}

	// Phase 3: two more failures while DOWN -> still ONE incident, and the
	// incident's failure count tracks the streak.
	h.drive(t, ep.ID, "incident failure count reaches 5", 8, func() bool {
		inc, err := h.repo.GetOpenIncident(ctx, ep.ID)
		return err == nil && inc.FailureCount >= 5
	})

	incidents, err := h.repo.ListIncidents(ctx, "", 100)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("duplicate incidents: %+v (%v)", incidents, err)
	}

	// Phase 4: recovery -> RESOLVED + alert + state HEALTHY.
	failing.Store(false)
	h.drive(t, ep.ID, "recovery to HEALTHY with resolved incident", 8, func() bool {
		st, _ := h.repo.GetStatus(ctx, ep.ID)
		_, openErr := h.repo.GetOpenIncident(ctx, ep.ID)
		return st.State == models.StateHealthy && st.ConsecutiveFailures == 0 && openErr == storage.ErrNotFound
	})

	all, _ := h.repo.ListIncidents(ctx, "", 100)
	if len(all) != 1 || all[0].Status != models.IncidentResolved || all[0].ResolvedAt == nil {
		t.Fatalf("resolved incident: %+v", all)
	}

	// Alerts: one DOWN + one RECOVERED (extra failures must not re-alert).
	downs := h.alerts.of(models.AlertDown)
	recovers := h.alerts.of(models.AlertRecovered)
	if len(downs) != 1 {
		t.Fatalf("DOWN alerts = %d, want 1: %+v", len(downs), downs)
	}
	if len(recovers) != 1 {
		t.Fatalf("RECOVERED alerts = %d, want 1", len(recovers))
	}
	if downs[0].EndpointName != "Payments API" || downs[0].FailureCount != 3 {
		t.Fatalf("DOWN alert payload: %+v", downs[0])
	}

	// Phase 5: a second outage opens a NEW incident (re-armed detector).
	failing.Store(true)
	h.drive(t, ep.ID, "second outage opens new incident", 12, func() bool {
		all, _ := h.repo.ListIncidents(ctx, "", 100)
		if len(all) != 2 {
			return false
		}
		hasOpen := false
		for _, inc := range all {
			if inc.Status == models.IncidentOpen {
				hasOpen = true
			}
		}
		return hasOpen
	})
	if got := len(h.alerts.of(models.AlertDown)); got != 2 {
		t.Fatalf("DOWN alerts = %d, want 2", got)
	}
}

// TestTransientFailuresDoNotAlert proves the false-positive filter: with
// threshold 3, alternating failure/success never reaches DOWN.
func TestTransientFailuresDoNotAlert(t *testing.T) {
	h := newHarness(t, 0)
	srv, failing := toggleServer(t, 500)
	ep := h.addEndpoint(t, "flaky", srv.URL, 2000, 3)

	pollUntil(t, "first success", 5*time.Second, func() bool {
		return h.engine.Stats().ChecksSucceeded >= 1
	})

	for i := 1; i <= 4; i++ { // fail, succeed, fail, succeed...
		failing.Store(true)
		n := int64(i)
		h.drive(t, ep.ID, "failure recorded", 4, func() bool {
			return h.engine.Stats().ChecksFailed >= n
		})
		failing.Store(false)
		h.drive(t, ep.ID, "success recorded", 4, func() bool {
			return h.engine.Stats().ChecksSucceeded >= n+1
		})
	}

	st, _ := h.repo.GetStatus(context.Background(), ep.ID)
	if st.State == models.StateDown {
		t.Fatal("alternating failures must never reach DOWN")
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("counter must reset on success: %+v", st)
	}
	if alerts := h.alerts.all(); len(alerts) != 0 {
		t.Fatalf("transient failures must not alert: %+v", alerts)
	}
	if _, err := h.repo.GetOpenIncident(context.Background(), ep.ID); err != storage.ErrNotFound {
		t.Fatal("no incident may exist for transient failures")
	}
}

// TestConcurrentMonitoringOfManyEndpoints checks the 50+ endpoints
// acceptance criterion: all endpoints get checked promptly and a slow target
// does not block the fleet.
func TestConcurrentMonitoringOfManyEndpoints(t *testing.T) {
	h := newHarness(t, 0)

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(800 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer fast.Close()

	const total = 60
	fastIDs := make([]int64, 0, total-2)
	for i := 0; i < total-2; i++ {
		e := h.addEndpoint(t, fmt.Sprintf("fast-%d", i), fast.URL, 2000, 3)
		fastIDs = append(fastIDs, e.ID)
	}
	slow1 := h.addEndpoint(t, "slow-1", slow.URL, 3000, 3)
	slow2 := h.addEndpoint(t, "slow-2", slow.URL, 3000, 3)

	checked := func(ids ...int64) int {
		statuses, err := h.repo.ListStatuses(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byID := map[int64]bool{}
		for _, st := range statuses {
			if st.LastCheckedAt != nil {
				byID[st.EndpointID] = true
			}
		}
		n := 0
		for _, id := range ids {
			if byID[id] {
				n++
			}
		}
		return n
	}

	// Fast endpoints must complete well before the 800ms slow probes finish:
	// proof that a slow endpoint does not block the cycle.
	start := time.Now()
	pollUntil(t, "all fast endpoints checked", 3*time.Second, func() bool {
		return checked(fastIDs...) == len(fastIDs)
	})
	elapsed := time.Since(start)
	if elapsed > 700*time.Millisecond {
		t.Fatalf("fast fleet waited %s; slow endpoints are blocking the cycle", elapsed)
	}

	// Eventually all endpoints, including slow ones, are checked.
	pollUntil(t, "slow endpoints checked", 15*time.Second, func() bool {
		return checked(slow1.ID, slow2.ID) == 2
	})
}
