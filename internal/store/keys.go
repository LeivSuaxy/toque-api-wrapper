package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type APIKey struct {
	ID                 int64
	KeyID              string
	SecretHash         string
	Name               string
	Enabled            bool
	RateLimitPerMinute int
	CreatedAt          string
	LastUsedAt         string
	RevokedAt          string
}

type APIKeyUsage struct {
	APIKey
	Requests      int64
	RequestBytes  int64
	ResponseBytes int64
}

func (s *Store) CreateAPIKey(ctx context.Context, keyID, secretHash, name string, rateLimitPerMinute int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO api_keys (key_id, secret_hash, name, rate_limit_per_minute)
		VALUES (?, ?, ?, ?)
	`, keyID, secretHash, name, rateLimitPerMinute)
	if err != nil {
		return 0, fmt.Errorf("create api key: %w", err)
	}
	return result.LastInsertId()
}

func (s *Store) GetAPIKeyByKeyID(ctx context.Context, keyID string) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, key_id, secret_hash, name, enabled, rate_limit_per_minute,
		       created_at, COALESCE(last_used_at, ''), COALESCE(revoked_at, '')
		FROM api_keys
		WHERE key_id = ?
	`, keyID)

	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	return key, err
}

func (s *Store) GetAPIKeyByID(ctx context.Context, id int64) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, key_id, secret_hash, name, enabled, rate_limit_per_minute,
		       created_at, COALESCE(last_used_at, ''), COALESCE(revoked_at, '')
		FROM api_keys
		WHERE id = ?
	`, id)

	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	return key, err
}

func (s *Store) ListAPIKeysWithUsage(ctx context.Context, since string) ([]APIKeyUsage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT k.id,
		       k.key_id,
		       k.secret_hash,
		       k.name,
		       k.enabled,
		       k.rate_limit_per_minute,
		       k.created_at,
		       COALESCE(k.last_used_at, ''),
		       COALESCE(k.revoked_at, ''),
		       COUNT(r.id),
		       COALESCE(SUM(r.request_bytes), 0),
		       COALESCE(SUM(r.response_bytes), 0)
		FROM api_keys k
		LEFT JOIN api_requests r
		       ON r.api_key_id = k.id
		      AND r.created_at >= ?
		GROUP BY k.id
		ORDER BY k.created_at DESC, k.id DESC
	`, since)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKeyUsage
	for rows.Next() {
		var (
			key     APIKeyUsage
			enabled int
		)
		err := rows.Scan(
			&key.ID,
			&key.KeyID,
			&key.SecretHash,
			&key.Name,
			&enabled,
			&key.RateLimitPerMinute,
			&key.CreatedAt,
			&key.LastUsedAt,
			&key.RevokedAt,
			&key.Requests,
			&key.RequestBytes,
			&key.ResponseBytes,
		)
		if err != nil {
			return nil, err
		}
		key.Enabled = enabled == 1
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return keys, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET enabled = 0, revoked_at = datetime('now')
		WHERE id = ?
	`, id)
	return err
}

func (s *Store) EnableAPIKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET enabled = 1, revoked_at = NULL
		WHERE id = ?
	`, id)
	return err
}

func (s *Store) UpdateAPIKeyRateLimit(ctx context.Context, id int64, rateLimitPerMinute int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET rate_limit_per_minute = ?
		WHERE id = ?
	`, rateLimitPerMinute, id)
	return err
}

func (s *Store) RotateAPIKeySecret(ctx context.Context, id int64, secretHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET secret_hash = ?
		WHERE id = ?
	`, secretHash, id)
	return err
}

func (s *Store) TouchAPIKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE api_keys
		SET last_used_at = datetime('now')
		WHERE id = ?
	`, id)
	return err
}

func scanAPIKey(row scanner) (APIKey, error) {
	var (
		key     APIKey
		enabled int
	)
	err := row.Scan(
		&key.ID,
		&key.KeyID,
		&key.SecretHash,
		&key.Name,
		&enabled,
		&key.RateLimitPerMinute,
		&key.CreatedAt,
		&key.LastUsedAt,
		&key.RevokedAt,
	)
	if err != nil {
		return APIKey{}, err
	}
	key.Enabled = enabled == 1
	return key, nil
}
