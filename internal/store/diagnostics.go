package store

import (
	"context"
	"fmt"
)

type SyncError struct {
	ID        int64
	CreatedAt string
	Message   string
	Detail    string
}

type APIRequestRecord struct {
	ID            int64
	APIKeyID      int64
	KeyName       string
	Method        string
	Path          string
	Query         string
	StatusCode    int
	RequestBytes  int64
	ResponseBytes int64
	DurationMS    int64
	RemoteIP      string
	UserAgent     string
	CreatedAt     string
}

type AuthFailureRecord struct {
	ID        int64
	KeyID     string
	Reason    string
	RemoteIP  string
	UserAgent string
	CreatedAt string
}

type DiagnosticsSummary struct {
	RatesCount          int64
	APIRequestsCount    int64
	AuthFailuresCount   int64
	SyncErrorsCount     int64
	LastRateSourceDate  string
	LastRateCollectedAt string
	LastRequestAt       string
	LastSyncErrorAt     string
}

func (s *Store) LatestRates(ctx context.Context) ([]Rate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.currency_code, r.rate, r.source_date, r.source_ts, r.collected_at
		FROM rates r
		WHERE r.id = (
			SELECT x.id
			FROM rates x
			WHERE x.currency_code = r.currency_code
			ORDER BY x.source_ts DESC, x.id DESC
			LIMIT 1
		)
		ORDER BY r.currency_code
	`)
	if err != nil {
		return nil, fmt.Errorf("query latest rates: %w", err)
	}
	defer rows.Close()

	var rates []Rate
	for rows.Next() {
		rate, err := scanRate(rows)
		if err != nil {
			return nil, err
		}
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest rates: %w", err)
	}
	return rates, nil
}

func (s *Store) RecentRatesFiltered(ctx context.Context, currencyCode string, limit int) ([]Rate, error) {
	if limit <= 0 {
		return nil, nil
	}

	query := `
		SELECT id, currency_code, rate, source_date, source_ts, collected_at
		FROM rates
	`
	args := make([]any, 0, 2)
	if currencyCode != "" {
		query += ` WHERE currency_code = ?`
		args = append(args, currencyCode)
	}
	query += ` ORDER BY source_ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recent rates: %w", err)
	}
	defer rows.Close()

	rates := make([]Rate, 0, limit)
	for rows.Next() {
		rate, err := scanRate(rows)
		if err != nil {
			return nil, err
		}
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent rates: %w", err)
	}
	return rates, nil
}

func (s *Store) RecentSyncErrors(ctx context.Context, limit int) ([]SyncError, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, created_at, message, COALESCE(detail, '')
		FROM sync_errors
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query sync errors: %w", err)
	}
	defer rows.Close()

	errors := make([]SyncError, 0, limit)
	for rows.Next() {
		var item SyncError
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Message, &item.Detail); err != nil {
			return nil, err
		}
		errors = append(errors, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync errors: %w", err)
	}
	return errors, nil
}

func (s *Store) RecentAPIRequests(ctx context.Context, apiKeyID int64, limit int) ([]APIRequestRecord, error) {
	if limit <= 0 {
		return nil, nil
	}

	query := `
		SELECT r.id,
		       r.api_key_id,
		       COALESCE(k.name, ''),
		       r.method,
		       r.path,
		       COALESCE(r.query, ''),
		       r.status_code,
		       r.request_bytes,
		       r.response_bytes,
		       r.duration_ms,
		       COALESCE(r.remote_ip, ''),
		       COALESCE(r.user_agent, ''),
		       r.created_at
		FROM api_requests r
		LEFT JOIN api_keys k ON k.id = r.api_key_id
	`
	args := make([]any, 0, 2)
	if apiKeyID > 0 {
		query += ` WHERE r.api_key_id = ?`
		args = append(args, apiKeyID)
	}
	query += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query api requests: %w", err)
	}
	defer rows.Close()

	requests := make([]APIRequestRecord, 0, limit)
	for rows.Next() {
		var item APIRequestRecord
		if err := rows.Scan(
			&item.ID,
			&item.APIKeyID,
			&item.KeyName,
			&item.Method,
			&item.Path,
			&item.Query,
			&item.StatusCode,
			&item.RequestBytes,
			&item.ResponseBytes,
			&item.DurationMS,
			&item.RemoteIP,
			&item.UserAgent,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		requests = append(requests, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api requests: %w", err)
	}
	return requests, nil
}

func (s *Store) RecentAuthFailures(ctx context.Context, limit int) ([]AuthFailureRecord, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id,
		       COALESCE(key_id, ''),
		       reason,
		       COALESCE(remote_ip, ''),
		       COALESCE(user_agent, ''),
		       created_at
		FROM auth_failures
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query auth failures: %w", err)
	}
	defer rows.Close()

	failures := make([]AuthFailureRecord, 0, limit)
	for rows.Next() {
		var item AuthFailureRecord
		if err := rows.Scan(
			&item.ID,
			&item.KeyID,
			&item.Reason,
			&item.RemoteIP,
			&item.UserAgent,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		failures = append(failures, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auth failures: %w", err)
	}
	return failures, nil
}

func (s *Store) DiagnosticsSummary(ctx context.Context) (DiagnosticsSummary, error) {
	var summary DiagnosticsSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM rates),
			(SELECT COUNT(*) FROM api_requests),
			(SELECT COUNT(*) FROM auth_failures),
			(SELECT COUNT(*) FROM sync_errors),
			COALESCE((SELECT MAX(source_date) FROM rates), ''),
			COALESCE((SELECT MAX(collected_at) FROM rates), ''),
			COALESCE((SELECT MAX(created_at) FROM api_requests), ''),
			COALESCE((SELECT MAX(created_at) FROM sync_errors), '')
	`).Scan(
		&summary.RatesCount,
		&summary.APIRequestsCount,
		&summary.AuthFailuresCount,
		&summary.SyncErrorsCount,
		&summary.LastRateSourceDate,
		&summary.LastRateCollectedAt,
		&summary.LastRequestAt,
		&summary.LastSyncErrorAt,
	)
	if err != nil {
		return DiagnosticsSummary{}, fmt.Errorf("query diagnostics summary: %w", err)
	}
	return summary, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, key_id, secret_hash, name, enabled, rate_limit_per_minute,
		       created_at, COALESCE(last_used_at, ''), COALESCE(revoked_at, '')
		FROM api_keys
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return keys, nil
}
