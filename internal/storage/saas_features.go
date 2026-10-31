// Persistence for per-tenant alert channels, per-organization API keys, and
// single-use password reset tokens (0003_saas_complete.sql).
package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"uptimex/internal/models"
)

// --- per-tenant alert channels ----------------------------------------------

const alertChannelCols = `id, org_id, name, type, target, secret, enabled, created_at, updated_at`

func scanAlertChannel(row interface{ Scan(...any) error }) (*models.AlertChannel, error) {
	var (
		ch               models.AlertChannel
		enabled          nullBool
		created, updated nullTime
	)
	err := row.Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Target, &ch.Secret, &enabled, &created, &updated)
	if err != nil {
		return nil, err
	}
	ch.Enabled = enabled.Bool
	ch.HasSecret = ch.Secret != ""
	ch.CreatedAt = created.Time.UTC()
	ch.UpdatedAt = updated.Time.UTC()
	return &ch, nil
}

func (s *SQLStore) CreateAlertChannel(ctx context.Context, ch *models.AlertChannel) error {
	now := time.Now().UTC()
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO alert_channels (org_id, name, type, target, secret, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?) RETURNING id`),
		ch.OrgID, ch.Name, ch.Type, ch.Target, ch.Secret, ch.Enabled, now, now).Scan(&ch.ID)
	if err != nil {
		return err
	}
	ch.HasSecret = ch.Secret != ""
	ch.CreatedAt, ch.UpdatedAt = now, now
	return nil
}

func (s *SQLStore) GetAlertChannel(ctx context.Context, id int64) (*models.AlertChannel, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+alertChannelCols+` FROM alert_channels WHERE id = ?`), id)
	ch, err := scanAlertChannel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ch, err
}

func (s *SQLStore) ListAlertChannelsByOrg(ctx context.Context, orgID int64, onlyEnabled bool) ([]models.AlertChannel, error) {
	q := `SELECT ` + alertChannelCols + ` FROM alert_channels WHERE org_id = ?`
	args := []any{orgID}
	if onlyEnabled {
		q += ` AND enabled = ?`
		args = append(args, true)
	}
	q += ` ORDER BY id`

	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.AlertChannel{}
	for rows.Next() {
		ch, err := scanAlertChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ch)
	}
	return out, rows.Err()
}

func (s *SQLStore) UpdateAlertChannel(ctx context.Context, ch *models.AlertChannel) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.q(`
		UPDATE alert_channels SET name=?, type=?, target=?, secret=?, enabled=?, updated_at=?
		WHERE id=? AND org_id=?`),
		ch.Name, ch.Type, ch.Target, ch.Secret, ch.Enabled, now, ch.ID, ch.OrgID)
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
	ch.HasSecret = ch.Secret != ""
	ch.UpdatedAt = now
	return nil
}

func (s *SQLStore) DeleteAlertChannel(ctx context.Context, orgID, id int64) error {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM alert_channels WHERE id = ? AND org_id = ?`), id, orgID)
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

// --- per-organization API keys ----------------------------------------------

const apiKeyCols = `id, org_id, name, key_prefix, token_hash, last_used_at, created_at`

func scanAPIKey(row interface{ Scan(...any) error }) (*models.APIKey, error) {
	var (
		k        models.APIKey
		lastUsed nullTime
		created  nullTime
	)
	err := row.Scan(&k.ID, &k.OrgID, &k.Name, &k.KeyPrefix, &k.TokenHash, &lastUsed, &created)
	if err != nil {
		return nil, err
	}
	k.LastUsedAt = lastUsed.Ptr()
	k.CreatedAt = created.Time.UTC()
	return &k, nil
}

func (s *SQLStore) CreateAPIKey(ctx context.Context, k *models.APIKey) error {
	now := time.Now().UTC()
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO api_keys (org_id, name, key_prefix, token_hash, created_at)
		VALUES (?,?,?,?,?) RETURNING id`),
		k.OrgID, k.Name, k.KeyPrefix, k.TokenHash, now).Scan(&k.ID)
	if err != nil {
		return err
	}
	k.CreatedAt = now
	return nil
}

func (s *SQLStore) ListAPIKeysByOrg(ctx context.Context, orgID int64) ([]models.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+apiKeyCols+` FROM api_keys WHERE org_id = ? ORDER BY created_at DESC`), orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetAPIKeyByHash(ctx context.Context, tokenHash string) (*models.APIKey, error) {
	row := s.db.QueryRowContext(ctx, s.q(`SELECT `+apiKeyCols+` FROM api_keys WHERE token_hash = ?`), tokenHash)
	k, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

func (s *SQLStore) TouchAPIKeyLastUsed(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`), at.UTC(), id)
	return err
}

func (s *SQLStore) DeleteAPIKey(ctx context.Context, orgID, id int64) error {
	res, err := s.db.ExecContext(ctx, s.q(`DELETE FROM api_keys WHERE id = ? AND org_id = ?`), id, orgID)
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

// --- password reset tokens --------------------------------------------------

func (s *SQLStore) CreatePasswordResetToken(ctx context.Context, t *models.PasswordResetToken) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO password_reset_tokens (token_hash, user_id, expires_at, created_at)
		VALUES (?,?,?,?)`),
		t.TokenHash, t.UserID, t.ExpiresAt.UTC(), t.CreatedAt.UTC())
	return err
}

// ConsumePasswordResetToken atomically verifies an unexpired reset token,
// rotates the account's password hash, invalidates all reset tokens for that
// user, and revokes every existing session for that user.
func (s *SQLStore) ConsumePasswordResetToken(ctx context.Context, tokenHash, newPasswordHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID int64
	err = tx.QueryRowContext(ctx, s.q(`
		SELECT user_id FROM password_reset_tokens WHERE token_hash = ? AND expires_at > ?`),
		tokenHash, time.Now().UTC()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, s.q(`UPDATE users SET password_hash = ? WHERE id = ?`), newPasswordHash, userID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM password_reset_tokens WHERE user_id = ?`), userID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, s.q(`DELETE FROM sessions WHERE user_id = ?`), userID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *SQLStore) DeleteExpiredPasswordResetTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM password_reset_tokens WHERE expires_at <= ?`), time.Now().UTC())
	return err
}
