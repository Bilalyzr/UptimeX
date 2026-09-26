// SaaS authentication: signup (creates organization + owner user + session),
// login, logout and the current-identity endpoint. Passwords are hashed with
// PBKDF2-HMAC-SHA256 (stdlib crypto/pbkdf2); sessions are random 256-bit
// tokens delivered as HttpOnly cookies and stored only as SHA-256 hashes.
package handlers

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"uptimex/api/middleware"
	"uptimex/internal/models"
	"uptimex/internal/plans"
	"uptimex/internal/storage"
)

// PBKDF2 parameters (OWASP 2023 guidance for PBKDF2-HMAC-SHA256).
const (
	pbkdf2Iterations = 600_000
	pbkdf2KeyLen     = 32
	pbkdf2SaltLen    = 16
)

// AuthHandler serves the /api/v1/auth/* surface.
type AuthHandler struct {
	Repo          storage.Repository
	Logger        *slog.Logger
	SessionTTL    time.Duration
	SecureCookies bool
}

type signupPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	OrgName  string `json:"org_name"`
}

type loginPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// hashPassword derives the stored PBKDF2 digest in PHC-like form.
func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword checks a password against a stored digest in constant time.
func verifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// newSessionToken returns (rawToken, sha256Hex). Only the hash is persisted.
func newSessionToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

// slugify builds a URL-safe slug from an organization name.
func slugify(name string) string {
	var b strings.Builder
	prevDash := true // avoids leading dash
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueSlug appends a numeric suffix until the slug is free.
func (h *AuthHandler) uniqueSlug(r *http.Request, base string) string {
	if base == "" {
		base = "org"
	}
	for n := 0; ; n++ {
		candidate := base
		if n > 0 {
			candidate = base + "-" + strconv.Itoa(n+1)
		}
		if _, err := h.Repo.GetOrganizationBySlug(r.Context(), candidate); errors.Is(err, storage.ErrNotFound) {
			return candidate
		} else if err != nil {
			return base + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
		}
		if n > 500 {
			return base + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
		}
	}
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.SessionTTL.Seconds()),
	})
}

func (h *AuthHandler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: middleware.SessionCookieName, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// issueSession creates a session for the user and sets the cookie.
func (h *AuthHandler) issueSession(w http.ResponseWriter, r *http.Request, u *models.User) (*models.Session, error) {
	token, hash, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sess := &models.Session{TokenHash: hash, UserID: u.ID, OrgID: u.OrgID, CreatedAt: now, ExpiresAt: now.Add(h.SessionTTL)}
	if err := h.Repo.CreateSession(r.Context(), sess); err != nil {
		return nil, err
	}
	h.setSessionCookie(w, token)
	return sess, nil
}

// Signup creates a new organization with its owner account and logs in.
func (h *AuthHandler) Signup(w http.ResponseWriter, r *http.Request) {
	var p signupPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.OrgName = strings.TrimSpace(p.OrgName)
	p.Name = strings.TrimSpace(p.Name)
	if p.Email == "" || !strings.Contains(p.Email, "@") || len(p.Email) > 255 {
		writeError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	if len(p.Password) < 8 || len(p.Password) > 200 {
		writeError(w, http.StatusBadRequest, "password must be between 8 and 200 characters")
		return
	}
	if len(p.OrgName) < 2 || len(p.OrgName) > 100 {
		writeError(w, http.StatusBadRequest, "organization name must be between 2 and 100 characters")
		return
	}
	if len(p.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name must be at most 100 characters")
		return
	}

	hash, err := hashPassword(p.Password)
	if err != nil {
		h.Logger.Error("hash_password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	org := &models.Organization{
		Name: p.OrgName, Slug: h.uniqueSlug(r, slugify(p.OrgName)),
		Plan: plans.Default().ID, StatusPageEnabled: true,
	}
	if err := h.Repo.CreateOrganization(r.Context(), org); err != nil {
		h.Logger.Error("create_organization", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user := &models.User{OrgID: org.ID, Email: p.Email, PasswordHash: hash, Name: p.Name, Role: models.RoleOwner}
	if err := h.Repo.CreateUser(r.Context(), user); err != nil {
		if errors.Is(err, storage.ErrDuplicateEmail) {
			writeError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		h.Logger.Error("create_user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := h.issueSession(w, r, user); err != nil {
		h.Logger.Error("create_session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = h.Repo.DeleteExpiredSessions(r.Context()) // opportunistic prune

	h.Logger.Info("signup", "org_id", org.ID, "user_id", user.ID, "email", user.Email)
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "org": org})
}

// Login authenticates an existing account.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var p loginPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	if p.Email == "" || p.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	user, err := h.Repo.GetUserByEmail(r.Context(), p.Email)
	if errors.Is(err, storage.ErrNotFound) || (err == nil && !verifyPassword(p.Password, user.PasswordHash)) {
		// Same error for unknown email and wrong password.
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		h.Logger.Error("get_user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if _, err := h.issueSession(w, r, user); err != nil {
		h.Logger.Error("create_session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	org, err := h.Repo.GetOrganization(r.Context(), user.OrgID)
	if err != nil {
		h.Logger.Error("get_org", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Logger.Info("login", "user_id", user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "org": org})
}

// Logout invalidates the current session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(middleware.SessionCookieName); err == nil && c.Value != "" {
		sum := sha256.Sum256([]byte(c.Value))
		if err := h.Repo.DeleteSessionByTokenHash(r.Context(), hex.EncodeToString(sum[:])); err != nil {
			h.Logger.Warn("delete_session", "error", err)
		}
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the signed-in user, organization and plan usage.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id == nil || !id.Session {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	user, err := h.Repo.GetUserByID(r.Context(), id.UserID)
	if err != nil {
		logInternalError(w, h.Logger, "me: get user", err)
		return
	}
	org, err := h.Repo.GetOrganization(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "me: get org", err)
		return
	}
	used, err := h.Repo.CountEndpointsInOrg(r.Context(), id.OrgID)
	if err != nil {
		logInternalError(w, h.Logger, "me: count endpoints", err)
		return
	}
	plan, _ := plans.ByID(org.Plan)
	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"org":  org,
		"usage": map[string]any{
			"endpoints":            used,
			"max_endpoints":        plan.MaxEndpoints,
			"min_interval_seconds": plan.MinIntervalSeconds,
		},
	})
}

// decodeJSON is the auth-body variant of the endpoint payload decoder.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}
