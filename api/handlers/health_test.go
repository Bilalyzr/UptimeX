package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"uptimex/api/handlers"
)

func TestLiveness(t *testing.T) {
	h := handlers.NewHealthHandler(time.Now().UTC().Add(-90*time.Second), "monitor-1", nil)
	srv := httptest.NewServer(http.HandlerFunc(h.Liveness))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status      string `json:"status"`
		Service     string `json:"service"`
		Uptime      int64  `json:"uptime_seconds"`
		HeartbeatAt string `json:"heartbeat_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "healthy" || body.Service != "uptimex" {
		t.Errorf("status/service = %q/%q, want healthy/uptimex", body.Status, body.Service)
	}
	if body.Uptime < 89 || body.Uptime > 95 {
		t.Errorf("uptime_seconds = %d, want ~90", body.Uptime)
	}
	if body.HeartbeatAt == "" {
		t.Error("heartbeat_at must be present")
	}
}

func TestLivenessSurvivesDatabaseFailure(t *testing.T) {
	// Liveness must never depend on the database: the external checker
	// depends on it, and a DB outage must not kill the heartbeat.
	h := handlers.NewHealthHandler(time.Now().UTC(), "m", func() error {
		return errFake{}
	})
	rec := httptest.NewRecorder()
	h.Liveness(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness status = %d, want 200 even when DB is down", rec.Code)
	}
}

func TestReadinessReflectsDatabase(t *testing.T) {
	h := handlers.NewHealthHandler(time.Now().UTC(), "m", func() error { return nil })
	rec := httptest.NewRecorder()
	h.Readiness(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want 200", rec.Code)
	}

	h2 := handlers.NewHealthHandler(time.Now().UTC(), "m", func() error { return errFake{} })
	rec2 := httptest.NewRecorder()
	h2.Readiness(rec2, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d, want 503 when DB is down", rec2.Code)
	}
}

type errFake struct{}

func (errFake) Error() string { return "database unavailable" }
