// SaaS tenancy storage: organizations, users, login sessions and org-scoped
// analytics queries. Org-scoped queries JOIN endpoints so NULL-org legacy
// rows are never visible to a tenant.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"uptimex/internal/models"
)

const orgCols = `id, name, slug, plan, status_page_enabled, created_at, updated_at`

func scanOrganization(row interface{ Scan(...any) error }) (*models.Organization, error) {
	var (
		o              models.Organization
		statusPage     nullBool
		createdUpdated [2]nullTime
	)
	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Plan, &statusPage, &createdUpdated[0], &createdUpdated[1])
	if err != nil {
		return nil, err
	}
	o.StatusPageEnabled = statusPage.Bool
	o.CreatedAt = createdUpdated[0].Time.UTC()
	o.UpdatedAt = createdUpdated[1].Time.UTC()
	return &o, nil
}

// CreateOrganization inserts a new tenant and returns its id.
func (s *SQLStore) CreateOrganization(ctx context.Context, o *models.Organization) error {
	now := time.Now().UTC()
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO organizations (name, slug, plan, status_page_enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?) RETURNING id`),
		o.Name, o.Slug, o.Plan, o.StatusPageEnabled, now, now).Scan(&o.ID)
	if err != nil {
		return err
	}
	o.CreatedAt, o.UpdatedAt = now, now
	return nil
}

func (s *SQLStore) GetOrganization(ctx context.Context, id int64) (*models.Organization, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+orgCols+` FROM organizations WHERE id = ?`), id)
	o, err := scanOrganization(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

func (s *SQLStore) GetOrganizationBySlug(ctx context.Context, slug string) (*models.Organization, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+orgCols+` FROM organizations WHERE slug = ?`), slug)
	o, err := scanOrganization(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return o, err
}

// UpdateOrganization persists mutable tenant settings (name, status page).
func (s *SQLStore) UpdateOrganization(ctx context.Context, o *models.Organization) error {
	res, err := s.db.ExecContext(ctx, s.q(`
		UPDATE organizations SET name=?, slug=?, plan=?, status_page_enabled=?, updated_at=? WHERE id=?`),
		o.Name, o.Slug, o.Plan, o.StatusPageEnabled, time.Now().UTC(), o.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateOrganizationPlan changes only the plan column.
func (s *SQLStore) UpdateOrganizationPlan(ctx context.Context, id int64, plan string) error {
	res, err := s.db.ExecContext(ctx, s.q(`UPDATE organizations SET plan=?, updated_at=? WHERE id=?`),
		plan, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- users -----------------------------------------------------------------

const userCols = `id, org_id, email, password_hash, name, role, created_at`

func scanUser(row interface{ Scan(...any) error }) (*models.User, error) {
	var (
		u       models.User
		created nullTime
	)
	err := row.Scan(&u.ID, &u.OrgID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &created)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = created.Time.UTC()
	return &u, nil
}

// CreateUser inserts an account; a duplicate email maps to ErrDuplicateEmail.
func (s *SQLStore) CreateUser(ctx context.Context, u *models.User) error {
	now := time.Now().UTC()
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO users (org_id, email, password_hash, name, role, created_at)
		VALUES (?,?,?,?,?,?) RETURNING id`),
		u.OrgID, u.Email, u.PasswordHash, u.Name, u.Role, now).Scan(&u.ID)
	if s.isUniqueViolation(err) {
		return ErrDuplicateEmail
	}
	if err == nil {
		u.CreatedAt = now
	}
	return err
}

func (s *SQLStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+userCols+` FROM users WHERE email = ?`), email)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *SQLStore) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+userCols+` FROM users WHERE id = ?`), id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// --- sessions --------------------------------------------------------------

// CreateSession persists a login session keyed by the token hash.
func (s *SQLStore) CreateSession(ctx context.Context, sess *models.Session) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO sessions (token_hash, user_id, org_id, created_at, expires_at)
		VALUES (?,?,?,?,?)`),
		sess.TokenHash, sess.UserID, sess.OrgID, sess.CreatedAt.UTC(), sess.ExpiresAt.UTC())
	return err
}

// GetSessionByTokenHash returns the live session for a token hash, or
// ErrNotFound when unknown or expired.
func (s *SQLStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*models.Session, error) {
	row := s.db.QueryRowContext(ctx, s.q(`
		SELECT token_hash, user_id, org_id, created_at, expires_at
		FROM sessions WHERE token_hash = ? AND expires_at > ?`), tokenHash, time.Now().UTC())
	var (
		sess    models.Session
		created nullTime
		expires nullTime
	)
	err := row.Scan(&sess.TokenHash, &sess.UserID, &sess.OrgID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = created.Time.UTC()
	sess.ExpiresAt = expires.Time.UTC()
	return &sess, nil
}

func (s *SQLStore) DeleteSessionByTokenHash(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM sessions WHERE token_hash = ?`), tokenHash)
	return err
}

