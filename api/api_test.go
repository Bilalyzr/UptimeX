package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"uptimex/api/handlers"
	"uptimex/api/routes"
	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/config"
	"uptimex/internal/logging"
	"uptimex/internal/metrics"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

// apiServer wires the full route tree over a real SQLite store and engine,
// plus a controllable probe target.
type apiServer struct {
	t       *testing.T
	handler http.Handler
	store   storage.Repository
	target  *httptest.Server
	failing *bool
}

func newAPIServer(t *testing.T, apiKey string) *apiServer {
	t.Helper()
	store, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	failing := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(target.Close)

	engine := monitor.New(store, checker.NewHTTPChecker(checker.NewGuard(true)),
		alert.NewManager(nil, 0, logging.NewForTest()),
		monitor.Config{WorkerCount: 4, JobQueueSize: 64, SchedulerTick: 40 * time.Millisecond},
		logging.NewForTest())
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Stop)

	metricsSvc := &metrics.Service{Repo: store}
	cfg := &config.APIConfig{RateLimitRPS: 1000, RateLimitBurst: 1000, APIKey: apiKey}
	handler := routes.New(routes.Deps{
		Logger:    logging.NewForTest(),
		APIConfig: cfg,
		Health:    handlers.NewHealthHandler(time.Now().UTC(), "test", func() error { return nil }),
		Endpoints: &handlers.EndpointHandler{Repo: store, Engine: engine, Guard: checker.NewGuard(true), Metrics: metricsSvc, Logger: logging.NewForTest()},
		Metrics:   &handlers.MetricsHandler{Metrics: metricsSvc, Repo: store, EngineStats: func() any { return engine.Stats() }, Logger: logging.NewForTest()},
		Incidents: &handlers.IncidentHandler{Repo: store, Logger: logging.NewForTest()},
	})
	return &apiServer{t: t, handler: handler, store: store, target: target, failing: &failing}
}

func (s *apiServer) do(method, path, body string, hdr map[string]string) (*http.Response, map[string]any) {
	s.t.Helper()
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	resp := rec.Result()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func validPayload(url string) string {
	return fmt.Sprintf(`{"name":"Payments API","url":%q,"method":"GET","interval_seconds":5,"timeout_ms":2000,"failure_threshold":3,"expected_status_min":200,"expected_status_max":299,"enabled":true}`, url)
}

func TestEndpointCRUDViaAPI(t *testing.T) {
	s := newAPIServer(t, "")

	// Create.
	resp, body := s.do("POST", "/api/v1/endpoints", validPayload(s.target.URL), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d: %v", resp.StatusCode, body)
	}
	id := int64(body["id"].(float64))
	if body["name"] != "Payments API" || body["enabled"] != true {
		t.Fatalf("created body = %v", body)
	}

	// List includes state + metrics skeleton.
	resp, body = s.do("GET", "/api/v1/endpoints", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("list = %d", resp.StatusCode)
	}
	eps := body["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("list length = %d", len(eps))
	}

	// Get.
	resp, body = s.do("GET", fmt.Sprintf("/api/v1/endpoints/%d", id), "", nil)
	if resp.StatusCode != 200 || body["endpoint"].(map[string]any)["name"] != "Payments API" {
		t.Fatalf("get = %d %v", resp.StatusCode, body)
	}

	// PATCH partial update: only interval + enabled.
	patch := `{"interval_seconds":60,"enabled":false}`
	resp, body = s.do("PATCH", fmt.Sprintf("/api/v1/endpoints/%d", id), patch, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("patch = %d: %v", resp.StatusCode, body)
	}
	if body["interval_seconds"].(float64) != 60 || body["enabled"] != false {
		t.Fatalf("patched body = %v", body)
	}
	if body["name"] != "Payments API" {
		t.Fatal("patch must not clear unspecified fields")
	}

	// On-demand test returns a probe result.
	resp, body = s.do("POST", fmt.Sprintf("/api/v1/endpoints/%d/test", id), "", nil)
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("test = %d %v", resp.StatusCode, body)
	}
	if body["status_code"].(float64) != 200 {
		t.Fatalf("test status = %v", body["status_code"])
	}

	// Delete.
	resp, _ = s.do("DELETE", fmt.Sprintf("/api/v1/endpoints/%d", id), "", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	resp, _ = s.do("GET", fmt.Sprintf("/api/v1/endpoints/%d", id), "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete = %d", resp.StatusCode)
	}
}

