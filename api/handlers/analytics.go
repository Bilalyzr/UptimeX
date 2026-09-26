package handlers

import (
	"log/slog"
	"net/http"

	"uptimex/api/middleware"
	"uptimex/internal/metrics"
	"uptimex/internal/models"
	"uptimex/internal/storage"
)

// MetricsHandler serves global analytics and engine self-observability.
type MetricsHandler struct {
	Metrics *metrics.Service
	Repo    storage.Repository
	// EngineStats snapshots live engine counters (pool, scheduler, checks).
	EngineStats func() any
	Logger      *slog.Logger
}

// Overview returns monitoring metrics (?window=1h|24h|7d), scoped to the
// caller's organization in SaaS mode.
func (h *MetricsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	window, err := metrics.ParseWindow(r.URL.Query().Get("window"), metrics.DefaultWindow)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var ov *metrics.OverviewMetrics
	if id := middleware.IdentityFromContext(r.Context()); id != nil && id.Session {
		ov, err = h.Metrics.OverviewInOrg(r.Context(), id.OrgID, window)
	} else {
		ov, err = h.Metrics.Overview(r.Context(), window)
	}
	if err != nil {
		h.Logger.Error("handler_error", "op", "overview", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// Stats returns live process metrics: checks attempted/succeeded/failed,
// worker utilization and queue depth (PRD §27). Engine stats span every
// tenant, so they are operator-only.
func (h *MetricsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if id := middleware.IdentityFromContext(r.Context()); id != nil && id.Session {
		writeError(w, http.StatusForbidden, "engine stats are available to operators only")
		return
	}
	if h.EngineStats == nil {
		writeError(w, http.StatusNotImplemented, "engine stats unavailable")
		return
	}
	writeJSON(w, http.StatusOK, h.EngineStats())
}

// IncidentHandler serves incident history.
type IncidentHandler struct {
	Repo   storage.Repository
	Logger *slog.Logger
}

// List returns incidents (?status=open|resolved|all&limit=, newest first).
func (h *IncidentHandler) List(w http.ResponseWriter, r *http.Request) {
	status := ""
	switch s := r.URL.Query().Get("status"); s {
	case "", "all":
		status = ""
	case "open":
		status = models.IncidentOpen
	case "resolved":
		status = models.IncidentResolved
	default:
		writeError(w, http.StatusBadRequest, "status must be open, resolved or all")
		return
	}
	limit := intParam(r, "limit", 100)
	if limit < 1 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
		return
	}
	var incidents []models.IncidentDetail
	var err error
	if id := middleware.IdentityFromContext(r.Context()); id != nil && id.Session {
		incidents, err = h.Repo.ListIncidentsInOrg(r.Context(), id.OrgID, status, limit)
	} else {
		incidents, err = h.Repo.ListIncidents(r.Context(), status, limit)
	}
	if err != nil {
		h.Logger.Error("handler_error", "op", "list incidents", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": incidents, "count": len(incidents)})
}
