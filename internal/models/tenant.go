// Multi-tenant SaaS entities: organizations (tenants), their users and login
// sessions. Endpoints belong to an organization via a nullable org_id — NULL
// marks pre-SaaS "global" endpoints owned by the operator (legacy mode).
package models

import "time"

// Plan identifiers. The authoritative catalog (limits, pricing) lives in
// internal/plans; these constants are the stable wire values.
const (
	PlanFree     = "free"
	PlanPro      = "pro"
	PlanBusiness = "business"
)

// User roles.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

// Organization is one SaaS tenant. The slug doubles as the public status
// page path (/status/{slug}).
type Organization struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	Plan              string    `json:"plan"`
	StatusPageEnabled bool      `json:"status_page_enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// User is an account belonging to exactly one organization.
type User struct {
	ID           int64     `json:"id"`
	OrgID        int64     `json:"org_id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session is a persisted login. The browser carries the raw token in a
// cookie; only its SHA-256 hash is stored.
type Session struct {
	TokenHash string    `json:"-"`
	UserID    int64     `json:"user_id"`
	OrgID     int64     `json:"org_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