// DeleteExpiredSessions prunes stale logins; called opportunistically.
func (s *SQLStore) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM sessions WHERE expires_at <= ?`), time.Now().UTC())
	return err
}

// --- org-scoped analytics ----------------------------------------------------

// CountEndpointsInOrg counts a tenant's endpoints (quota checks).
func (s *SQLStore) CountEndpointsInOrg(ctx context.Context, orgID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM endpoints WHERE org_id = ?`, orgID).Scan(&n)
	return n, err
}

// ListIncidentsInOrg lists a tenant's incidents newest-first, optionally by status.
func (s *SQLStore) ListIncidentsInOrg(ctx context.Context, orgID int64, status string, limit int) ([]models.IncidentDetail, error) {
	q := `SELECT i.id, i.endpoint_id, i.opened_at, i.resolved_at, i.status, i.failure_count, i.last_error,
	             e.name, e.url
	      FROM incidents i JOIN endpoints e ON e.id = i.endpoint_id
	      WHERE e.org_id = ?`
	var args []any
	args = append(args, orgID)
	if status != "" {
		q += ` AND i.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY i.opened_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.IncidentDetail{}
	for rows.Next() {
		var (
			d        models.IncidentDetail
			resolved nullTime
		)
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.OpenedAt, &resolved, &d.Status,
			&d.FailureCount, &d.LastError, &d.EndpointName, &d.EndpointURL); err != nil {
			return nil, err
		}
		d.ResolvedAt = resolved.Ptr()
		d.OpenedAt = d.OpenedAt.UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// CountOutcomesInOrg returns (total, successful) checks for a tenant's
// endpoints within the window.
func (s *SQLStore) CountOutcomesInOrg(ctx context.Context, orgID int64, since time.Time) (int64, int64, error) {
	var total, success int64
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN hc.success THEN 1 ELSE 0 END), 0)
		FROM health_checks hc JOIN endpoints e ON e.id = hc.endpoint_id
		WHERE e.org_id = ? AND hc.checked_at >= ?`),
		orgID, since.UTC()).Scan(&total, &success)
	return total, success, err
}

// StatusCountsInOrg groups a tenant's checks by status code and error type.
func (s *SQLStore) StatusCountsInOrg(ctx context.Context, orgID int64, since time.Time) ([]StatusCount, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT COALESCE(hc.status_code, 0), COALESCE(hc.error_type, ''), COUNT(*)
		FROM health_checks hc JOIN endpoints e ON e.id = hc.endpoint_id
		WHERE e.org_id = ? AND hc.checked_at >= ?
		GROUP BY hc.status_code, hc.error_type`),
		orgID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StatusCount{}
	for rows.Next() {
		var sc StatusCount
		if err := rows.Scan(&sc.StatusCode, &sc.ErrorType, &sc.Count); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// SelectSamplesInOrg returns per-endpoint raw observations for one tenant,
// newest-first bounded per endpoint (mirror of SelectAllSamples, org-scoped).
func (s *SQLStore) SelectSamplesInOrg(ctx context.Context, orgID int64, since time.Time, perEndpointLimit int) (map[int64][]LatencySample, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT endpoint_id, checked_at, response_time_ms, success FROM (
			SELECT hc.endpoint_id, hc.checked_at, hc.response_time_ms, hc.success,
			       ROW_NUMBER() OVER (PARTITION BY hc.endpoint_id ORDER BY hc.checked_at DESC) AS rn
			FROM health_checks hc JOIN endpoints e ON e.id = hc.endpoint_id
			WHERE e.org_id = ? AND hc.checked_at >= ?
		) t WHERE rn <= ?
		ORDER BY endpoint_id, checked_at ASC`), orgID, since.UTC(), perEndpointLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]LatencySample{}
	for rows.Next() {
		var (
			sample    LatencySample
			success   nullBool
			checkedAt nullTime
		)
		if err := rows.Scan(&sample.EndpointID, &checkedAt, &sample.ResponseTimeMs, &success); err != nil {
			return nil, err
		}
		sample.CheckedAt = checkedAt.Time.UTC()
		sample.Success = success.Bool
		out[sample.EndpointID] = append(out[sample.EndpointID], sample)
	}
	return out, rows.Err()
}
