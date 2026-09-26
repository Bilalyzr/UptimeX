// Package handlers contains the HTTP handlers for the monitor REST API.
// Handlers only translate HTTP <-> domain concerns; business logic lives in
// the internal packages.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"uptimex/internal/version"
)

// Health carries the liveness heartbeat payload (PRD §17 checker-of-checkers).
type Health struct {
	Status        string    `json:"status"`
	Service       string    `json:"service"`
	Version       string    `json:"version"`
	Monitor       string    `json:"monitor,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	HeartbeatAt   time.Time `json:"heartbeat_at"`
}

// Readiness reports dependency health (database reachability).
type Readiness struct {
	Status string `json:"status"`
	DB     string `json:"database"`
}

// HealthHandler serves liveness and readiness endpoints.
type HealthHandler struct {
	startedAt time.Time
	monitor   string
	// dbPing must return nil when the storage layer is usable. It is allowed
	// to be nil (service starting up).
	dbPing func() error
}

// NewHealthHandler builds a health handler. dbPing may be nil.
func NewHealthHandler(startedAt time.Time, monitor string, dbPing func() error) *HealthHandler {
	return &HealthHandler{startedAt: startedAt, monitor: monitor, dbPing: dbPing}
}

// Liveness always answers 200 while the process is able to serve. It never
// depends on the database: a dead database must not stop the endpoint that
// the external heartbeat checker relies on.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, Health{
		Status:        "healthy",
		Service:       version.ServiceName,
		Version:       version.Version,
		Monitor:       h.monitor,
		StartedAt:     h.startedAt,
		UptimeSeconds: int64(now.Sub(h.startedAt).Seconds()),
		HeartbeatAt:   now,
	})
}

// Readiness answers 200 only when the process can serve traffic usefully
// (database reachable). Orchestrators should use this, not liveness.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	status := http.StatusOK
	if h.dbPing != nil {
		if err := h.dbPing(); err != nil {
			dbStatus = "unavailable"
			status = http.StatusServiceUnavailable
		}
	}
	writeJSON(w, status, Readiness{Status: map[bool]string{true: "ready", false: "degraded"}[status == http.StatusOK], DB: dbStatus})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
