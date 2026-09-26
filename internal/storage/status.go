package storage

import (
	"context"
	"database/sql"
	"errors"

	"uptimex/internal/models"
)

const statusCols = `endpoint_id, state, consecutive_failures, last_success_at, last_failure_at, last_checked_at, updated_at`

func scanStatus(row interface{ Scan(...any) error }) (*models.EndpointStatus, error) {
	var (
		st          models.EndpointStatus
		lastSuccess nullTime
		lastFailure nullTime
		lastChecked nullTime
		updatedAt   nullTime
	)
	err := row.Scan(&st.EndpointID, &st.State, &st.ConsecutiveFailures,
		&lastSuccess, &lastFailure, &lastChecked, &updatedAt)
	if err != nil {
		return nil, err
	}
	st.LastSuccessAt = lastSuccess.Ptr()
	st.LastFailureAt = lastFailure.Ptr()
	st.LastCheckedAt = lastChecked.Ptr()
	st.UpdatedAt = updatedAt.Time.UTC()
	return &st, nil
}

func (s *SQLStore) GetStatus(ctx context.Context, endpointID int64) (*models.EndpointStatus, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+statusCols+` FROM endpoint_status WHERE endpoint_id=?`), endpointID)
	st, err := scanStatus(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return st, err
}

// UpsertStatus writes the failure-detector state for an endpoint atomically.
func (s *SQLStore) UpsertStatus(ctx context.Context, st *models.EndpointStatus) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO endpoint_status (endpoint_id, state, consecutive_failures, last_success_at, last_failure_at, last_checked_at, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (endpoint_id) DO UPDATE SET
			state = excluded.state,
			consecutive_failures = excluded.consecutive_failures,
			last_success_at = excluded.last_success_at,
			last_failure_at = excluded.last_failure_at,
			last_checked_at = excluded.last_checked_at,
			updated_at = excluded.updated_at`),
		st.EndpointID, string(st.State), st.ConsecutiveFailures,
		timePtrArg(st.LastSuccessAt), timePtrArg(st.LastFailureAt), timePtrArg(st.LastCheckedAt),
		st.UpdatedAt.UTC())
	return err
}

func (s *SQLStore) ListStatuses(ctx context.Context) ([]models.EndpointStatus, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+statusCols+` FROM endpoint_status ORDER BY endpoint_id`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.EndpointStatus{}
	for rows.Next() {
		st, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}