func TestEndpointValidationErrors(t *testing.T) {
	s := newAPIServer(t, "")
	cases := []struct {
		name string
		body string
		want string
	}{
		{"bad scheme", `{"name":"x","url":"ftp://example.com"}`, "url"},
		{"interval too small", `{"name":"x","url":"https://a.example.com","interval_seconds":1}`, "interval_seconds"},
		{"timeout out of range", `{"name":"x","url":"https://a.example.com","timeout_ms":10}`, "timeout_ms"},
		{"threshold zero", `{"name":"x","url":"https://a.example.com","failure_threshold":0}`, "failure_threshold"},
		{"status range inverted", `{"name":"x","url":"https://a.example.com","expected_status_min":400,"expected_status_max":200}`, "expected_status"},
		{"missing name", `{"url":"https://a.example.com"}`, "name"},
		{"bad method", `{"name":"x","url":"https://a.example.com","method":"DELETE"}`, "method"},
	}
	for _, c := range cases {
		resp, body := s.do("POST", "/api/v1/endpoints", c.body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (%v)", c.name, resp.StatusCode, body)
			continue
		}
		msg := body["error"].(string)
		if !strings.Contains(msg, c.want) {
			t.Errorf("%s: error %q should mention %q", c.name, msg, c.want)
		}
	}

	// Unknown fields rejected (strict decoding).
	resp, _ := s.do("POST", "/api/v1/endpoints", `{"name":"x","url":"https://a.example.com","typo":1}`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", resp.StatusCode)
	}

	// Malformed JSON.
	resp, _ = s.do("POST", "/api/v1/endpoints", `{not json`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed json: status = %d, want 400", resp.StatusCode)
	}

	// Invalid id.
	resp, _ = s.do("GET", "/api/v1/endpoints/abc", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", resp.StatusCode)
	}
}

func TestAPIKeyProtectsMutations(t *testing.T) {
	s := newAPIServer(t, "secret-key")

	// Reads allowed without key.
	if resp, _ := s.do("GET", "/api/v1/endpoints", "", nil); resp.StatusCode != 200 {
		t.Fatalf("list without key = %d, want 200", resp.StatusCode)
	}
	// Mutations blocked without key.
	if resp, _ := s.do("POST", "/api/v1/endpoints", validPayload(s.target.URL), nil); resp.StatusCode != 401 {
		t.Fatalf("create without key = %d, want 401", resp.StatusCode)
	}
	// Wrong key blocked.
	if resp, _ := s.do("POST", "/api/v1/endpoints", validPayload(s.target.URL), map[string]string{"X-API-Key": "wrong"}); resp.StatusCode != 401 {
		t.Fatalf("create wrong key = %d, want 401", resp.StatusCode)
	}
	// Correct key allowed.
	hdr := map[string]string{"X-API-Key": "secret-key"}
	if resp, body := s.do("POST", "/api/v1/endpoints", validPayload(s.target.URL), hdr); resp.StatusCode != 201 {
		t.Fatalf("create with key = %d: %v", resp.StatusCode, body)
	}
}

