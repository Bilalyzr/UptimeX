// Public status pages: an unauthenticated, per-tenant view of service
// health at /api/v1/public/status/{slug}. Organizations can turn theirs off.
package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"uptimex/internal/metrics"
	"uptimex/internal/models"
	"uptimex/internal/storage"
)

// PublicHandler serves /api/v1/public/*.
type PublicHandler struct {
	Repo    storage.Repository
	Logger  *slog.Logger
}

// statusWindow is the analytics window behind the public uptime numbers.
const statusWindow = 24 * time.Hour

// PublicService is one service row on the status page.
type PublicService struct {
	ID             int64                `json:"id"`
	Name           string               `json:"name"`
	State          models.EndpointState `json:"state"`
	UptimePct      *float64             `json:"uptime_pct"`
	P95Ms          *float64             `json:"p95_ms"`
	LastCheckedAt  *time.Time           `json:"last_checked_at"`
	OpenIncidentID *int64               `json:"open_incident_id"`
}

// StatusBySlug renders the public status page payload for an organization.
func (h *PublicHandler) StatusBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "status page slug is required")
		return
	}
	org, err := h.Repo.GetOrganizationBySlug(r.Context(), slug)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && !org.StatusPageEnabled) {
		// Indistinguishable on purpose: private pages are not discoverable.
		writeError(w, http.StatusNotFound, "status page not found")
		return
	}
	if err != nil {
		logInternalError(w, h.Logger, "status: get org", err)
		return
	}

	endpoints, err := h.Repo.ListEndpoints(r.Context(), false)
	if err != nil {
		logInternalError(w, h.Logger, "status: list endpoints", err)
		return
	}
	statuses, err := h.Repo.ListStatuses(r.Context())
	if err != nil {
		logInternalError(w, h.Logger, "status: list statuses", err)
		return
	}
	stateByEndpoint := map[int64]models.EndpointStatus{}
	for _, st := range statuses {
		stateByEndpoint[st.EndpointID] = st
	}

	from := time.Now().UTC().Add(-statusWindow)
	samplesByEp, err := h.Repo.SelectSamplesInOrg(r.Context(), org.ID, from, metrics.MaxSamplesPerEndpoint)
	if err != nil {
		logInternalError(w, h.Logger, "status: samples", err)
		return
	}
	openIncidents, err := h.Repo.ListIncidentsInOrg(r.Context(), org.ID, models.IncidentOpen, 50)
	if err != nil {
		logInternalError(w, h.Logger, "status: incidents", err)
		return
	}
	openByEndpoint := map[int64]int64{}
	for _, inc := range openIncidents {
		openByEndpoint[inc.EndpointID] = inc.ID
	}

	services := []PublicService{}
	var allLatencies []int64
	totalChecks, successChecks := 0, 0
	overallState := models.StateHealthy
	for _, e := range endpoints {
		if e.OrgID == nil || *e.OrgID != org.ID || !e.Enabled {
			continue
		}
		svc := PublicService{ID: e.ID, Name: e.Name}
		if st, ok := stateByEndpoint[e.ID]; ok {
			svc.State = st.State
			svc.LastCheckedAt = st.LastCheckedAt
		} else {
			svc.State = models.StateHealthy
		}
		if svc.State != models.StateHealthy {
			overallState = svc.State
		}
		if id, ok := openByEndpoint[e.ID]; ok {
			svc.OpenIncidentID = &id
		}
		if samples := samplesByEp[e.ID]; len(samples) > 0 {
			var ok int
			values := make([]int64, 0, len(samples))
			for _, smp := range samples {
				values = append(values, smp.ResponseTimeMs)
				if smp.Success {
					ok++
				}
			}
			uptime := metrics.Pct(int64(ok), int64(len(samples)))
			lat := metrics.SummarizeLatency(values)
			svc.UptimePct = &uptime
			svc.P95Ms = &lat.P95Ms
			totalChecks += len(samples)
			successChecks += ok
			allLatencies = append(allLatencies, values...)
		}
		services = append(services, svc)
	}

	overall := map[string]any{"state": overallState}
	if totalChecks > 0 {
		overall["uptime_pct"] = metrics.Pct(int64(successChecks), int64(totalChecks))
		overall["p95_ms"] = metrics.SummarizeLatency(allLatencies).P95Ms
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"organization":   map[string]any{"name": org.Name, "slug": org.Slug},
		"generated_at":   time.Now().UTC(),
		"window":         metrics.WindowString(statusWindow),
		"overall":        overall,
		"services":       services,
		"open_incidents": publicIncidents(openIncidents),
	})
}

// publicIncident trims incident details to what a public page may show.
func publicIncidents(rows []models.IncidentDetail) []map[string]any {
	out := []map[string]any{}
	for _, inc := range rows {
		out = append(out, map[string]any{
			"id":            inc.ID,
			"endpoint_name": inc.EndpointName,
			"opened_at":     inc.OpenedAt,
			"status":        inc.Status,
		})
	}
	return out
}
