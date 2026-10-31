// SaaS tenant alert channels (webhooks with Pro/Business HMAC signing, email
// destinations) and tenant-scoped programmatic API keys (upx_...).
package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"uptimex/api/middleware"
	"uptimex/internal/checker"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

const (
	maxAlertChannelsPerOrg = 20
	maxAPIKeysPerOrg       = 25
)

// AlertChannelHandler manages per-tenant notification destinations under
// /api/v1/alert-channels*.
type AlertChannelHandler struct {
	Repo        storage.Repository
	Engine      *monitor.Engine
	Guard       *checker.Guard
	Logger      *slog.Logger
	SMTPEnabled bool
	PublicURL   string
}

type alertChannelPayload struct {
	Name    *string `json:"name"`
	Type    *string `json:"type"`
	Target  *string `json:"target"`
	Secret  *string `json:"secret"`
	Enabled *bool   `json:"enabled"`
}

func (h *AlertChannelHandler) tenantIdentity(w http.ResponseWriter, r *http.Request) (*middleware.Identity, bool) {
	id := middleware.IdentityFromContext(r.Context())
	if id == nil || !id.IsTenant() {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return nil, false
	}
	return id, true
}

func parsePathID(w http.ResponseWriter, r *http.Request, label string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "invalid "+label+" id")
		return 0, false
	}
	return id, true
}

func (h *AlertChannelHandler) validateChannelFields(w http.ResponseWriter, r *http.Request, orgID int64, ch *models.AlertChannel, secretTouched bool) bool {
	ch.Name = strings.TrimSpace(ch.Name)
	if len(ch.Name) < 1 || len(ch.Name) > 120 {
		writeError(w, http.StatusBadRequest, "channel name must be between 1 and 120 characters")
		return false
	}
	ch.Type = strings.ToLower(strings.TrimSpace(ch.Type))
	ch.Target = strings.TrimSpace(ch.Target)

	switch ch.Type {
	case models.AlertChannelWebhook:
		if len(ch.Target) < 8 || len(ch.Target) > 2048 {
			writeError(w, http.StatusBadRequest, "webhook target URL is required")
			return false
		}
		guard := h.Guard
		if guard == nil {
			guard = checker.NewGuard(false)
		}
		if err := guard.ValidateURL(ch.Target); err != nil {
			writeError(w, http.StatusBadRequest, "invalid webhook URL: "+err.Error())
			return false
		}
		if len(ch.Secret) > 256 {
			writeError(w, http.StatusBadRequest, "webhook secret must be at most 256 characters")
			return false
		}
		if secretTouched && ch.Secret != "" {
			org, err := h.Repo.GetOrganization(r.Context(), orgID)
			if err != nil {
				logInternalError(w, h.Logger, "alert channel: get org", err)
				return false
			}
			if org.Plan == models.PlanFree {
				writeError(w, http.StatusForbidden, "HMAC-signed webhooks require the Pro or Business plan")
				return false
			}
		}
	case models.AlertChannelEmail:
		if len(ch.Target) < 3 || len(ch.Target) > 320 {
			writeError(w, http.StatusBadRequest, "a valid email recipient is required")
			return false
		}
		addr, err := mail.ParseAddress(ch.Target)
		if err != nil || addr.Address == "" || strings.ContainsAny(addr.Address, "\r\n") {
			writeError(w, http.StatusBadRequest, "a valid email recipient is required")
			return false
		}
		ch.Target = addr.Address
		ch.Secret = ""
	default:
		writeError(w, http.StatusBadRequest, "channel type must be 'webhook' or 'email'")
		return false
	}

	ch.HasSecret = ch.Secret != ""
	return true
}

// List returns all alert channels configured for the caller's organization.
func (h *AlertChannelHandler) List(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tenantIdentity(w, r)
	if !ok {
		return
	}
	list, err := h.Repo.ListAlertChannelsByOrg(r.Context(), id.OrgID, false)
	if err != nil {
		logInternalError(w, h.Logger, "list alert channels", err)
		return
	}
	if list == nil {
		list = []models.AlertChannel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channels":        list,
		"count":           len(list),
		"smtp_configured": h.SMTPEnabled,
	})
}

