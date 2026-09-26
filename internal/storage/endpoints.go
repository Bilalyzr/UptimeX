package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"uptimex/internal/models"
)

const endpointCols = `id, name, url, method, interval_seconds, timeout_ms, failure_threshold,
	expected_status_min, expected_status_max, enabled, created_at, updated_at`

func scanEndpoint(row interface{ Scan(...any) error }) (*models.Endpoint, error) {
	var (
		e                models.Endpoint
		created, updated nullTime
		enabled          nullBool
	)
	err := row.Scan(&e.ID, &e.Name, &e.URL, &e.Method, &e.IntervalSeconds, &e.TimeoutMs,
		&e.FailureThreshold, &e.ExpectedStatusMin, &e.ExpectedStatusMax, &enabled, &created, &updated)
	if err != nil {
		return nil, err
	}
	e.Enabled = enabled.Bool
	e.CreatedAt = created.Time.UTC()
	e.UpdatedAt = updated.Time.UTC()
	return &e, nil
}

// CreateEndpoint inserts a new endpoint and seeds its status row in one
// transaction so every endpoint always has status state.
func (s *SQLStore) CreateEndpoint(ctx context.Context, e *models.Endpoint) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, s.q(`
		INSERT INTO endpoints (name, url, method, interval_seconds, timeout_ms, failure_threshold,
			expected_status_min, expected_status_max, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?) RETURNING id`),
		e.Name, e.URL, e.Method, e.IntervalSeconds, e.TimeoutMs, e.FailureThreshold,
		e.ExpectedStatusMin, e.ExpectedStatusMax, e.Enabled, now, now).Scan(&e.ID)
	if err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, s.q(`
		INSERT INTO endpoint_status (endpoint_id, state, consecutive_failures, updated_at)
		VALUES (?, 'HEALTHY', 0, ?)`), e.ID, now); err != nil {
		return err
	}

	e.CreatedAt, e.UpdatedAt = now, now
	return tx.Commit()
}

func (s *SQLStore) GetEndpoint(ctx context.Context, id int64) (*models.Endpoint, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+endpointCols+` FROM endpoints WHERE id = ?`), id)
	e, err := scanEndpoint(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

func (s *SQLStore) ListEndpoints(ctx context.Context, onlyEnabled bool) ([]models.Endpoint, error) {
	q := `SELECT ` + endpointCols + ` FROM endpoints`
	var args []any
	if onlyEnabled {
		q += ` WHERE enabled = ?`
		args = append(args, true)
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Endpoint{}
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLStore) UpdateEndpoint(ctx context.Context, e *models.Endpoint) error {
	res, err := s.db.ExecContext(ctx, s.q(`
		UPDATE endpoints SET name=?, url=?, method=?, interval_seconds=?, timeout_ms=?,
			failure_threshold=?, expected_status_min=?, expected_status_max=?, enabled=?, updated_at=?
		WHERE id=?`),
		e.Name, e.URL, e.Method, e.IntervalSeconds, e.TimeoutMs, e.FailureThreshold,
		e.ExpectedStatusMin, e.ExpectedStatusMax, e.Enabled, time.Now().UTC(), e.ID)
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

func (s *SQLStore) DeleteEndpoint(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM endpoints WHERE id=?`), id)
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

func (s *SQLStore) CountEndpoints(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM endpoints`).Scan(&n)
	return n, err
}
