// Package routes assembles the HTTP surface of the monitor service:
// middleware chain + route registration. It is the single place where URL
// paths map to handlers.
package routes

import (
	"log/slog"
	"net/http"

	"uptimex/api/handlers"
	"uptimex/api/middleware"
	"uptimex/internal/config"
)

// Deps carries every handler dependency. Keeping them explicit avoids global
// state and makes the wiring testable.
type Deps struct {
	Logger    *slog.Logger
	APIConfig *config.APIConfig
	Health    *handlers.HealthHandler
	// Filled in by later phases; nil handlers are simply not registered.
	Endpoints *handlers.EndpointHandler
	Metrics   *handlers.MetricsHandler
	Incidents *handlers.IncidentHandler
}

// New builds the root http.Handler. Middleware runs outermost-first:
// Recovery -> Logging -> CORS -> RateLimit -> mux.
func New(d Deps) http.Handler {
	mux := buildMux(d)
	var h http.Handler = mux
	h = middleware.NewRateLimit(d.APIConfig.RateLimitRPS, d.APIConfig.RateLimitBurst, h)
	if d.APIConfig.CORSAllowedOrigin != "" {
		h = &middleware.CORS{AllowedOrigin: d.APIConfig.CORSAllowedOrigin, Next: h}
	}
	h = &middleware.Logging{Logger: d.Logger, Next: h}
	h = &middleware.Recovery{Logger: d.Logger, Next: h}
	return h
}

// buildMux registers all routes. Separated from New so tests can reuse it.
func buildMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", d.Health.Liveness)
	mux.HandleFunc("GET /ready", d.Health.Readiness)

	if d.Endpoints != nil {
		mux.HandleFunc("GET /api/v1/endpoints", d.Endpoints.List)
		mux.Handle("POST /api/v1/endpoints", authed(d, d.Endpoints.Create))
		mux.HandleFunc("GET /api/v1/endpoints/{id}", d.Endpoints.Get)
		mux.Handle("PATCH /api/v1/endpoints/{id}", authed(d, d.Endpoints.Update))
		mux.Handle("DELETE /api/v1/endpoints/{id}", authed(d, d.Endpoints.Delete))
		mux.HandleFunc("GET /api/v1/endpoints/{id}/checks", d.Endpoints.Checks)
		mux.HandleFunc("GET /api/v1/endpoints/{id}/metrics", d.Endpoints.EndpointMetrics)
		mux.Handle("POST /api/v1/endpoints/{id}/test", authed(d, d.Endpoints.Test))
	}

	if d.Metrics != nil {
		mux.HandleFunc("GET /api/v1/metrics/overview", d.Metrics.Overview)
		mux.HandleFunc("GET /api/v1/stats", d.Metrics.Stats)
	}

	if d.Incidents != nil {
		mux.HandleFunc("GET /api/v1/incidents", d.Incidents.List)
	}

	return mux
}

// authed wraps administrative routes with API-key authentication.
func authed(d Deps, h http.HandlerFunc) http.Handler {
	return &middleware.APIKeyAuth{Key: d.APIConfig.APIKey, Next: h}
}