// Create registers a new tenant alert channel.
func (h *AlertChannelHandler) Create(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tenantIdentity(w, r)
	if !ok {
		return
	}
	var p alertChannelPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	existing, err := h.Repo.ListAlertChannelsByOrg(r.Context(), id.OrgID, false)
	if err != nil {
		logInternalError(w, h.Logger, "count alert channels", err)
		return
	}
	if len(existing) >= maxAlertChannelsPerOrg {
		writeError(w, http.StatusConflict, "alert channel limit reached ("+strconv.Itoa(maxAlertChannelsPerOrg)+" per organization)")
		return
	}

	ch := &models.AlertChannel{
		OrgID:   id.OrgID,
		Enabled: true,
	}
	if p.Name != nil {
		ch.Name = *p.Name
	}
	if p.Type != nil {
		ch.Type = *p.Type
	}
	if p.Target != nil {
		ch.Target = *p.Target
	}
	if p.Secret != nil {
		ch.Secret = strings.TrimSpace(*p.Secret)
	}
	if p.Enabled != nil {
		ch.Enabled = *p.Enabled
	}
	if !h.validateChannelFields(w, r, id.OrgID, ch, ch.Secret != "") {
		return
	}
	if err := h.Repo.CreateAlertChannel(r.Context(), ch); err != nil {
		logInternalError(w, h.Logger, "create alert channel", err)
		return
	}
	ch.HasSecret = ch.Secret != ""
	h.Logger.Info("alert_channel_created", "org_id", id.OrgID, "channel_id", ch.ID, "type", ch.Type)
	writeJSON(w, http.StatusCreated, ch)
}

// Update modifies an existing tenant alert channel.
func (h *AlertChannelHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tenantIdentity(w, r)
	if !ok {
		return
	}
	chID, ok := parsePathID(w, r, "alert channel")
	if !ok {
		return
	}
	ch, err := h.Repo.GetAlertChannel(r.Context(), chID)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && ch.OrgID != id.OrgID) {
		writeError(w, http.StatusNotFound, "alert channel not found")
		return
	}
	if err != nil {
		logInternalError(w, h.Logger, "get alert channel", err)
		return
	}

	var p alertChannelPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.Name != nil {
		ch.Name = *p.Name
	}
	if p.Type != nil {
		ch.Type = *p.Type
	}
	if p.Target != nil {
		ch.Target = *p.Target
	}
	secretTouched := false
	if p.Secret != nil {
		ch.Secret = strings.TrimSpace(*p.Secret)
		secretTouched = true
	}
	if p.Enabled != nil {
		ch.Enabled = *p.Enabled
	}
	if !h.validateChannelFields(w, r, id.OrgID, ch, secretTouched) {
		return
	}
	if err := h.Repo.UpdateAlertChannel(r.Context(), ch); err != nil {
		logInternalError(w, h.Logger, "update alert channel", err)
		return
	}
	ch.HasSecret = ch.Secret != ""
	writeJSON(w, http.StatusOK, ch)
}

// Delete removes a tenant alert channel.
func (h *AlertChannelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tenantIdentity(w, r)
	if !ok {
		return
	}
	chID, ok := parsePathID(w, r, "alert channel")
	if !ok {
		return
	}
	if err := h.Repo.DeleteAlertChannel(r.Context(), id.OrgID, chID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "alert channel not found")
			return
		}
		logInternalError(w, h.Logger, "delete alert channel", err)
		return
	}
	h.Logger.Info("alert_channel_deleted", "org_id", id.OrgID, "channel_id", chID)
	w.WriteHeader(http.StatusNoContent)
}

