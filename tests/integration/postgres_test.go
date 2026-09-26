// Package integration runs the storage suite against a real PostgreSQL
// instance. Skipped unless TEST_POSTGRES_DSN is set (CI provides a service
// container; locally: docker run -p 55433:5432 postgres:16-alpine).
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"uptimex/internal/models"
	"uptimex/internal/storage"
)

func newStore(t *testing.T) storage.Repository {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Each run gets a uniquely named database so tests are isolated.
	admin, err := storage.Open(ctx, storage.Options{Driver: "postgres", PostgresDSN: dsn, MaxOpenConns: 2})
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close()

	dbName := "hm_test_" + time.Now().Format("150405.000000000")
	if err := adminCreateDatabase(ctx, admin, dbName); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() { _ = adminDropDatabase(context.Background(), admin, dbName) })

	store, err := storage.Open(ctx, storage.Options{
		Driver:       "postgres",
		PostgresDSN:  replaceDBName(dsn, dbName),
		MaxOpenConns: 5, MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("open test store (migrations): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestPostgresEndpointCRUDAndStatus(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	e := &models.Endpoint{Name: "payments", URL: "https://api.example.com/health", Method: "GET",
		IntervalSeconds: 30, TimeoutMs: 5000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true}
	if err := s.CreateEndpoint(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEndpoint(ctx, e.ID)
	if err != nil || got.Name != "payments" || !got.Enabled {
		t.Fatalf("roundtrip: %+v (%v)", got, err)
	}

	st, err := s.GetStatus(ctx, e.ID)
	if err != nil || st.State != models.StateHealthy {
		t.Fatalf("seeded status: %+v (%v)", st, err)
	}

	got.Enabled = false
	if err := s.UpdateEndpoint(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteEndpoint(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEndpoint(ctx, e.ID); err != storage.ErrNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestPostgresChecksAndAggregates(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	e := &models.Endpoint{Name: "api", URL: "https://api.example.com", Method: "GET",
		IntervalSeconds: 30, TimeoutMs: 5000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true}
	if err := s.CreateEndpoint(ctx, e); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	sc := 200
	for i, ms := range []int64{100, 200, 300} {
		if _, err := s.InsertCheck(ctx, models.CheckResult{EndpointID: e.ID, CheckedAt: now.Add(time.Duration(i) * time.Second),
			StatusCode: &sc, ResponseTimeMs: ms, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.InsertCheck(ctx, models.CheckResult{EndpointID: e.ID, CheckedAt: now.Add(4 * time.Second),
		ResponseTimeMs: 5000, Success: false, ErrorType: models.ErrTypeTimeout, ErrorMessage: "deadline"}); err != nil {
		t.Fatal(err)
	}

	total, success, err := s.CountOutcomes(ctx, nil, now.Add(-time.Minute))
	if err != nil || total != 4 || success != 3 {
		t.Fatalf("outcomes %d/%d (%v)", total, success, err)
	}

	samples, err := s.SelectEndpointSamples(ctx, e.ID, now.Add(-time.Minute), 10)
	if err != nil || len(samples) != 4 {
		t.Fatalf("samples %d (%v)", len(samples), err)
	}

	all, err := s.SelectAllSamples(ctx, now.Add(-time.Minute), 2)
	if err != nil || len(all[e.ID]) != 2 {
		t.Fatalf("SelectAllSamples bound failed: %v", err)
	}

	dist, err := s.StatusCounts(ctx, nil, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var timeo int64
	for _, d := range dist {
		if d.StatusCode == 0 && d.ErrorType == models.ErrTypeTimeout {
			timeo = d.Count
		}
	}
	if timeo != 1 {
		t.Fatalf("timeout bucket = %d", timeo)
	}
}

func TestPostgresIncidentGuarantees(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	e := &models.Endpoint{Name: "web", URL: "https://web.example.com", Method: "GET",
		IntervalSeconds: 30, TimeoutMs: 5000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true}
	if err := s.CreateEndpoint(ctx, e); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	id, err := s.OpenIncident(ctx, e.ID, now, 3, "503")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenIncident(ctx, e.ID, now, 4, "again"); err != storage.ErrDuplicateOpenIncident {
		t.Fatalf("duplicate guard: %v", err)
	}
	if err := s.ResolveIncident(ctx, id, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetOpenIncident(ctx, e.ID); err != storage.ErrNotFound {
		t.Fatal("incident should be resolved")
	}
	list, err := s.ListIncidents(ctx, "", 10)
	if err != nil || len(list) != 1 || list[0].EndpointName != "web" {
		t.Fatalf("list: %+v (%v)", list, err)
	}
}
