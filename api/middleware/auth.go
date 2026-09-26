// Request identity resolution for the SaaS layer: session cookies (tenant
// users) and the legacy global API key (operator). The middleware only
// resolves; enforcement (401 on anonymous traffic in SaaS mode) is a separate
// wrapper so legacy deployments keep their exact previous behavior.
package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"uptimex/internal/models"
)

// SessionCookieName is the browser cookie carrying the raw session token.
const SessionCookieName = "uptimex_session"

// Identity is the resolved caller of an API request.
type Identity struct {
	UserID int64
	OrgID  int64 // tenant id; 0 = global (legacy) scope
	Email  string
	Plan   string
	Role   string
	// Session is true for a logged-in tenant user.
	Session bool
	// Legacy is true for the operator scope: valid global API key, or the
	// open development mode (no key configured). Legacy identities see all
	// endpoints, including NULL-org ones.
	Legacy bool
	// keyValid distinguishes "valid API key" from "open dev mode" in Legacy.
	keyValid bool
}

// Authenticated reports whether the caller presented valid credentials.
func (i *Identity) Authenticated() bool { return i.Session || (i.Legacy && i.keyValid) }

// ScopeOrgID returns the tenant scope, or nil for the global legacy scope.
func (i *Identity) ScopeOrgID() *int64 {
	if i == nil || i.Legacy || i.OrgID == 0 {
		return nil
	}
	id := i.OrgID
	return &id
}

type identityCtxKey struct{}

// WithIdentity attaches the resolved identity to the request context.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// IdentityFromContext returns the resolved identity, or nil for anonymous.
func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityCtxKey{}).(*Identity)
	return id
}

// SessionRepo is the storage surface identity resolution needs.
type SessionRepo interface {
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*models.Session, error)
	GetUserByID(ctx context.Context, id int64) (*models.User, error)
	GetOrganization(ctx context.Context, id int64) (*models.Organization, error)
}

// ResolveIdentity resolves session/API-key identities and stores them in the
// request context. It never rejects a request.
type ResolveIdentity struct {
	Repo   SessionRepo
	APIKey string // legacy global operator key ("" = open dev mode)
	Next   http.Handler
}

func (m *ResolveIdentity) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := m.resolve(w, r)
	m.Next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
}

func (m *ResolveIdentity) resolve(w http.ResponseWriter, r *http.Request) *Identity {
	// 1) Session cookie → tenant identity.
	if m.Repo != nil {
		if c, err := r.Cookie(SessionCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
			sum := sha256.Sum256([]byte(c.Value))
			sess, err := m.Repo.GetSessionByTokenHash(r.Context(), hex.EncodeToString(sum[:]))
			if err == nil {
				if user, err := m.Repo.GetUserByID(r.Context(), sess.UserID); err == nil {
					plan := models.PlanFree
					if org, err := m.Repo.GetOrganization(r.Context(), sess.OrgID); err == nil {
						plan = org.Plan
					}
					return &Identity{
						UserID: user.ID, OrgID: sess.OrgID, Email: user.Email,
						Plan: plan, Role: user.Role, Session: true,
					}
				}
			}
		}
	}

	// 2) Global API key → legacy operator identity.
	provided := r.Header.Get("X-API-Key")
	if provided == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			provided = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if m.APIKey != "" && provided != "" &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(m.APIKey)) == 1 {
		return &Identity{Legacy: true, keyValid: true}
	}

	// 3) Anonymous. In open dev mode (no key configured) callers keep the
	// legacy global scope, matching pre-SaaS behavior; otherwise anonymous.
	if m.APIKey == "" {
		return &Identity{Legacy: true}
	}
	return nil
}

// Gate enforces authentication after ResolveIdentity has run. Accepted:
// a tenant session, the operator API key, or — only outside SaaS mode — the
// open development mode (no key configured).
type Gate struct {
	APIKey   string
	SAASMode bool
	Next     http.Handler
}

func (m *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id != nil && id.Session {
		m.Next.ServeHTTP(w, r)
		return
	}
	if m.APIKey == "" && !m.SAASMode {
		m.Next.ServeHTTP(w, r)
		return
	}
	if id != nil && id.Legacy && id.keyValid {
		m.Next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"authentication required"}`))
}

// RequireSession admits only logged-in tenant users (never the operator
// key): used by /auth/me and /org* account routes.
type RequireSession struct {
	Next http.Handler
}

func (m *RequireSession) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if id := IdentityFromContext(r.Context()); id != nil && id.Session {
		m.Next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"sign in required"}`))
}