// Test sends a live test notification to a single tenant alert channel.
func (h *AlertChannelHandler) Test(w http.ResponseWriter, r *http.Request) {
	id, ok := h.tenantIdentity(w, r)
	if !ok {
		return
	}
	chID, ok := parsePathID(w, r, "alert channel")
	if !ok {
		return
	}
	ch, err := h.Repo.GetAlertChannel(r.Context(), chID)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && ch.OrgID != id.OrgID) {
		writeError(w, http.StatusNotFound, "alert channel not found")
		return
	}
	if err != nil {
		logInternalError(w, h.Logger, "get alert channel", err)
		return
	}
	if ch.Type == models.AlertChannelEmail && !h.SMTPEnabled {
		writeError(w, http.StatusBadRequest, "SMTP is not configured on this UptimeX server")
		return
	}
	if ch.Type == models.AlertChannelWebhook {
		guard := h.Guard
		if guard == nil {
			guard = checker.NewGuard(false)
		}
		if err := guard.ValidateURL(ch.Target); err != nil {
			writeError(w, http.StatusBadRequest, "invalid webhook URL: "+err.Error())
			return
		}
	}
	if h.Engine == nil {
		writeError(w, http.StatusServiceUnavailable, "monitoring engine is not available")
		return
	}
	sender := h.Engine.ChannelFromModel(*ch)
	if sender == nil {
		writeError(w, http.StatusBadRequest, "channel cannot be initialized (check server SMTP settings)")
		return
	}

	dashURL := "/app"
	if h.PublicURL != "" {
		dashURL = strings.TrimRight(h.PublicURL, "/") + "/app"
	}
	ctx, cancel := contextWithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	testAlert := models.Alert{
		Type:         models.AlertTest,
		EndpointName: "Test Monitor",
		URL:          "https://example.com/health",
		DashboardURL: dashURL,
		DetectedAt:   time.Now().UTC(),
		Message:      "Test notification from UptimeX (" + ch.Name + ")",
	}
	if err := sender.Send(ctx, testAlert); err != nil {
		writeError(w, http.StatusBadGateway, "delivery failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

// APIKeyHandler manages per-organization programmatic API keys under
// /api/v1/api-keys* (session authentication required).
type APIKeyHandler struct {
	Repo   storage.Repository
	Logger *slog.Logger
}

type apiKeyCreatePayload struct {
	Name string `json:"name"`
}

func (h *APIKeyHandler) sessionIdentity(w http.ResponseWriter, r *http.Request) (*middleware.Identity, bool) {
	id := middleware.IdentityFromContext(r.Context())
	if id == nil || !id.Session {
		writeError(w, http.StatusUnauthorized, "sign in required")
		return nil, false
	}
	return id, true
}

// List returns active API keys for the signed-in user's organization (without raw tokens).
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	keys, err := h.Repo.ListAPIKeysByOrg(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "list api keys", err)
		return
	}
	if keys == nil {
		keys = []models.APIKey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": keys, "count": len(keys)})
}

// Create mints a new per-organization API key and returns the raw token once.
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	var p apiKeyCreatePayload
	if !decodeJSON(w, r, &p) {
		return
	}
	name := strings.TrimSpace(p.Name)
	if len(name) < 1 || len(name) > 100 {
		writeError(w, http.StatusBadRequest, "key name must be between 1 and 100 characters")
		return
	}
	existing, err := h.Repo.ListAPIKeysByOrg(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "count api keys", err)
		return
	}
	if len(existing) >= maxAPIKeysPerOrg {
		writeError(w, http.StatusConflict, "API key limit reached ("+strconv.Itoa(maxAPIKeysPerOrg)+" per organization)")
		return
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		logInternalError(w, h.Logger, "mint api key", err)
		return
	}
	rawToken := "upx_" + base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(rawToken))
	hashHex := hex.EncodeToString(sum[:])

	key := &models.APIKey{
		OrgID:     id.OrgID,
		Name:      name,
		KeyPrefix: rawToken[:12],
		TokenHash: hashHex,
	}
	if err := h.Repo.CreateAPIKey(r.Context(), key); err != nil {
		logInternalError(w, h.Logger, "create api key", err)
		return
	}
	h.Logger.Info("api_key_created", "org_id", id.OrgID, "key_id", key.ID, "prefix", key.KeyPrefix)
	writeJSON(w, http.StatusCreated, map[string]any{
		"api_key": key,
		"token":   rawToken,
	})
}

// Delete revokes an API key belonging to the caller's organization.
func (h *APIKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := h.sessionIdentity(w, r)
	if !ok {
		return
	}
	keyID, ok := parsePathID(w, r, "api key")
	if !ok {
		return
	}
	if err := h.Repo.DeleteAPIKey(r.Context(), id.OrgID, keyID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "api key not found")
			return
		}
		logInternalError(w, h.Logger, "delete api key", err)
		return
	}
	h.Logger.Info("api_key_deleted", "org_id", id.OrgID, "key_id", keyID)
	w.WriteHeader(http.StatusNoContent)
}
