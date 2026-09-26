package scheduler

import (
	"context"
	"testing"
	"time"

	"uptimex/internal/logging"
	"uptimex/internal/models"
	"uptimex/internal/storage"
)

type captureSubmitter struct {
	submitted []models.Endpoint
	failIDs   map[int64]bool
}

func (c *captureSubmitter) Submit(ctx context.Context, ep models.Endpoint) error {
	if c.failIDs != nil && c.failIDs[ep.ID] {
		return context.Canceled
	}
	c.submitted = append(c.submitted, ep)
	return nil
}

func newStore(t *testing.T) storage.Repository {
	t.Helper()
	s, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mkEndpoint(t *testing.T, s storage.Repository, name string, interval int, lastChecked *time.Time) models.Endpoint {
	t.Helper()
	e := models.Endpoint{Name: name, URL: "https://" + name + ".example.com", Method: "GET",
		IntervalSeconds: interval, TimeoutMs: 2000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true}
	if err := s.CreateEndpoint(context.Background(), &e); err != nil {
		t.Fatal(err)
	}
	if lastChecked != nil {
		if err := s.UpsertStatus(context.Background(), &models.EndpointStatus{
			EndpointID: e.ID, State: models.StateHealthy, LastCheckedAt: lastChecked, UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestCycleSubmitsDueEndpointsOnly(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	fresh := mkEndpoint(t, s, "fresh", 30, nil)                                 // never checked -> due
	old := mkEndpoint(t, s, "old", 30, ptr(time.Now().UTC().Add(-time.Minute))) // overdue
	recent := mkEndpoint(t, s, "recent", 30, ptr(time.Now().UTC()))             // just checked -> not due

	cap := &captureSubmitter{}
	sched := New(s, cap, time.Second, logging.NewForTest())
	sched.Cycle(ctx)

	ids := map[int64]bool{}
	for _, e := range cap.submitted {
		ids[e.ID] = true
	}
	if !ids[fresh.ID] || !ids[old.ID] {
		t.Fatalf("fresh+overdue must be submitted: %+v", cap.submitted)
	}
	if ids[recent.ID] {
		t.Fatal("recently checked endpoint must not be re-submitted")
	}
}

func TestDuplicateSubmissionPreventedUntilResult(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	e := mkEndpoint(t, s, "dupe", 30, nil)

	cap := &captureSubmitter{}
	sched := New(s, cap, time.Second, logging.NewForTest())
	sched.Cycle(ctx)
	sched.Cycle(ctx) // still in flight -> must not duplicate
	if len(cap.submitted) != 1 {
		t.Fatalf("submitted %d times, want 1 (duplicate guard)", len(cap.submitted))
	}

	sched.OnResult(e.ID)
	sched.Cycle(ctx) // released; still not due (never recorded a check)... it IS due because no status row update happened
	// Note: due() is based on persisted LastCheckedAt which is updated by the
	// collector when results are stored, not by OnResult. In this test no
	// check was persisted, so the endpoint is due again and gets submitted.
	if len(cap.submitted) != 2 {
		t.Fatalf("after release, endpoint must be schedulable again (got %d)", len(cap.submitted))
	}
}

func TestDisabledEndpointsSkipped(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	e := mkEndpoint(t, s, "off", 30, nil)
	e.Enabled = false
	if err := s.UpdateEndpoint(ctx, &e); err != nil {
		t.Fatal(err)
	}

	cap := &captureSubmitter{}
	sched := New(s, cap, time.Second, logging.NewForTest())
	sched.Cycle(ctx)
	if len(cap.submitted) != 0 {
		t.Fatalf("disabled endpoint submitted: %+v", cap.submitted)
	}
}

func TestSubmitFailureReleasesSlot(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mkEndpoint(t, s, "flaky", 30, nil)

	cap := &captureSubmitter{failIDs: map[int64]bool{}}
	// Make every submit fail.
	cap.failIDs[1] = true
	cap.failIDs[2] = true
	cap.failIDs[3] = true

	sched := New(s, cap, time.Second, logging.NewForTest())
	sched.Cycle(ctx)
	if len(cap.submitted) != 0 {
		t.Fatal("all submits failed; nothing recorded")
	}
	if st := sched.Stats(); st.InFlight != 0 {
		t.Fatalf("failed submits must release in-flight slots: %+v", st)
	}
}

func TestDueLogic(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	if !due(nil, 30, now) {
		t.Error("never-checked must be due")
	}
	if due(ptr(now.Add(-10*time.Second)), 30, now) {
		t.Error("10s after last check with 30s interval must not be due")
	}
	if !due(ptr(now.Add(-31*time.Second)), 30, now) {
		t.Error("31s after last check with 30s interval must be due")
	}
}

func ptr(t time.Time) *time.Time { return &t }
