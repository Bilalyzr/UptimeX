package heartbeatchecker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"uptimex/internal/logging"
	"uptimex/internal/models"
)

func TestCheckerToleratesMissesBeforeAlerting(t *testing.T) {
	var (
		mu      sync.Mutex
		healthy = true
		downs   int
		recs    int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		h := healthy
		mu.Unlock()
		if !h {
			// Simulate a dead monitor: hang until the client times out.
			time.Sleep(500 * time.Millisecond)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","service":"uptimex","heartbeat_at":"` + time.Now().UTC().Format(time.RFC3339Nano) + `"}`))
	}))
	defer srv.Close()

	c := New(Config{
		MonitorURL:         srv.URL,
		Interval:           20 * time.Millisecond,
		MissesThreshold:    3,
		Timeout:            80 * time.Millisecond,
		Logger:             logging.NewForTest(),
		OnMonitorDown:      func(ctx context.Context, reason string, misses int) { downs++ },
		OnMonitorRecovered: func(ctx context.Context, down time.Duration) { recs++ },
	})

	// Healthy phase: no alerts.
	for i := 0; i < 5; i++ {
		if err := c.Probe(context.Background()); err != nil {
			t.Fatalf("healthy probe failed: %v", err)
		}
	}

	// Monitor dies: two misses must NOT alert (tolerance).
	mu.Lock()
	healthy = false
	mu.Unlock()
	c.poll(context.Background())
	c.poll(context.Background())
	if downs != 0 || c.Alerted() {
		t.Fatal("must tolerate misses below threshold")
	}

	// Third miss triggers exactly one alert.
	c.poll(context.Background())
	if downs != 1 {
		t.Fatalf("downs = %d, want 1", downs)
	}
	if !c.Alerted() {
		t.Fatal("alerted state must be set")
	}

	// Further misses must not re-alert.
	c.poll(context.Background())
	c.poll(context.Background())
	if downs != 1 {
		t.Fatalf("duplicate alerts: %d", downs)
	}

	// Recovery restores state and notifies once.
	mu.Lock()
	healthy = true
	mu.Unlock()
	c.poll(context.Background())
	if downs != 1 || recs != 1 {
		t.Fatalf("downs=%d recs=%d, want 1/1", downs, recs)
	}
	if c.Alerted() || c.Misses() != 0 {
		t.Fatal("checker must reset after recovery")
	}
}

func TestProbeRejectsNonHealthyResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := New(Config{MonitorURL: srv.URL, Logger: logging.NewForTest()})
	if err := c.Probe(context.Background()); err == nil {
		t.Fatal("503 must be a missed heartbeat")
	}
}

func TestProbeRejectsStaleHeartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stale := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339Nano)
		_, _ = w.Write([]byte(`{"status":"healthy","heartbeat_at":"` + stale + `"}`))
	}))
	defer srv.Close()
	c := New(Config{MonitorURL: srv.URL, Logger: logging.NewForTest()})
	if err := c.Probe(context.Background()); err == nil {
		t.Fatal("stale heartbeat_at must count as a miss")
	}
}

func TestProbeUnreachableMonitor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New(Config{MonitorURL: url, Timeout: 500 * time.Millisecond, Logger: logging.NewForTest()})
	if err := c.Probe(context.Background()); err == nil {
		t.Fatal("connection refused must be a missed heartbeat")
	}
}

func TestBuildDownAlert(t *testing.T) {
	a := BuildDownAlert("http://m:8080/health", "dial timeout", 3, "m1")
	if a.Type != models.AlertMonitorDown {
		t.Fatalf("type = %s", a.Type)
	}
	if a.Message == "" || a.EndpointID != 0 {
		t.Fatalf("alert = %+v", a)
	}
}
