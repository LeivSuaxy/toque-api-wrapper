package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("store: not found")

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	u := url.URL{Scheme: "file", Path: abs}
	query := u.Query()
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(NORMAL)")
	query.Add("_pragma", "foreign_keys(ON)")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	for _, statement := range schema {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS rates (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		currency_code TEXT    NOT NULL,
		rate          REAL    NOT NULL,
		source_date   TEXT    NOT NULL,
		source_ts     INTEGER NOT NULL,
		collected_at  TEXT    NOT NULL DEFAULT (datetime('now')),
		source        TEXT    NOT NULL DEFAULT 'eltoque',
		UNIQUE (currency_code, source_date)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rates_lookup
		ON rates (currency_code, source_ts DESC)`,
	`CREATE TABLE IF NOT EXISTS api_keys (
		id                    INTEGER PRIMARY KEY AUTOINCREMENT,
		key_id                TEXT    NOT NULL UNIQUE,
		secret_hash           TEXT    NOT NULL,
		name                  TEXT    NOT NULL,
		enabled               INTEGER NOT NULL DEFAULT 1,
		rate_limit_per_minute INTEGER NOT NULL DEFAULT 60,
		created_at            TEXT    NOT NULL DEFAULT (datetime('now')),
		last_used_at          TEXT,
		revoked_at            TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_api_keys_key_id ON api_keys (key_id)`,
	`CREATE TABLE IF NOT EXISTS api_requests (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		api_key_id     INTEGER NOT NULL REFERENCES api_keys(id),
		method         TEXT    NOT NULL,
		path           TEXT    NOT NULL,
		query          TEXT,
		status_code    INTEGER NOT NULL,
		request_bytes  INTEGER NOT NULL DEFAULT 0,
		response_bytes INTEGER NOT NULL DEFAULT 0,
		duration_ms    INTEGER NOT NULL,
		remote_ip      TEXT,
		user_agent     TEXT,
		created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
	)`,
	`CREATE INDEX IF NOT EXISTS idx_api_requests_key_time
		ON api_requests (api_key_id, created_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_api_requests_time
		ON api_requests (created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS auth_failures (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		key_id     TEXT,
		reason     TEXT NOT NULL,
		remote_ip  TEXT,
		user_agent TEXT,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	`CREATE INDEX IF NOT EXISTS idx_auth_failures_time
		ON auth_failures (created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS sync_errors (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		message    TEXT NOT NULL,
		detail     TEXT
	)`,
}
