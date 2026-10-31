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

func TestPostgresSaaSFeaturesAndRetention(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	org := &models.Organization{Name: "PG Tenant", Slug: "pg-tenant", Plan: models.PlanFree, StatusPageEnabled: true}
	user := &models.User{Email: "pg@tenant.test", PasswordHash: "hash1", Name: "PG Owner", Role: models.RoleOwner}
	if err := s.CreateOrganizationWithOwner(ctx, org, user); err != nil {
		t.Fatalf("CreateOrganizationWithOwner: %v", err)
	}

	ch := &models.AlertChannel{OrgID: org.ID, Name: "Webhook", Type: models.AlertChannelWebhook, Target: "https://example.com/hook", Secret: "sec", Enabled: true}
	if err := s.CreateAlertChannel(ctx, ch); err != nil {
		t.Fatalf("CreateAlertChannel: %v", err)
	}
	channels, err := s.ListAlertChannelsByOrg(ctx, org.ID, true)
	if err != nil || len(channels) != 1 || !channels[0].HasSecret {
		t.Fatalf("ListAlertChannelsByOrg: %+v (%v)", channels, err)
	}

	key := &models.APIKey{OrgID: org.ID, Name: "CI", KeyPrefix: "upx_12345678", TokenHash: "deadbeef"}
	if err := s.CreateAPIKey(ctx, key); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if gotKey, err := s.GetAPIKeyByHash(ctx, "deadbeef"); err != nil || gotKey.OrgID != org.ID {
		t.Fatalf("GetAPIKeyByHash: %+v (%v)", gotKey, err)
	}

	rt := &models.PasswordResetToken{TokenHash: "resethash", UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	if err := s.CreatePasswordResetToken(ctx, rt); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}
	if uid, err := s.ConsumePasswordResetToken(ctx, "resethash", "hash2"); err != nil || uid != user.ID {
		t.Fatalf("ConsumePasswordResetToken: uid=%d err=%v", uid, err)
	}

	ep := &models.Endpoint{
		OrgID: &org.ID, Name: "pg-ep", URL: "https://example.com", Method: "POST",
		Headers: map[string]string{"X-Test": "1"}, Body: `{"a":1}`, ExpectedBodyContains: "ok",
		IntervalSeconds: 60, TimeoutMs: 5000, FailureThreshold: 3, ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true,
	}
	if err := s.CreateEndpoint(ctx, ep); err != nil {
		t.Fatalf("CreateEndpoint with custom probe fields: %v", err)
	}
	loadedEp, err := s.GetEndpoint(ctx, ep.ID)
	if err != nil || loadedEp.ExpectedBodyContains != "ok" || loadedEp.Headers["X-Test"] != "1" {
		t.Fatalf("custom probe roundtrip: %+v (%v)", loadedEp, err)
	}

	sc := 200
	_, _ = s.InsertCheck(ctx, models.CheckResult{
		EndpointID: ep.ID, CheckedAt: now.Add(-15 * 24 * time.Hour),
		StatusCode: &sc, ResponseTimeMs: 25, Success: true,
	})
	pruned, err := s.PruneChecksByOrgPlan(ctx, map[string]time.Time{
		models.PlanFree: now.Add(-7 * 24 * time.Hour),
	}, now.Add(-90*24*time.Hour))
	if err != nil || pruned != 1 {
		t.Fatalf("PruneChecksByOrgPlan: pruned=%d err=%v", pruned, err)
	}
}
