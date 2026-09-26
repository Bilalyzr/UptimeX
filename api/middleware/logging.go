// Package middleware provides cross-cutting HTTP concerns: request logging,
// panic recovery, CORS, API-key authentication and rate limiting.
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseRecorder captures the status code written by downstream handlers.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logging logs one structured line per request: method, path, status,
// duration and client address. Health probes are excluded to avoid noise.
type Logging struct {
	Logger *slog.Logger
	Next   http.Handler
}

func (m *Logging) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" || r.URL.Path == "/ready" {
		m.Next.ServeHTTP(w, r)
		return
	}
	start := time.Now()
	rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
	m.Next.ServeHTTP(rec, r)
	m.Logger.Info("http_request",
		"method", r.Method,
		"path", r.URL.Path,
		"status", rec.status,
		"duration_ms", time.Since(start).Milliseconds(),
		"remote", r.RemoteAddr,
	)
}

// Recovery converts panics into 500 responses and logs the panic. A probe or
// handler bug must never take the API process down.
type Recovery struct {
	Logger *slog.Logger
	Next   http.Handler
}

func (m *Recovery) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			m.Logger.Error("panic_recovered", "panic", rec, "path", r.URL.Path)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		}
	}()
	m.Next.ServeHTTP(w, r)
}
