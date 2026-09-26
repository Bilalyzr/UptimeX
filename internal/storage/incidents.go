package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"uptimex/internal/models"
)

const incidentCols = `id, endpoint_id, opened_at, resolved_at, status, failure_count, last_error`

func scanIncident(row interface{ Scan(...any) error }) (*models.Incident, error) {
	var (
		i        models.Incident
		resolved nullTime
	)
	err := row.Scan(&i.ID, &i.EndpointID, &i.OpenedAt, &resolved, &i.Status, &i.FailureCount, &i.LastError)
	if err != nil {
		return nil, err
	}
	i.ResolvedAt = resolved.Ptr()
	i.OpenedAt = i.OpenedAt.UTC()
	return &i, nil
}

// OpenIncident creates a new OPEN incident. If one already exists for the
// endpoint, the database's partial unique index rejects the insert and
// ErrDuplicateOpenIncident is returned — one outage corresponds to exactly
// one OPEN incident even under races.
func (s *SQLStore) OpenIncident(ctx context.Context, endpointID int64, at time.Time, failureCount int, lastError string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO incidents (endpoint_id, opened_at, resolved_at, status, failure_count, last_error)
		VALUES (?,?,NULL,'OPEN',?,?) RETURNING id`),
		endpointID, at.UTC(), failureCount, lastError).Scan(&id)
	if s.isUniqueViolation(err) {
		return 0, ErrDuplicateOpenIncident
	}
	return id, err
}

// ResolveIncident marks an incident RESOLVED with its resolution timestamp.
func (s *SQLStore) ResolveIncident(ctx context.Context, incidentID int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx, s.q(`
		UPDATE incidents SET status='RESOLVED', resolved_at=? WHERE id=? AND status='OPEN'`),
		at.UTC(), incidentID)
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

// UpdateIncident refreshes the failure count and last error of an open
// incident so its record reflects the latest observation.
func (s *SQLStore) UpdateIncident(ctx context.Context, incidentID int64, failureCount int, lastError string) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		UPDATE incidents SET failure_count=?, last_error=? WHERE id=?`),
		failureCount, lastError, incidentID)
	return err
}

func (s *SQLStore) GetOpenIncident(ctx context.Context, endpointID int64) (*models.Incident, error) {
	row := s.db.QueryRowContext(ctx, s.q(`
		SELECT `+incidentCols+` FROM incidents WHERE endpoint_id=? AND status='OPEN'`), endpointID)
	i, err := scanIncident(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return i, err
}

// ListIncidents returns incidents newest-first, optionally filtered by
// status ('OPEN' or 'RESOLVED'; empty means all).
func (s *SQLStore) ListIncidents(ctx context.Context, status string, limit int) ([]models.IncidentDetail, error) {
	q := `SELECT i.id, i.endpoint_id, i.opened_at, i.resolved_at, i.status, i.failure_count, i.last_error,
	             e.name, e.url
	      FROM incidents i JOIN endpoints e ON e.id = i.endpoint_id`
	var args []any
	if status != "" {
		q += ` WHERE i.status = ?`
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
