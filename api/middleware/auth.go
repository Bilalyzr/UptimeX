// Request identity resolution for the SaaS layer: session cookies (tenant
// users), per-organization API keys (upx_...), and the legacy global API key
// (operator). The middleware only resolves; enforcement (401 on anonymous
// traffic in SaaS mode) is a separate wrapper so legacy deployments keep their
// exact previous behavior.
package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"uptimex/internal/models"
)

// SessionCookieName is the browser cookie carrying the raw session token.
const SessionCookieName = "uptimex_session"

// OrgAPIKeyPrefix distinguishes tenant-scoped API keys from operator keys.
const OrgAPIKeyPrefix = "upx_"

// Identity is the resolved caller of an API request.
type Identity struct {
	UserID   int64
	OrgID    int64 // tenant id; 0 = global (legacy) scope
	APIKeyID int64 // non-zero when authenticated via a tenant API key
	Email    string
	Plan     string
	Role     string
	// Session is true for a logged-in tenant user (browser cookie).
	Session bool
	// OrgKey is true for a valid per-organization programmatic API key.
	OrgKey bool
	// Legacy is true for the operator scope: valid global API key, or the
	// open development mode (no key configured). Legacy identities see all
	// endpoints, including NULL-org ones.
	Legacy bool
	// keyValid distinguishes "valid API key" from "open dev mode" in Legacy.
	keyValid bool
}

// Authenticated reports whether the caller presented valid credentials.
func (i *Identity) Authenticated() bool {
	return i != nil && (i.Session || i.OrgKey || (i.Legacy && i.keyValid))
}

// IsTenant reports whether the caller is scoped to a specific SaaS organization
// (either via a browser session or a per-org API key).
func (i *Identity) IsTenant() bool {
	return i != nil && (i.Session || i.OrgKey) && i.OrgID > 0
}

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

// orgAPIKeyRepo is an optional extension implemented by SQLStore to resolve
// tenant-scoped upx_... API keys without forcing tiny test mocks to implement it.
type orgAPIKeyRepo interface {
	GetAPIKeyByHash(ctx context.Context, tokenHash string) (*models.APIKey, error)
	TouchAPIKeyLastUsed(ctx context.Context, id int64, at time.Time) error
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

func (m *ResolveIdentity) resolve(_ http.ResponseWriter, r *http.Request) *Identity {
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

	// 2) Header / Bearer credential → either global operator key or tenant upx_ key.
	provided := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if provided == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			provided = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	if m.APIKey != "" && provided != "" &&
		subtle.ConstantTimeCompare([]byte(provided), []byte(m.APIKey)) == 1 {
		return &Identity{Legacy: true, keyValid: true}
	}
	if strings.HasPrefix(provided, OrgAPIKeyPrefix) && m.Repo != nil {
		if kr, ok := m.Repo.(orgAPIKeyRepo); ok {
			sum := sha256.Sum256([]byte(provided))
			if key, err := kr.GetAPIKeyByHash(r.Context(), hex.EncodeToString(sum[:])); err == nil {
				if org, err := m.Repo.GetOrganization(r.Context(), key.OrgID); err == nil {
					_ = kr.TouchAPIKeyLastUsed(r.Context(), key.ID, time.Now().UTC())
					return &Identity{
						OrgID:    org.ID,
						APIKeyID: key.ID,
						Plan:     org.Plan,
						Role:     models.RoleOwner,
						OrgKey:   true,
					}
				}
			}
		}
		// An invalid upx_ key must never fall through to anonymous dev access.
		return nil
	}

	// 3) Anonymous. In open dev mode (no key configured) callers keep the
	// legacy global scope, matching pre-SaaS behavior; otherwise anonymous.
	if m.APIKey == "" {
		return &Identity{Legacy: true}
	}
	return nil
}

// Gate enforces authentication after ResolveIdentity has run. Accepted:
// a tenant session, a per-org API key, the operator API key, or — only outside
// SaaS mode — the open development mode (no key configured).
type Gate struct {
	APIKey   string
	SAASMode bool
	Next     http.Handler
}

func (m *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id != nil && (id.Session || id.OrgKey) {
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
	msg := `{"error":"authentication required"}`
	if !m.SAASMode && m.APIKey != "" {
		msg = `{"error":"missing or invalid API key"}`
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(msg))
}

// RequireSession admits only logged-in tenant users (never the operator
// key or an API key): used by /auth/me, password/session management, and
// key/channel administration routes.
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
