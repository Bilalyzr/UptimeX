package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"uptimex/internal/models"
)

// StatusCount is one bucket of the HTTP status-code distribution: rows with
// no HTTP response (network/timeout errors) report StatusCode 0 and the
// error type.
type StatusCount struct {
	StatusCode int64
	ErrorType  string
	Count      int64
}

// InsertCheck persists one probe result and returns the row id.
func (s *SQLStore) InsertCheck(ctx context.Context, r models.CheckResult) (int64, error) {
	var statusCode, errMsg any
	if r.StatusCode != nil {
		statusCode = *r.StatusCode
	}
	if r.ErrorMessage != "" {
		errMsg = r.ErrorMessage
	}
	var errType any
	if r.ErrorType != "" {
		errType = r.ErrorType
	}

	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO health_checks (endpoint_id, checked_at, status_code, response_time_ms, success, error_type, error_message, created_at)
		VALUES (?,?,?,?,?,?,?,?) RETURNING id`),
		r.EndpointID, r.CheckedAt.UTC(), statusCode, r.ResponseTimeMs, r.Success, errType, errMsg, time.Now().UTC(),
	).Scan(&id)
	return id, err
}

func scanCheck(row interface{ Scan(...any) error }) (*models.HealthCheck, error) {
	var (
		c          models.HealthCheck
		statusCode sql.NullInt64
		errType    sql.NullString
		errMsg     sql.NullString
		success    nullBool
	)
	err := row.Scan(&c.ID, &c.EndpointID, &c.CheckedAt, &statusCode, &c.ResponseTimeMs,
		&success, &errType, &errMsg, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	if statusCode.Valid {
		v := int(statusCode.Int64)
		c.StatusCode = &v
	}
	c.Success = success.Bool
	if errType.Valid {
		v := errType.String
		c.ErrorType = &v
	}
	if errMsg.Valid {
		v := errMsg.String
		c.ErrorMessage = &v
	}
	c.CheckedAt = c.CheckedAt.UTC()
	c.CreatedAt = c.CreatedAt.UTC()
	return &c, nil
}

const checkCols = `id, endpoint_id, checked_at, status_code, response_time_ms, success, error_type, error_message, created_at`

func (s *SQLStore) ListChecks(ctx context.Context, endpointID int64, since time.Time, limit int) ([]models.HealthCheck, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT `+checkCols+` FROM health_checks
		WHERE endpoint_id=? AND checked_at >= ?
		ORDER BY checked_at DESC LIMIT ?`), endpointID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.HealthCheck{}
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// SelectEndpointSamples returns the most recent raw observations for one
// endpoint (newest first) within the window, bounded by limit.
func (s *SQLStore) SelectEndpointSamples(ctx context.Context, endpointID int64, since time.Time, limit int) ([]LatencySample, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT endpoint_id, checked_at, response_time_ms, success
		FROM health_checks
		WHERE endpoint_id=? AND checked_at >= ?
		ORDER BY checked_at DESC LIMIT ?`), endpointID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSamples(rows)
}

// SelectAllSamples returns per-endpoint raw observations for every endpoint,
// newest-first bounded per endpoint, used for dashboard-wide analytics.
func (s *SQLStore) SelectAllSamples(ctx context.Context, since time.Time, perEndpointLimit int) (map[int64][]LatencySample, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT endpoint_id, checked_at, response_time_ms, success FROM (
			SELECT endpoint_id, checked_at, response_time_ms, success,
			       ROW_NUMBER() OVER (PARTITION BY endpoint_id ORDER BY checked_at DESC) AS rn
			FROM health_checks WHERE checked_at >= ?
		) t WHERE rn <= ?
		ORDER BY endpoint_id, checked_at ASC`), since, perEndpointLimit)
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

func scanSamples(rows *sql.Rows) ([]LatencySample, error) {
	out := []LatencySample{}
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
		out = append(out, sample)
	}
	return out, rows.Err()
}

// CountOutcomes returns (total, successful) checks in the window, optionally
// scoped to one endpoint.
func (s *SQLStore) CountOutcomes(ctx context.Context, endpointID *int64, since time.Time) (int64, int64, error) {
	q := `SELECT COUNT(*), COALESCE(SUM(CASE WHEN success THEN 1 ELSE 0 END), 0)
	      FROM health_checks WHERE checked_at >= ?`
	var args []any
	args = append(args, since.UTC())
	if endpointID != nil {
		q += ` AND endpoint_id = ?`
		args = append(args, *endpointID)
	}
	var total, success int64
	err := s.db.QueryRowContext(ctx, s.q(q), args...).Scan(&total, &success)
	return total, success, err
}

// StatusCounts groups checks by HTTP status code and error type within the
// window, optionally scoped to one endpoint. StatusCode 0 means no response.
func (s *SQLStore) StatusCounts(ctx context.Context, endpointID *int64, since time.Time) ([]StatusCount, error) {
	q := `SELECT COALESCE(status_code, 0), COALESCE(error_type, ''), COUNT(*)
	      FROM health_checks WHERE checked_at >= ?`
	var args []any
	args = append(args, since.UTC())
	if endpointID != nil {
		q += ` AND endpoint_id = ?`
		args = append(args, *endpointID)
	}
	q += ` GROUP BY status_code, error_type`

	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
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

// PruneChecksBefore deletes raw health_checks older than cutoff (PRD FR-14).
func (s *SQLStore) PruneChecksBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM health_checks WHERE checked_at < ?`), cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneChecksByOrgPlan enforces per-organization retention horizons based on
// each tenant's subscribed plan, plus defaultCutoff for legacy NULL-org rows.
func (s *SQLStore) PruneChecksByOrgPlan(ctx context.Context, cutoffsByPlan map[string]time.Time, defaultCutoff time.Time) (int64, error) {
	var total int64
	for planID, cutoff := range cutoffsByPlan {
		if cutoff.IsZero() {
			continue
		}
		res, err := s.db.ExecContext(ctx, s.q(`
			DELETE FROM health_checks
			WHERE endpoint_id IN (
				SELECT e.id FROM endpoints e
				JOIN organizations o ON o.id = e.org_id
				WHERE o.plan = ?
			) AND checked_at < ?`), planID, cutoff.UTC())
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if !defaultCutoff.IsZero() {
		res, err := s.db.ExecContext(ctx, s.q(`
			DELETE FROM health_checks
			WHERE endpoint_id IN (SELECT id FROM endpoints WHERE org_id IS NULL)
			  AND checked_at < ?`), defaultCutoff.UTC())
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}

var _ = errors.Is // keep errors import when interface evolves
