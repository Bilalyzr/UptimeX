package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"uptimex/internal/models"
)

func testEndpoint(url string) models.Endpoint {
	return models.Endpoint{
		ID: 1, Name: "test", URL: url, Method: "GET",
		IntervalSeconds: 30, TimeoutMs: 2000, FailureThreshold: 3,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true,
	}
}

func TestCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), testEndpoint(srv.URL))

	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.StatusCode == nil || *res.StatusCode != 200 {
		t.Fatalf("status = %v", res.StatusCode)
	}
	if res.ResponseTimeMs < 0 {
		t.Fatalf("negative latency: %d", res.ResponseTimeMs)
	}
	if res.ErrorType != "" {
		t.Fatalf("unexpected error type %q", res.ErrorType)
	}
}

func TestCheckHTTPErrorOutsideExpectedRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), testEndpoint(srv.URL))

	if res.Success || res.StatusCode == nil || *res.StatusCode != 503 {
		t.Fatalf("expected 503 failure, got %+v", res)
	}
	if res.ErrorType != models.ErrTypeHTTPError {
		t.Fatalf("error type = %q, want http_error", res.ErrorType)
	}
}

func TestCheckCustomExpectedRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	ep := testEndpoint(srv.URL)
	ep.ExpectedStatusMin, ep.ExpectedStatusMax = 200, 204
	c := NewHTTPChecker(NewGuard(true))
	if res := c.Check(context.Background(), ep); !res.Success {
		t.Fatalf("204 must satisfy 200-204: %+v", res)
	}

	ep.ExpectedStatusMin = 200
	ep.ExpectedStatusMax = 202
	if res := c.Check(context.Background(), ep); res.Success {
		t.Fatal("204 must fail range 200-202")
	}
}

func TestCheckTimeoutIsRecordedAsTimeoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ep := testEndpoint(srv.URL)
	ep.TimeoutMs = 100 // far below handler latency
	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), ep)

	if res.Success {
		t.Fatal("timeout must be a failed check")
	}
	if res.ErrorType != models.ErrTypeTimeout {
		t.Fatalf("error type = %q, want timeout (%s)", res.ErrorType, res.ErrorMessage)
	}
	if res.StatusCode != nil {
		t.Fatalf("no status expected on timeout, got %d", *res.StatusCode)
	}
}

func TestCheckConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // port now refuses connections

	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), testEndpoint(url))

	if res.Success || res.ErrorType != models.ErrTypeConnection {
		t.Fatalf("expected connection error, got %+v", res)
	}
}

func TestCheckDNSFailure(t *testing.T) {
	c := NewHTTPChecker(NewGuard(true))
	ep := testEndpoint("http://this-domain-does-not-exist-hm.invalid/health")
	res := c.Check(context.Background(), ep)

	if res.Success || res.ErrorType != models.ErrTypeDNS {
		t.Fatalf("expected dns error, got %+v", res)
	}
}

func TestCheckTLSError(t *testing.T) {
	// httptest.NewTLSServer uses a cert not trusted by the system pool.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), testEndpoint(srv.URL))

	if res.Success || res.ErrorType != models.ErrTypeTLS {
		t.Fatalf("expected tls error, got %+v", res)
	}
}

func TestCheckRedirectsFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ep := testEndpoint(srv.URL + "/start")
	c := NewHTTPChecker(NewGuard(true))
	res := c.Check(context.Background(), ep)
	if !res.Success || res.StatusCode == nil || *res.StatusCode != 200 {
		t.Fatalf("redirect should be followed to 200: %+v", res)
	}
}

func TestGuardBlocksPrivateTargetsByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	strict := NewHTTPChecker(NewGuard(false))
	res := strict.Check(context.Background(), testEndpoint(srv.URL)) // 127.0.0.1
	if res.ErrorType != models.ErrTypeBlocked {
		t.Fatalf("strict guard must block loopback target, got %+v", res)
	}

	permissive := NewHTTPChecker(NewGuard(true))
	if res := permissive.Check(context.Background(), testEndpoint(srv.URL)); !res.Success {
		t.Fatalf("permissive guard must allow loopback, got %+v", res)
	}
}

func TestGuardValidateURLRules(t *testing.T) {
	g := NewGuard(false)
	bad := []string{
		"ftp://example.com/x",          // scheme
		"://missing-scheme",            // malformed
		"http://",                      // no host
		"http://user:pass@example.com", // credentials
		"http://192.168.1.1/health",    // literal private IP
		"http://169.254.169.254/meta",  // cloud metadata
		"http://10.0.0.5:70000/",       // bad port
	}
	for _, u := range bad {
		if err := g.ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) should fail", u)
		}
	}
	good := []string{
		"https://api.example.com/health",
		"http://example.com:8080/ping",
	}
	for _, u := range good {
		if err := g.ValidateURL(u); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", u, err)
		}
	}
}

func TestCheckNeverPanicsOnGarbageURL(t *testing.T) {
	c := NewHTTPChecker(NewGuard(true))
	ep := testEndpoint("http://[::1]:namedport/")
	ep.URL = "not a url at all"
	res := c.Check(context.Background(), ep) // must not panic
	if res.Success || res.ErrorType == "" {
		t.Fatalf("garbage URL must produce structured failure: %+v", res)
	}
}
