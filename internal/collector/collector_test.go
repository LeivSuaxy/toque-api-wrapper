package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/eltoque"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

type fakeClient struct {
	rates eltoque.Rates
	err   error
}

func (f fakeClient) Fetch(_ context.Context) (eltoque.Rates, error) {
	return f.rates, f.err
}

type fakeStore struct {
	records   []store.RateRecord
	pruned    bool
	syncError string
}

func (f *fakeStore) UpsertRates(_ context.Context, records []store.RateRecord) error {
	f.records = records
	return nil
}

func (f *fakeStore) PruneRates(_ context.Context, _ int64) (int64, error) {
	f.pruned = true
	return 0, nil
}

func (f *fakeStore) InsertSyncError(_ context.Context, message, _ string) error {
	f.syncError = message
	return nil
}

func TestRunOncePersistsSortedRecords(t *testing.T) {
	store := &fakeStore{}
	runner := New(fakeClient{
		rates: eltoque.Rates{
			SourceDate: "2026-11-08 08:45:00",
			SourceTS:   100,
			Tasas:      map[string]float64{"USD": 930, "ECU": 1020},
		},
	}, store, time.Minute, 24*time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(store.records) != 2 {
		t.Fatalf("records = %d, want 2", len(store.records))
	}
	if store.records[0].CurrencyCode != "ECU" || store.records[1].CurrencyCode != "USD" {
		t.Fatalf("records not sorted: %+v", store.records)
	}
	if !store.pruned {
		t.Fatal("prune not called")
	}
}

func TestRunOnceRecordsSyncError(t *testing.T) {
	store := &fakeStore{}
	runner := New(fakeClient{err: errors.New("boom")}, store, time.Minute, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := runner.RunOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if store.syncError != "fetch eltoque rates" {
		t.Fatalf("sync error = %q", store.syncError)
	}
}
