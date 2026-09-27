// End-to-end tests for the SaaS surface: signup/login sessions, per-org
// data isolation, plan quotas, billing plan changes and public status pages.
package api_test

import (
	"context"
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
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

// newProbeTarget runs an always-healthy HTTP probe target.
func newProbeTarget(t *testing.T) *httptest.Server {
	t.Helper()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(target.Close)
	return target
}

// newSaaSServer builds the full route tree in SaaS mode (identity required
// on data routes, auth/org/public handlers registered).
func newSaaSServer(t *testing.T) *apiServer {
	t.Helper()
	store, err := storage.Open(context.Background(), storage.Options{Driver: "sqlite", SQLitePath: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	target := newProbeTarget(t)
	engine := monitor.New(store, checker.NewHTTPChecker(checker.NewGuard(true)),
		alert.NewManager(nil, 0, logging.NewForTest()),
		monitor.Config{WorkerCount: 4, JobQueueSize: 64, SchedulerTick: 40 * time.Millisecond},
		logging.NewForTest())
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Stop)

	metricsSvc := &metrics.Service{Repo: store}
	cfg := &config.APIConfig{RateLimitRPS: 1000, RateLimitBurst: 1000, SAASMode: true, SessionTTL: time.Hour}
	handler := routes.New(routes.Deps{
		Logger:    logging.NewForTest(),
		APIConfig: cfg,
		Repo:      store,
		Health:    handlers.NewHealthHandler(time.Now().UTC(), "test", func() error { return nil }),
		Endpoints: &handlers.EndpointHandler{Repo: store, Engine: engine, Guard: checker.NewGuard(true), Metrics: metricsSvc, Logger: logging.NewForTest()},
		Metrics:   &handlers.MetricsHandler{Metrics: metricsSvc, Repo: store, EngineStats: func() any { return engine.Stats() }, Logger: logging.NewForTest()},
		Incidents: &handlers.IncidentHandler{Repo: store, Logger: logging.NewForTest()},
		Auth:      &handlers.AuthHandler{Repo: store, Logger: logging.NewForTest(), SessionTTL: time.Hour},
		Org:       &handlers.OrgHandler{Repo: store, Logger: logging.NewForTest()},
		Public:    &handlers.PublicHandler{Repo: store, Logger: logging.NewForTest()},
	})
	return &apiServer{t: t, handler: handler, store: store, target: target}
}

// saasClient is a cookie-carrying HTTP client over the apiServer.
type saasClient struct {
	s      *apiServer
	cookie string
}

func (c *saasClient) do(method, path, body string) (*http.Response, map[string]any) {
	c.s.t.Helper()
	hdr := map[string]string{}
	if c.cookie != "" {
		hdr["Cookie"] = c.cookie
	}
	resp, out := c.s.do(method, path, body, hdr)
	for _, ck := range resp.Cookies() {
		if ck.Name == "uptimex_session" && ck.Value != "" {
			c.cookie = "uptimex_session=" + ck.Value
		}
	}
	return resp, out
}

func signupBody(email, password, orgName string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q,"org_name":%q}`, email, password, orgName)
}

func TestSaaSAuthFlow(t *testing.T) {
	s := newSaaSServer(t)

	// Anonymous data access is rejected in SaaS mode.
	if resp, _ := s.do("GET", "/api/v1/endpoints", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", resp.StatusCode)
	}

	c := &saasClient{s: s}
	resp, body := c.do("POST", "/api/v1/auth/signup", signupBody("alice@acme.test", "hunter2hunter2", "Acme Corp"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup = %d: %v", resp.StatusCode, body)
	}
	if c.cookie == "" {
		t.Fatal("signup must set a session cookie")
	}
	org := body["org"].(map[string]any)
	if org["slug"] != "acme-corp" || org["plan"] != "free" {
		t.Fatalf("org = %v", org)
	}

	// Me reflects the session.
	resp, body = c.do("GET", "/api/v1/auth/me", "")
	if resp.StatusCode != 200 {
		t.Fatalf("me = %d: %v", resp.StatusCode, body)
	}
	if body["user"].(map[string]any)["email"] != "alice@acme.test" {
		t.Fatalf("me user = %v", body["user"])
	}

	// Duplicate signup is a conflict.
	resp, _ = c.do("POST", "/api/v1/auth/signup", signupBody("alice@acme.test", "hunter2hunter2", "Other"))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate email = %d, want 409", resp.StatusCode)
	}
	// Weak password rejected.
	resp, _ = c.do("POST", "/api/v1/auth/signup", signupBody("bob@acme.test", "short", "Bob Inc"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("weak password = %d, want 400", resp.StatusCode)
	}

	// Logout clears the session.
	if resp, _ := c.do("POST", "/api/v1/auth/logout", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", resp.StatusCode)
	}

	// Login with wrong password fails; correct one works.
	resp, _ = c.do("POST", "/api/v1/auth/login", `{"email":"alice@acme.test","password":"wrongwrong"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", resp.StatusCode)
	}
	resp, body = c.do("POST", "/api/v1/auth/login", `{"email":"alice@acme.test","password":"hunter2hunter2"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("login = %d: %v", resp.StatusCode, body)
	}
	if resp, _ := c.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != 200 {
		t.Fatal("me after login should be 200")
	}
}

func TestSaaSOrgIsolationAndQuota(t *testing.T) {
	s := newSaaSServer(t)
	acme := &saasClient{s: s}
	if resp, body := acme.do("POST", "/api/v1/auth/signup", signupBody("alice@acme.test", "hunter2hunter2", "Acme Corp")); resp.StatusCode != 201 {
		t.Fatalf("acme signup = %d: %v", resp.StatusCode, body)
	}
	beta := &saasClient{s: s}
	if resp, body := beta.do("POST", "/api/v1/auth/signup", signupBody("ben@beta.test", "hunter2hunter2", "Beta LLC")); resp.StatusCode != 201 {
		t.Fatalf("beta signup = %d: %v", resp.StatusCode, body)
	}

	// Free plan: interval floor is 60s.
	tooFast := fmt.Sprintf(`{"name":"api","url":%q,"interval_seconds":30}`, s.target.URL)
	if resp, body := acme.do("POST", "/api/v1/endpoints", tooFast); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("free 30s interval = %d: %v", resp.StatusCode, body)
	}
	ok := fmt.Sprintf(`{"name":"api-%d","url":%q,"interval_seconds":60}`, 0, s.target.URL)
	resp, body := acme.do("POST", "/api/v1/endpoints", ok)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d: %v", resp.StatusCode, body)
	}
	acmeID := int64(body["id"].(float64))
	if body["org_id"] == nil {
		t.Fatalf("created endpoint must carry org_id: %v", body)
	}

	// Fill the free quota (5 total).
	for i := 1; i < 5; i++ {
		Ep := fmt.Sprintf(`{"name":"api-%d","url":%q,"interval_seconds":60}`, i, s.target.URL)
		if resp, body := acme.do("POST", "/api/v1/endpoints", Ep); resp.StatusCode != 201 {
			t.Fatalf("fill %d = %d: %v", i, resp.StatusCode, body)
		}
	}
	sixth := fmt.Sprintf(`{"name":"overflow","url":%q,"interval_seconds":60}`, s.target.URL)
	resp, body = acme.do("POST", "/api/v1/endpoints", sixth)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body["error"].(string), "limit") {
		t.Fatalf("quota = %d: %v", resp.StatusCode, body)
	}

	// Beta sees only its own (empty) world.
	resp, body = beta.do("GET", "/api/v1/endpoints", "")
	if resp.StatusCode != 200 || body["endpoints"].([]any) != nil && len(body["endpoints"].([]any)) != 0 {
		t.Fatalf("beta list = %d %v", resp.StatusCode, body)
	}
	if resp, _ := beta.do("GET", fmt.Sprintf("/api/v1/endpoints/%d", acmeID), ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("beta get acme endpoint = %d, want 404", resp.StatusCode)
	}
	if resp, _ := beta.do("DELETE", fmt.Sprintf("/api/v1/endpoints/%d", acmeID), ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("beta delete acme endpoint = %d, want 404", resp.StatusCode)
	}

	// Overview and incidents are org-scoped.
	resp, body = beta.do("GET", "/api/v1/metrics/overview?window=1h", "")
	if resp.StatusCode != 200 || body["endpoints"].(map[string]any)["total"].(float64) != 0 {
		t.Fatalf("beta overview = %d %v", resp.StatusCode, body)
	}
	resp, body = acme.do("GET", "/api/v1/metrics/overview?window=1h", "")
	if body["endpoints"].(map[string]any)["total"].(float64) != 5 {
		t.Fatalf("acme overview total = %v", body["endpoints"])
	}

	// Engine stats are operator-only in SaaS mode.
	if resp, _ := acme.do("GET", "/api/v1/stats", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tenant stats = %d, want 403", resp.StatusCode)
	}
}

func TestSaaSPlanChange(t *testing.T) {
	s := newSaaSServer(t)
	c := &saasClient{s: s}
	if resp, body := c.do("POST", "/api/v1/auth/signup", signupBody("alice@acme.test", "hunter2hunter2", "Acme Corp")); resp.StatusCode != 201 {
		t.Fatalf("signup = %d: %v", resp.StatusCode, body)
	}

	// Org payload carries the catalog.
	resp, body := c.do("GET", "/api/v1/org", "")
	if resp.StatusCode != 200 {
		t.Fatalf("org = %d", resp.StatusCode)
	}
	if len(body["plans"].([]any)) != 3 {
		t.Fatalf("plans = %v", body["plans"])
	}

	// Upgrade to pro.
	resp, body = c.do("POST", "/api/v1/org/plan", `{"plan_id":"pro"}`)
	if resp.StatusCode != 200 || body["org"].(map[string]any)["plan"] != "pro" {
		t.Fatalf("upgrade = %d: %v", resp.StatusCode, body)
	}

	// Pro allows 30s interval and 50 endpoints.
	fast := fmt.Sprintf(`{"name":"fast","url":%q,"interval_seconds":30}`, s.target.URL)
	if resp, body := c.do("POST", "/api/v1/endpoints", fast); resp.StatusCode != 201 {
		t.Fatalf("pro 30s = %d: %v", resp.StatusCode, body)
	}
	// But not 10s (that's Business).
	faster := fmt.Sprintf(`{"name":"faster","url":%q,"interval_seconds":10}`, s.target.URL)
	if resp, _ := c.do("POST", "/api/v1/endpoints", faster); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("pro 10s = %d, want 403", resp.StatusCode)
	}

	// Unknown plan rejected.
	if resp, _ := c.do("POST", "/api/v1/org/plan", `{"plan_id":"enterprise"}`); resp.StatusCode != 400 {
		t.Fatal("unknown plan should 400")
	}
}

func TestSaaSPublicStatusPage(t *testing.T) {
	s := newSaaSServer(t)
	c := &saasClient{s: s}
	resp, body := c.do("POST", "/api/v1/auth/signup", signupBody("alice@acme.test", "hunter2hunter2", "Acme Corp"))
	if resp.StatusCode != 201 {
		t.Fatalf("signup = %d: %v", resp.StatusCode, body)
	}
	Ep := fmt.Sprintf(`{"name":"Public API","url":%q,"interval_seconds":60}`, s.target.URL)
	if resp, body := c.do("POST", "/api/v1/endpoints", Ep); resp.StatusCode != 201 {
		t.Fatalf("create = %d: %v", resp.StatusCode, body)
	}

	// Anonymous status page by slug.
	resp, body = s.do("GET", "/api/v1/public/status/acme-corp", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d: %v", resp.StatusCode, body)
	}
	if body["organization"].(map[string]any)["name"] != "Acme Corp" {
		t.Fatalf("status org = %v", body["organization"])
	}
	services := body["services"].([]any)
	if len(services) != 1 || services[0].(map[string]any)["name"] != "Public API" {
		t.Fatalf("status services = %v", services)
	}

	// Unknown slug is a plain 404.
	if resp, _ := s.do("GET", "/api/v1/public/status/does-not-exist", "", nil); resp.StatusCode != 404 {
		t.Fatal("unknown slug should 404")
	}

	// Disabling the status page hides it.
	if resp, _ := c.do("PATCH", "/api/v1/org", `{"status_page_enabled":false}`); resp.StatusCode != 200 {
		t.Fatal("org patch should succeed")
	}
	if resp, _ := s.do("GET", "/api/v1/public/status/acme-corp", "", nil); resp.StatusCode != 404 {
		t.Fatal("disabled status page should 404")
	}
}

func TestSaaSAccountSecurity(t *testing.T) {
	s := newSaaSServer(t)

	// Fresh account with two concurrent sessions (two devices).
	main := &saasClient{s: s}
	if resp, body := main.do("POST", "/api/v1/auth/signup", signupBody("sec@audit.test", "first-password-1", "Sec Corp")); resp.StatusCode != 201 {
		t.Fatalf("signup = %d: %v", resp.StatusCode, body)
	}
	other := &saasClient{s: s}
	if resp, body := other.do("POST", "/api/v1/auth/login", `{"email":"sec@audit.test","password":"first-password-1"}`); resp.StatusCode != 200 {
		t.Fatalf("second device login = %d: %v", resp.StatusCode, body)
	}

	// Session list shows both, exactly one flagged current.
	resp, body := main.do("GET", "/api/v1/auth/sessions", "")
	if resp.StatusCode != 200 {
		t.Fatalf("sessions = %d: %v", resp.StatusCode, body)
	}
	sessions := body["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("session count = %d, want 2", len(sessions))
	}
	currents := 0
	for _, raw := range sessions {
		if raw.(map[string]any)["current"] == true {
			currents++
		}
	}
	if currents != 1 {
		t.Fatalf("current-flagged sessions = %d, want 1", currents)
	}
	if sessions[0].(map[string]any)["token_hash"] != nil {
		t.Fatal("token hashes must never be exposed")
	}

	// Password change rejects a wrong current password.
	resp, body = main.do("PUT", "/api/v1/auth/password", `{"current_password":"WRONG","new_password":"second-password-2"}`)
	if resp.StatusCode != 401 {
		t.Fatalf("wrong current password = %d: %v", resp.StatusCode, body)
	}
	// And a too-short new password.
	resp, _ = main.do("PUT", "/api/v1/auth/password", `{"current_password":"first-password-1","new_password":"short"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("weak new password = %d, want 400", resp.StatusCode)
	}

	// Successful change keeps this device, revokes the other one.
	resp, body = main.do("PUT", "/api/v1/auth/password", `{"current_password":"first-password-1","new_password":"second-password-2"}`)
	if resp.StatusCode != 204 {
		t.Fatalf("password change = %d: %v", resp.StatusCode, body)
	}
	if resp, _ := main.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != 200 {
		t.Fatal("current device must stay signed in")
	}
	if resp, _ := other.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != 401 {
		t.Fatal("other device must be revoked after password change")
	}
	// Old password no longer authenticates; the new one does.
	resp, _ = main.do("POST", "/api/v1/auth/login", `{"email":"sec@audit.test","password":"first-password-1"}`)
	if resp.StatusCode != 401 {
		t.Fatal("old password must be rejected")
	}
	resp, _ = main.do("POST", "/api/v1/auth/login", `{"email":"sec@audit.test","password":"second-password-2"}`)
	if resp.StatusCode != 200 {
		t.Fatal("new password must authenticate")
	}

	// Revoke-others: log in twice again, then DELETE keeps only the caller.
	other2 := &saasClient{s: s}
	other2.do("POST", "/api/v1/auth/login", `{"email":"sec@audit.test","password":"second-password-2"}`)
	if resp, _ := main.do("DELETE", "/api/v1/auth/sessions", ""); resp.StatusCode != 204 {
		t.Fatal("revoke-others should return 204")
	}
	if resp, _ := other2.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != 401 {
		t.Fatal("other device must be revoked")
	}
	if resp, _ := main.do("GET", "/api/v1/auth/me", ""); resp.StatusCode != 200 {
		t.Fatal("caller session must survive revoke-others")
	}

	// Unknown API paths answer with the JSON error contract.
	resp, body = s.do("GET", "/api/v1/does-not-exist", "", nil)
	if resp.StatusCode != 404 || body["error"] != "route not found" {
		t.Fatalf("api 404 = %d %v, want JSON route-not-found", resp.StatusCode, body)
	}
}
