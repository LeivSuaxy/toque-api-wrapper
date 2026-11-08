package store

import (
	"context"
	"database/sql"
	"fmt"
)

type APIRequest struct {
	APIKeyID      int64
	Method        string
	Path          string
	Query         string
	StatusCode    int
	RequestBytes  int64
	ResponseBytes int64
	DurationMS    int64
	RemoteIP      string
	UserAgent     string
}

type AuthFailure struct {
	KeyID     string
	Reason    string
	RemoteIP  string
	UserAgent string
}

type Usage struct {
	Requests      int64
	RequestBytes  int64
	ResponseBytes int64
	LastRequestAt string
}

func (s *Store) InsertAPIRequest(ctx context.Context, request APIRequest) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_requests
			(api_key_id, method, path, query, status_code, request_bytes,
			 response_bytes, duration_ms, remote_ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		request.APIKeyID,
		request.Method,
		request.Path,
		request.Query,
		request.StatusCode,
		request.RequestBytes,
		request.ResponseBytes,
		request.DurationMS,
		request.RemoteIP,
		request.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("insert api request: %w", err)
	}
	return nil
}

func (s *Store) InsertAuthFailure(ctx context.Context, failure AuthFailure) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_failures (key_id, reason, remote_ip, user_agent)
		VALUES (?, ?, ?, ?)
	`, failure.KeyID, failure.Reason, failure.RemoteIP, failure.UserAgent)
	return err
}

func (s *Store) Usage(ctx context.Context, apiKeyID int64, since string) (Usage, error) {
	var (
		usage Usage
		last  sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(request_bytes), 0),
		       COALESCE(SUM(response_bytes), 0),
		       MAX(created_at)
		FROM api_requests
		WHERE api_key_id = ? AND created_at >= ?
	`, apiKeyID, since).Scan(
		&usage.Requests,
		&usage.RequestBytes,
		&usage.ResponseBytes,
		&last,
	)
	if err != nil {
		return Usage{}, err
	}
	if last.Valid {
		usage.LastRequestAt = last.String
	}
	return usage, nil
}

func (s *Store) PruneAPIRequests(ctx context.Context, before string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM api_requests WHERE created_at < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("prune api requests: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) PruneAuthFailures(ctx context.Context, before string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_failures WHERE created_at < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("prune auth failures: %w", err)
	}
	return result.RowsAffected()
}
