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
	"uptimex/internal/storage"
)

// Deps carries every handler dependency. Keeping them explicit avoids global
// state and makes the wiring testable.
type Deps struct {
	Logger    *slog.Logger
	APIConfig *config.APIConfig
	Repo      storage.Repository // identity resolution (sessions, orgs)
	Health    *handlers.HealthHandler
	// Filled in by later phases; nil handlers are simply not registered.
	Endpoints *handlers.EndpointHandler
	Metrics   *handlers.MetricsHandler
	Incidents *handlers.IncidentHandler
	Auth      *handlers.AuthHandler
	Org       *handlers.OrgHandler
	Public    *handlers.PublicHandler
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

	if d.Auth != nil {
		mux.HandleFunc("POST /api/v1/auth/signup", d.Auth.Signup)
		mux.HandleFunc("POST /api/v1/auth/login", d.Auth.Login)
		mux.HandleFunc("POST /api/v1/auth/logout", d.Auth.Logout)
		mux.Handle("GET /api/v1/auth/me", requireSession(d, d.Auth.Me))
	}

	if d.Public != nil {
		mux.HandleFunc("GET /api/v1/public/status/{slug}", d.Public.StatusBySlug)
	}

	if d.Org != nil {
		mux.Handle("GET /api/v1/org", requireSession(d, d.Org.Get))
		mux.Handle("PATCH /api/v1/org", requireSession(d, d.Org.Update))
		mux.Handle("POST /api/v1/org/plan", requireSession(d, d.Org.ChangePlan))
	}

	if d.Endpoints != nil {
		mux.Handle("GET /api/v1/endpoints", open(d, d.Endpoints.List))
		mux.Handle("POST /api/v1/endpoints", authed(d, d.Endpoints.Create))
		mux.Handle("GET /api/v1/endpoints/{id}", open(d, d.Endpoints.Get))
		mux.Handle("PATCH /api/v1/endpoints/{id}", authed(d, d.Endpoints.Update))
		mux.Handle("DELETE /api/v1/endpoints/{id}", authed(d, d.Endpoints.Delete))
		mux.Handle("GET /api/v1/endpoints/{id}/checks", open(d, d.Endpoints.Checks))
		mux.Handle("GET /api/v1/endpoints/{id}/metrics", open(d, d.Endpoints.EndpointMetrics))
		mux.Handle("POST /api/v1/endpoints/{id}/test", authed(d, d.Endpoints.Test))
	}

	if d.Metrics != nil {
		mux.Handle("GET /api/v1/metrics/overview", open(d, d.Metrics.Overview))
		mux.Handle("GET /api/v1/stats", open(d, d.Metrics.Stats))
	}

	if d.Incidents != nil {
		mux.Handle("GET /api/v1/incidents", open(d, d.Incidents.List))
	}

	return mux
}

// resolve wraps a handler with identity resolution (session cookie or global
// API key). It never rejects.
func resolve(d Deps, next http.Handler) http.Handler {
	return &middleware.ResolveIdentity{Repo: d.Repo, APIKey: d.APIConfig.APIKey, Next: next}
}

// gate enforces authentication: a tenant session, the operator API key, or
// (only outside SaaS mode) the open development mode.
func gate(d Deps, next http.Handler) http.Handler {
	return &middleware.Gate{APIKey: d.APIConfig.APIKey, SAASMode: d.APIConfig.SAASMode, Next: next}
}

// authed wraps routes that mutate state: always gated, in every mode.
// Legacy deployments keep the pre-SaaS contract (API key when configured,
// open when not) and additionally accept login sessions.
func authed(d Deps, h http.HandlerFunc) http.Handler {
	return resolve(d, gate(d, h))
}

// open wraps read routes: ungated in legacy deployments (matching the
// pre-SaaS API), gated in SaaS mode so tenants cannot read each other's data.
func open(d Deps, h http.HandlerFunc) http.Handler {
	if d.APIConfig.SAASMode {
		return resolve(d, gate(d, h))
	}
	return resolve(d, h)
}

// requireSession wraps routes that exist only for logged-in tenant users.
func requireSession(d Deps, h http.HandlerFunc) http.Handler {
	return resolve(d, &middleware.RequireSession{Next: h})
}
