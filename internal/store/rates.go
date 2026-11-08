package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Rate struct {
	ID           int64
	CurrencyCode string
	Rate         float64
	SourceDate   string
	SourceTS     int64
	CollectedAt  string
}

type RateRecord struct {
	CurrencyCode string
	Rate         float64
	SourceDate   string
	SourceTS     int64
}

func (s *Store) UpsertRates(ctx context.Context, records []RateRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rates transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO rates (currency_code, rate, source_date, source_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (currency_code, source_date)
		DO UPDATE SET rate = excluded.rate, source_ts = excluded.source_ts
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert rates: %w", err)
	}
	defer statement.Close()

	for _, record := range records {
		if _, err := statement.ExecContext(ctx, record.CurrencyCode, record.Rate, record.SourceDate, record.SourceTS); err != nil {
			return fmt.Errorf("upsert rate %s: %w", record.CurrencyCode, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rates transaction: %w", err)
	}
	return nil
}

func (s *Store) LatestRate(ctx context.Context, currencyCode string) (Rate, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, currency_code, rate, source_date, source_ts, collected_at
		FROM rates
		WHERE currency_code = ?
		ORDER BY source_ts DESC
		LIMIT 1
	`, currencyCode)

	rate, err := scanRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Rate{}, ErrNotFound
	}
	return rate, err
}

func (s *Store) RecentRates(ctx context.Context, currencyCode string, limit int) ([]Rate, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, currency_code, rate, source_date, source_ts, collected_at
		FROM rates
		WHERE currency_code = ?
		ORDER BY source_ts DESC
		LIMIT ?
	`, currencyCode, limit)
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

func (s *Store) RatesBetween(ctx context.Context, currencyCode string, fromTS, toTS *int64, limit int) ([]Rate, error) {
	if limit <= 0 {
		return nil, nil
	}

	query := `
		SELECT id, currency_code, rate, source_date, source_ts, collected_at
		FROM rates
		WHERE currency_code = ?
	`
	args := make([]any, 0, 4)
	args = append(args, currencyCode)
	if fromTS != nil {
		query += ` AND source_ts >= ?`
		args = append(args, *fromTS)
	}
	if toTS != nil {
		query += ` AND source_ts < ?`
		args = append(args, *toTS)
	}
	query += ` ORDER BY source_ts DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query rates between: %w", err)
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
		return nil, fmt.Errorf("iterate rates between: %w", err)
	}
	return rates, nil
}

func (s *Store) CurrencyCodes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT currency_code
		FROM rates
		ORDER BY currency_code
	`)
	if err != nil {
		return nil, fmt.Errorf("query currency codes: %w", err)
	}
	defer rows.Close()

	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate currency codes: %w", err)
	}
	return codes, nil
}

func (s *Store) LastCollectedAt(ctx context.Context) (string, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(source_date) FROM rates`).Scan(&value); err != nil {
		return "", err
	}
	if !value.Valid {
		return "", nil
	}
	return value.String, nil
}

func (s *Store) CountRates(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rates`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) PruneRates(ctx context.Context, cutoffTS int64) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM rates WHERE source_ts < ?`, cutoffTS)
	if err != nil {
		return 0, fmt.Errorf("prune rates: %w", err)
	}
	return result.RowsAffected()
}

func (s *Store) InsertSyncError(ctx context.Context, message, detail string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_errors (message, detail)
		VALUES (?, ?)
	`, message, detail)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRate(row scanner) (Rate, error) {
	var rate Rate
	err := row.Scan(
		&rate.ID,
		&rate.CurrencyCode,
		&rate.Rate,
		&rate.SourceDate,
		&rate.SourceTS,
		&rate.CollectedAt,
	)
	return rate, err
}