func TestChecksMetricsIncidentsEndpoints(t *testing.T) {
	s := newAPIServer(t, "")

	// Register an endpoint and wait for the engine to check it.
	resp, body := s.do("POST", "/api/v1/endpoints", validPayload(s.target.URL), nil)
	if resp.StatusCode != 201 {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	id := int64(body["id"].(float64))

	waitFor(t, 5*time.Second, "checks recorded", func() bool {
		resp, body := s.do("GET", fmt.Sprintf("/api/v1/endpoints/%d/checks?limit=50", id), "", nil)
		return resp.StatusCode == 200 && body["count"].(float64) >= 1
	})

	// Force failures to DOWN: stop the target.
	*s.failing = true
	force := func() {
		// Directly backdate so the next cycle fires despite the 5s interval.
		st, _ := s.store.GetStatus(context.Background(), id)
		old := time.Now().UTC().Add(-time.Minute)
		st.LastCheckedAt = &old
		_ = s.store.UpsertStatus(context.Background(), st)
	}
	waitFor(t, 8*time.Second, "incident exists", func() bool {
		force()
		resp, body := s.do("GET", "/api/v1/incidents?status=open", "", nil)
		return resp.StatusCode == 200 && body["count"].(float64) >= 1
	})

	// Endpoint metrics reflect failures and distributions.
	resp, body = s.do("GET", fmt.Sprintf("/api/v1/endpoints/%d/metrics?window=1h", id), "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("metrics = %d", resp.StatusCode)
	}
	if body["total_checks"].(float64) < 3 {
		t.Fatalf("total checks = %v", body["total_checks"])
	}
	dist := body["status_distribution"].(map[string]any)
	if dist["5xx"] == nil {
		t.Fatalf("distribution missing 5xx: %v", dist)
	}
	if body["latency"].(map[string]any)["sample_count"] == nil {
		t.Fatal("latency population must be documented")
	}

	// Overview aggregates across endpoints.
	resp, body = s.do("GET", "/api/v1/metrics/overview?window=1h", "", nil)
	if resp.StatusCode != 200 || body["endpoints"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("overview = %d %v", resp.StatusCode, body)
	}
	if body["open_incidents"].(float64) < 1 {
		t.Fatalf("overview open_incidents = %v", body["open_incidents"])
	}

	// Engine stats endpoint.
	resp, body = s.do("GET", "/api/v1/stats", "", nil)
	if resp.StatusCode != 200 || body["pool"] == nil {
		t.Fatalf("stats = %d %v", resp.StatusCode, body)
	}

	// Invalid window rejected.
	resp, _ = s.do("GET", "/api/v1/metrics/overview?window=9x", "", nil)
	if resp.StatusCode != 400 {
		t.Fatalf("bad window = %d, want 400", resp.StatusCode)
	}

	// Incidents list with details.
	resp, body = s.do("GET", "/api/v1/incidents", "", nil)
	incidents := body["incidents"].([]any)
	if len(incidents) == 0 {
		t.Fatal("incidents list empty")
	}
	first := incidents[0].(map[string]any)
	if first["endpoint_name"] != "Payments API" || first["status"] != "OPEN" {
		t.Fatalf("incident = %v", first)
	}

	// Cleanup stops the flaky state for other tests.
	*s.failing = false
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStrictGuardBlocksPrivateRegistration(t *testing.T) {
	// The handler guard is strict (production default): registering a
	// private target must be rejected at the API boundary.
	store, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	handler := routes.New(routes.Deps{
		Logger:    logging.NewForTest(),
		APIConfig: &config.APIConfig{RateLimitRPS: 100, RateLimitBurst: 100},
		Health:    handlers.NewHealthHandler(time.Now().UTC(), "t", nil),
	})
	_ = handler // full wiring below

	endpointHandler := &handlers.EndpointHandler{
		Repo:   store,
		Guard:  checker.NewGuard(false), // strict
		Logger: logging.NewForTest(),
	}
	srv := httptest.NewServer(http.HandlerFunc(endpointHandler.Create))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"name":"internal","url":"http://192.168.1.5/health"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("private registration = %d, want 400", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if !strings.Contains(body["error"].(string), "blocked") &&
		!strings.Contains(body["error"].(string), "SSRF") {
		t.Fatalf("error = %v, want SSRF mention", body["error"])
	}
}

func TestRateLimit(t *testing.T) {
	store, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.APIConfig{RateLimitRPS: 2, RateLimitBurst: 2}
	handler := routes.New(routes.Deps{
		Logger:    logging.NewForTest(),
		APIConfig: cfg,
		Health:    handlers.NewHealthHandler(time.Now().UTC(), "t", nil),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Bucket of 2, refill 2/s: the 3rd immediate request must 429.
	var last int
	for i := 0; i < 5; i++ {
		resp, err := http.Get(srv.URL + "/api/v1/endpoints")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("expected eventual 429, got %d", last)
	}
}

var _ = models.StateHealthy // keep models import if assertions shrink
