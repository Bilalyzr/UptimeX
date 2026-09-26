package storage

import (
	"context"
	"testing"
	"time"

	"uptimex/internal/models"
)

// openTestStore opens a fresh in-memory SQLite store with migrations applied.
func openTestStore(t *testing.T) *SQLStore {
	t.Helper()
	store, err := Open(context.Background(), Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustEndpoint(t *testing.T, s *SQLStore, name string) *models.Endpoint {
	t.Helper()
	e := &models.Endpoint{
		Name: name, URL: "https://" + name + ".example.com/health", Method: "GET",
		IntervalSeconds: 30, TimeoutMs: 5000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true,
	}
	if err := s.CreateEndpoint(context.Background(), e); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	return e
}

func TestEndpointCRUD(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	e := mustEndpoint(t, s, "payments")

	got, err := s.GetEndpoint(ctx, e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "payments" || !got.Enabled || got.CreatedAt.IsZero() {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	got.Name = "payments-v2"
	got.Enabled = false
	if err := s.UpdateEndpoint(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, _ := s.GetEndpoint(ctx, e.ID)
	if again.Name != "payments-v2" || again.Enabled {
		t.Fatal("update not applied")
	}

	// Creating an endpoint must seed its status row.
	st, err := s.GetStatus(ctx, e.ID)
	if err != nil {
		t.Fatalf("status seed: %v", err)
	}
	if st.State != models.StateHealthy || st.ConsecutiveFailures != 0 {
		t.Fatalf("seeded status = %+v", st)
	}

	list, err := s.ListEndpoints(ctx, true)
	if err != nil || len(list) != 0 {
		t.Fatalf("enabled-only list = %v (%v), want empty", list, err)
	}
	all, err := s.ListEndpoints(ctx, false)
	if err != nil || len(all) != 1 {
		t.Fatalf("list all = %d (%v), want 1", len(all), err)
	}

	if err := s.DeleteEndpoint(ctx, e.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetEndpoint(ctx, e.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// Status row must cascade.
	if _, err := s.GetStatus(ctx, e.ID); err != ErrNotFound {
		t.Fatalf("status should cascade-delete, got %v", err)
	}
	if err := s.DeleteEndpoint(ctx, e.ID); err != ErrNotFound {
		t.Fatal("second delete must return ErrNotFound")
	}
}

func TestCheckPersistenceAndQueries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	e := mustEndpoint(t, s, "api")
	now := time.Now().UTC()

	sc := 200
	if _, err := s.InsertCheck(ctx, models.CheckResult{EndpointID: e.ID, CheckedAt: now, StatusCode: &sc, ResponseTimeMs: 120, Success: true}); err != nil {
		t.Fatal(err)
	}
	// A network error has no status code.
	if _, err := s.InsertCheck(ctx, models.CheckResult{EndpointID: e.ID, CheckedAt: now.Add(time.Second), ResponseTimeMs: 5000, Success: false, ErrorType: models.ErrTypeTimeout, ErrorMessage: "context deadline exceeded"}); err != nil {
		t.Fatal(err)
	}

	checks, err := s.ListChecks(ctx, e.ID, now.Add(-time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Fatalf("checks = %d, want 2", len(checks))
	}
	if checks[0].StatusCode != nil { // newest first: the timeout row
		t.Fatalf("expected NULL status_code on newest check, got %v", checks[0].StatusCode)
	}
	if checks[0].ErrorType == nil || *checks[0].ErrorType != "timeout" {
		t.Fatalf("error type = %v", checks[0].ErrorType)
	}

	total, success, err := s.CountOutcomes(ctx, nil, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || success != 1 {
		t.Fatalf("outcomes = %d/%d, want 2/1", total, success)
	}

	sc500 := 500
	s.InsertCheck(ctx, models.CheckResult{EndpointID: e.ID, CheckedAt: now.Add(2 * time.Second), StatusCode: &sc500, ResponseTimeMs: 40, Success: false, ErrorType: models.ErrTypeHTTPError, ErrorMessage: "500"})

	dist, err := s.StatusCounts(ctx, nil, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[int64]int64{}
	for _, d := range dist {
		byCode[d.StatusCode] += d.Count
	}
	if byCode[200] != 1 || byCode[0] != 1 || byCode[500] != 1 {
		t.Fatalf("distribution = %v", byCode)
	}

	samples, err := s.SelectEndpointSamples(ctx, e.ID, now.Add(-time.Minute), 10)
	if err != nil || len(samples) != 3 {
		t.Fatalf("samples = %d (%v)", len(samples), err)
	}
	all, err := s.SelectAllSamples(ctx, now.Add(-time.Minute), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(all[e.ID]) != 2 { // bounded per endpoint
		t.Fatalf("per-endpoint bound failed: %d", len(all[e.ID]))
	}
}

func TestIncidentLifecycleAndDuplicateGuard(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	e := mustEndpoint(t, s, "web")
	now := time.Now().UTC()

	id, err := s.OpenIncident(ctx, e.ID, now, 3, "503 service unavailable")
	if err != nil || id == 0 {
		t.Fatalf("open incident: %v", err)
	}

	if _, err := s.OpenIncident(ctx, e.ID, now.Add(time.Second), 4, "again"); err != ErrDuplicateOpenIncident {
		t.Fatalf("duplicate open must be rejected, got %v", err)
	}

	open, err := s.GetOpenIncident(ctx, e.ID)
	if err != nil {
		t.Fatalf("get open: %v", err)
	}
	if open.ID != id || open.Status != models.IncidentOpen || open.FailureCount != 3 {
		t.Fatalf("open incident = %+v", open)
	}

	if err := s.UpdateIncident(ctx, id, 5, "504"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveIncident(ctx, id, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	res, err := s.GetOpenIncident(ctx, e.ID)
	if err != ErrNotFound {
		t.Fatalf("open incident after resolve: %v (%v)", res, err)
	}

	details, err := s.ListIncidents(ctx, "", 10)
	if err != nil || len(details) != 1 {
		t.Fatalf("list incidents = %d (%v)", len(details), err)
	}
	if details[0].EndpointName != "web" || details[0].ResolvedAt == nil {
		t.Fatalf("detail = %+v", details[0])
	}
	if details[0].FailureCount != 5 {
		t.Fatalf("failure count not updated: %d", details[0].FailureCount)
	}

	// Resolving twice must report not-found instead of silently succeeding.
	if err := s.ResolveIncident(ctx, id, now); err != ErrNotFound {
		t.Fatalf("double resolve = %v, want ErrNotFound", err)
	}
}

func TestUpsertStatus(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	e := mustEndpoint(t, s, "ups")
	now := time.Now().UTC()

	fail := now.Add(-time.Minute)
	st := &models.EndpointStatus{
		EndpointID: e.ID, State: models.StateFailing, ConsecutiveFailures: 2,
		LastFailureAt: &fail, UpdatedAt: now,
	}
	if err := s.UpsertStatus(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetStatus(ctx, e.ID)
	if got.State != models.StateFailing || got.ConsecutiveFailures != 2 || got.LastSuccessAt != nil {
		t.Fatalf("status = %+v", got)
	}

	// Second upsert updates in place.
	st.ConsecutiveFailures = 3
	st.State = models.StateDown
	if err := s.UpsertStatus(ctx, st); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListStatuses(ctx)
	if len(list) != 1 || list[0].State != models.StateDown {
		t.Fatalf("statuses = %+v", list)
	}
}

func TestMigrationIdempotence(t *testing.T) {
	// Reopening a migrated file database must not re-apply or fail.
	dir := t.TempDir()
	path := dir + "/m.db"
	s1, err := Open(context.Background(), Options{Driver: "sqlite", SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	mustEndpoint(t, s1, "persisted")
	_ = s1.Close()

	s2, err := Open(context.Background(), Options{Driver: "sqlite", SQLitePath: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	n, err := s2.CountEndpoints(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("count after reopen = %d (%v)", n, err)
	}
}
