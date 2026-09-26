package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"uptimex/api/middleware"
	"uptimex/internal/checker"
	"uptimex/internal/metrics"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/plans"
	"uptimex/internal/storage"
)

// EndpointHandler manages monitored endpoints. Handlers stay thin: parse,
// validate, delegate to storage/engine/metrics, encode.
type EndpointHandler struct {
	Repo    storage.Repository
	Engine  *monitor.Engine
	Guard   *checker.Guard
	Metrics *metrics.Service
	Logger  *slog.Logger
}

// endpointPayload is the create/update request body.
type endpointPayload struct {
	Name              *string `json:"name"`
	URL               *string `json:"url"`
	Method            *string `json:"method"`
	IntervalSeconds   *int    `json:"interval_seconds"`
	TimeoutMs         *int    `json:"timeout_ms"`
	FailureThreshold  *int    `json:"failure_threshold"`
	ExpectedStatusMin *int    `json:"expected_status_min"`
	ExpectedStatusMax *int    `json:"expected_status_max"`
	Enabled           *bool   `json:"enabled"`
}

var allowedMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true}

// validate checks a fully-assembled endpoint configuration.
func validateEndpoint(e *models.Endpoint, guard *checker.Guard) error {
	var errs []string
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		errs = append(errs, "name is required")
	}
	if len(e.Name) > 255 {
		errs = append(errs, "name must be at most 255 characters")
	}
	if err := guard.ValidateURL(e.URL); err != nil {
		errs = append(errs, "url: "+err.Error())
	}
	e.Method = strings.ToUpper(strings.TrimSpace(e.Method))
	if e.Method == "" {
		e.Method = http.MethodGet
	}
	if !allowedMethods[e.Method] {
		errs = append(errs, "method must be GET, HEAD or POST")
	}
	if e.IntervalSeconds < 5 || e.IntervalSeconds > 86400 {
		errs = append(errs, "interval_seconds must be between 5 and 86400")
	}
	if e.TimeoutMs < 100 || e.TimeoutMs > 60000 {
		errs = append(errs, "timeout_ms must be between 100 and 60000")
	}
	if e.FailureThreshold < 1 || e.FailureThreshold > 20 {
		errs = append(errs, "failure_threshold must be between 1 and 20")
	}
	if e.ExpectedStatusMin < 100 || e.ExpectedStatusMax > 599 || e.ExpectedStatusMin > e.ExpectedStatusMax {
		errs = append(errs, "expected_status range must satisfy 100 <= min <= max <= 599")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// --- tenancy helpers --------------------------------------------------------

// canAccessEndpoint reports whether the identity may see/modify the endpoint.
// The legacy operator scope sees everything; tenant users only their org's.
func canAccessEndpoint(id *middleware.Identity, e *models.Endpoint) bool {
	if id == nil || id.Legacy {
		return true
	}
	return e.OrgID != nil && *e.OrgID == id.OrgID
}

// enforcePlanQuota rejects creates that would exceed the org's endpoint
// quota. Legacy scope is unlimited.
func (h *EndpointHandler) enforcePlanQuota(w http.ResponseWriter, r *http.Request, id *middleware.Identity) bool {
	if id == nil || id.Legacy {
		return true
	}
	plan, _ := plans.ByID(id.Plan)
	used, err := h.Repo.CountEndpointsInOrg(r.Context(), id.OrgID)
	if err != nil {
		h.internalError(w, "quota count", err)
		return false
	}
	if used >= int64(plan.MaxEndpoints) {
		writeError(w, http.StatusForbidden, "endpoint limit reached for the "+plan.Name+
			" plan (max "+strconv.Itoa(plan.MaxEndpoints)+"). Upgrade your plan to add more monitors.")
		return false
	}
	return true
}

// enforcePlanInterval rejects intervals faster than the plan allows.
func enforcePlanInterval(w http.ResponseWriter, id *middleware.Identity, intervalSeconds int) bool {
	if id == nil || id.Legacy {
		return true
	}
	plan, _ := plans.ByID(id.Plan)
	if intervalSeconds < plan.MinIntervalSeconds {
		writeError(w, http.StatusForbidden, "check interval "+strconv.Itoa(intervalSeconds)+
			"s is below the "+plan.Name+" plan minimum of "+strconv.Itoa(plan.MinIntervalSeconds)+
			"s. Upgrade for faster checks.")
		return false
	}
	return true
}

// List returns endpoint configuration + live state + windowed metrics.
func (h *EndpointHandler) List(w http.ResponseWriter, r *http.Request) {
	window, ok := h.windowParam(w, r)
	if !ok {
		return
	}
	id := middleware.IdentityFromContext(r.Context())
	var rows []metrics.EndpointSummary
	var err error
	if id != nil && id.Session {
		rows, err = h.Metrics.EndpointSummariesInOrg(r.Context(), id.OrgID, window)
	} else {
		rows, err = h.Metrics.EndpointSummaries(r.Context(), window)
	}
	if err != nil {
		h.internalError(w, "list endpoints", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": rows, "window": metrics.WindowString(window)})
}

// Create registers a new endpoint (PRD §21 example payload).
func (h *EndpointHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p endpointPayload
	if !h.decode(w, r, &p) {
		return
	}
	e := models.Endpoint{}
	if p.Name != nil {
		e.Name = *p.Name
	}
	if p.URL != nil {
		e.URL = strings.TrimSpace(*p.URL)
	}
	if p.Method != nil {
		e.Method = *p.Method
	}
	if p.IntervalSeconds != nil {
		e.IntervalSeconds = *p.IntervalSeconds
	} else {
		e.IntervalSeconds = 30
	}
	if p.TimeoutMs != nil {
		e.TimeoutMs = *p.TimeoutMs
	} else {
		e.TimeoutMs = 5000
	}
	if p.FailureThreshold != nil {
		e.FailureThreshold = *p.FailureThreshold
	} else {
		e.FailureThreshold = 3
	}
	if p.ExpectedStatusMin != nil {
		e.ExpectedStatusMin = *p.ExpectedStatusMin
	} else {
		e.ExpectedStatusMin = 200
	}
	if p.ExpectedStatusMax != nil {
		e.ExpectedStatusMax = *p.ExpectedStatusMax
	} else {
		e.ExpectedStatusMax = 299
	}
	if p.Enabled != nil {
		e.Enabled = *p.Enabled
	} else {
		e.Enabled = true
	}

	if err := validateEndpoint(&e, h.Guard); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := middleware.IdentityFromContext(r.Context())
	if !h.enforcePlanQuota(w, r, id) {
		return
	}
	if !enforcePlanInterval(w, id, e.IntervalSeconds) {
		return
	}
	if id != nil && id.Session {
		orgID := id.OrgID
		e.OrgID = &orgID
	}
	if err := h.Repo.CreateEndpoint(r.Context(), &e); err != nil {
		h.internalError(w, "create endpoint", err)
		return
	}
	if err := h.Engine.ReloadEndpoints(r.Context()); err != nil {
		h.Logger.Warn("engine reload failed after create", "error", err)
	}
	h.Logger.Info("endpoint_created", "endpoint_id", e.ID, "name", e.Name, "url", e.URL)
	writeJSON(w, http.StatusCreated, e)
}

// getOwnedEndpoint loads an endpoint and enforces tenant ownership.
func (h *EndpointHandler) getOwnedEndpoint(w http.ResponseWriter, r *http.Request) (*models.Endpoint, bool) {
	id, ok := h.idParam(w, r)
	if !ok {
		return nil, false
	}
	e, err := h.Repo.GetEndpoint(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return nil, false
	}
	if err != nil {
		h.internalError(w, "get endpoint", err)
		return nil, false
	}
	if !canAccessEndpoint(middleware.IdentityFromContext(r.Context()), e) {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return nil, false
	}
	return e, true
}

// Get returns one endpoint with its live status and open incident.
func (h *EndpointHandler) Get(w http.ResponseWriter, r *http.Request) {
	e, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	resp := map[string]any{"endpoint": e}
	if st, err := h.Repo.GetStatus(r.Context(), e.ID); err == nil {
		resp["status"] = st
	}
	if inc, err := h.Repo.GetOpenIncident(r.Context(), e.ID); err == nil {
		resp["open_incident"] = inc
	}
	writeJSON(w, http.StatusOK, resp)
}

// Update partially updates an endpoint (PATCH semantics).
func (h *EndpointHandler) Update(w http.ResponseWriter, r *http.Request) {
	existing, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	id := existing.ID

	var p endpointPayload
	if !h.decode(w, r, &p) {
		return
	}
	if p.Name != nil {
		existing.Name = *p.Name
	}
	if p.URL != nil {
		existing.URL = strings.TrimSpace(*p.URL)
	}
	if p.Method != nil {
		existing.Method = *p.Method
	}
	if p.IntervalSeconds != nil {
		existing.IntervalSeconds = *p.IntervalSeconds
	}
	if p.TimeoutMs != nil {
		existing.TimeoutMs = *p.TimeoutMs
	}
	if p.FailureThreshold != nil {
		existing.FailureThreshold = *p.FailureThreshold
	}
	if p.ExpectedStatusMin != nil {
		existing.ExpectedStatusMin = *p.ExpectedStatusMin
	}
	if p.ExpectedStatusMax != nil {
		existing.ExpectedStatusMax = *p.ExpectedStatusMax
	}
	if p.Enabled != nil {
		existing.Enabled = *p.Enabled
	}

	if err := validateEndpoint(existing, h.Guard); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !enforcePlanInterval(w, middleware.IdentityFromContext(r.Context()), existing.IntervalSeconds) {
		return
	}
	if err := h.Repo.UpdateEndpoint(r.Context(), existing); err != nil {
		h.internalError(w, "update endpoint", err)
		return
	}
	if err := h.Engine.ReloadEndpoints(r.Context()); err != nil {
		h.Logger.Warn("engine reload failed after update", "error", err)
	}
	h.Logger.Info("endpoint_updated", "endpoint_id", id)
	writeJSON(w, http.StatusOK, existing)
}

// Delete removes an endpoint and all dependent rows (cascade).
func (h *EndpointHandler) Delete(w http.ResponseWriter, r *http.Request) {
	e, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	id := e.ID
	if err := h.Repo.DeleteEndpoint(r.Context(), id); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "endpoint not found")
		return
	} else if err != nil {
		h.internalError(w, "delete endpoint", err)
		return
	}
	if err := h.Engine.ReloadEndpoints(r.Context()); err != nil {
		h.Logger.Warn("engine reload failed after delete", "error", err)
	}
	h.Logger.Info("endpoint_deleted", "endpoint_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// Checks returns historical raw checks (?since=<rfc3339|duration>&limit=).
func (h *EndpointHandler) Checks(w http.ResponseWriter, r *http.Request) {
	e, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	id := e.ID

	limit := intParam(r, "limit", 200)
	if limit < 1 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
		return
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	if s := r.URL.Query().Get("since"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			since = time.Now().UTC().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t.UTC()
		} else {
			writeError(w, http.StatusBadRequest, "since must be a duration (e.g. 6h) or RFC3339 timestamp")
			return
		}
	}
	checks, err := h.Repo.ListChecks(r.Context(), id, since, limit)
	if err != nil {
		h.internalError(w, "list checks", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks, "count": len(checks)})
}

// EndpointMetrics returns per-endpoint analytics (?window=1h|24h|7d).
func (h *EndpointHandler) EndpointMetrics(w http.ResponseWriter, r *http.Request) {
	e, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	id := e.ID
	window, ok := h.windowParam(w, r)
	if !ok {
		return
	}
	m, err := h.Metrics.EndpointMetricsFor(r.Context(), id, window)
	if err != nil {
		h.internalError(w, "endpoint metrics", err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// Test runs a single on-demand probe and returns the raw result without
// feeding the failure state machine (manual diagnostics must not distort
// the incident record).
func (h *EndpointHandler) Test(w http.ResponseWriter, r *http.Request) {
	e, ok := h.getOwnedEndpoint(w, r)
	if !ok {
		return
	}
	id := e.ID
	ctx, cancel := contextWithTimeout(r.Context(), time.Duration(e.TimeoutMs+2000)*time.Millisecond)
	defer cancel()
	res := h.Engine.RunCheckNow(ctx, *e)
	h.Logger.Info("on_demand_check", "endpoint_id", id, "success", res.Success, "status", codeOrZero(res.StatusCode), "error_type", res.ErrorType)
	writeJSON(w, http.StatusOK, res)
}

// --- helpers -------------------------------------------------------------

func (h *EndpointHandler) idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid endpoint id")
		return 0, false
	}
	return id, true
}

func (h *EndpointHandler) windowParam(w http.ResponseWriter, r *http.Request) (time.Duration, bool) {
	window, err := metrics.ParseWindow(r.URL.Query().Get("window"), metrics.DefaultWindow)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return window, true
}

func (h *EndpointHandler) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (h *EndpointHandler) internalError(w http.ResponseWriter, op string, err error) {
	logInternalError(w, h.Logger, op, err)
}

// logInternalError is the shared 500 path for all handlers.
func logInternalError(w http.ResponseWriter, logger *slog.Logger, op string, err error) {
	logger.Error("handler_error", "op", op, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func intParam(r *http.Request, name string, def int) int {
	if s := r.URL.Query().Get(name); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

func codeOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func contextWithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = 7 * time.Second
	}
	return context.WithTimeout(parent, d)
}
