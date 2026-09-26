// Organization management and billing: tenant profile, plan catalog with
// live usage, and plan changes. Plan switching is immediate (demo billing);
// a production deployment plugs a Stripe checkout session in front of
// ChangePlan and calls it only from the webhook.
package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"uptimex/api/middleware"
	"uptimex/internal/plans"
	"uptimex/internal/storage"
)

// OrgHandler serves /api/v1/org*.
type OrgHandler struct {
	Repo   storage.Repository
	Logger *slog.Logger
}

type orgPatchPayload struct {
	Name              *string `json:"name"`
	StatusPageEnabled *bool   `json:"status_page_enabled"`
}

type planChangePayload struct {
	PlanID string `json:"plan_id"`
}

func (h *OrgHandler) sessionIdentity(w http.ResponseWriter, r *http.Request) (*middleware.Identity, bool) {
	id := middleware.IdentityFromContext(r.Context())
	if id == nil || !id.Session {
		writeError(w, http.StatusUnauthorized, "sign in to manage this organization")
		return nil, false
	}
	return id, true
}

// Get returns the organization, plan usage and the full plan catalog.
func (h *OrgHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	org, err := h.Repo.GetOrganization(r.Context(), id.OrgID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		logInternalError(w, h.Logger, "org get", err)
		return
	}
	used, err := h.Repo.CountEndpointsInOrg(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "org usage", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"org":    org,
		"usage":  map[string]any{"endpoints": used},
		"plans":  plans.All(),
		"billing": map[string]any{"mode": "demo", "provider": "stripe-ready"},
	})
}

// Update changes mutable organization settings.
func (h *OrgHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	org, err := h.Repo.GetOrganization(r.Context(), id.OrgID)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	if err != nil {
		logInternalError(w, h.Logger, "org get", err)
		return
	}
	var p orgPatchPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.Name != nil {
		if len(*p.Name) < 2 || len(*p.Name) > 100 {
			writeError(w, http.StatusBadRequest, "organization name must be between 2 and 100 characters")
			return
		}
		org.Name = *p.Name
	}
	if p.StatusPageEnabled != nil {
		org.StatusPageEnabled = *p.StatusPageEnabled
	}
	if err := h.Repo.UpdateOrganization(r.Context(), org); err != nil {
		logInternalError(w, h.Logger, "org update", err)
		return
	}
	writeJSON(w, http.StatusOK, org)
}

// ChangePlan switches the organization's plan. Demo billing: the switch is
// immediate. With Stripe configured, this endpoint is called by the
// checkout.session.completed webhook instead of by the browser.
func (h *OrgHandler) ChangePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	var p planChangePayload
	if !decodeJSON(w, r, &p) {
		return
	}
	plan, found := plans.ByID(p.PlanID)
	if !found {
		writeError(w, http.StatusBadRequest, "unknown plan; available: free, pro, business")
		return
	}
	// Downgrades must fit inside the target plan's quotas.
	if used, err := h.Repo.CountEndpointsInOrg(r.Context(), id.OrgID); err == nil && used > int64(plan.MaxEndpoints) {
		writeError(w, http.StatusConflict, "cannot downgrade: delete endpoints first (current "+
			strconv.FormatInt(used, 10)+" exceeds "+plan.Name+" limit of "+
			strconv.Itoa(plan.MaxEndpoints)+")")
		return
	}
	if err := h.Repo.UpdateOrganizationPlan(r.Context(), id.OrgID, plan.ID); err != nil {
		logInternalError(w, h.Logger, "plan change", err)
		return
	}
	org, err := h.Repo.GetOrganization(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "org get", err)
		return
	}
	h.Logger.Info("plan_changed", "org_id", id.OrgID, "plan", plan.ID)
	writeJSON(w, http.StatusOK, map[string]any{"org": org, "plan": plan})
}
