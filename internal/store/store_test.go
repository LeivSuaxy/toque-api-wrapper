package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func TestRatesUpsertAndQueries(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	records := []RateRecord{
		{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:00:00", SourceTS: 1},
		{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:05:00", SourceTS: 2},
		{CurrencyCode: "ECU", Rate: 30, SourceDate: "2026-11-08 08:05:00", SourceTS: 2},
	}
	if err := database.UpsertRates(ctx, records); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Same natural key updates the rate instead of duplicating it.
	if err := database.UpsertRates(ctx, []RateRecord{
		{CurrencyCode: "USD", Rate: 15, SourceDate: "2026-11-08 08:05:00", SourceTS: 2},
	}); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	latest, err := database.LatestRate(ctx, "USD")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Rate != 15 || latest.SourceDate != "2026-11-08 08:05:00" {
		t.Fatalf("latest = %+v", latest)
	}

	recent, err := database.RecentRates(ctx, "USD", 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent len = %d, want 2", len(recent))
	}
	if recent[0].Rate != 15 || recent[1].Rate != 10 {
		t.Fatalf("recent order = %+v", recent)
	}

	codes, err := database.CurrencyCodes(ctx)
	if err != nil {
		t.Fatalf("codes: %v", err)
	}
	if len(codes) != 2 || codes[0] != "ECU" || codes[1] != "USD" {
		t.Fatalf("codes = %v", codes)
	}

	count, err := database.CountRates(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	lastCollected, err := database.LastCollectedAt(ctx)
	if err != nil {
		t.Fatalf("last collected: %v", err)
	}
	if lastCollected != "2026-11-08 08:05:00" {
		t.Fatalf("last collected = %q", lastCollected)
	}

	deleted, err := database.PruneRates(ctx, 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestRatesBetween(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	if err := database.UpsertRates(ctx, []RateRecord{
		{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-01 08:00:00", SourceTS: 1},
		{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-02 08:00:00", SourceTS: 2},
		{CurrencyCode: "USD", Rate: 30, SourceDate: "2026-11-03 08:00:00", SourceTS: 3},
		{CurrencyCode: "USD", Rate: 40, SourceDate: "2026-11-04 08:00:00", SourceTS: 4},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	from := int64(2)
	to := int64(4)
	ranged, err := database.RatesBetween(ctx, "USD", &from, &to, 10)
	if err != nil {
		t.Fatalf("rates between: %v", err)
	}
	if len(ranged) != 2 || ranged[0].SourceTS != 3 || ranged[1].SourceTS != 2 {
		t.Fatalf("ranged = %+v", ranged)
	}

	openEnded, err := database.RatesBetween(ctx, "USD", &from, nil, 10)
	if err != nil {
		t.Fatalf("open ended: %v", err)
	}
	if len(openEnded) != 3 || openEnded[0].SourceTS != 4 {
		t.Fatalf("open ended = %+v", openEnded)
	}

	limited, err := database.RatesBetween(ctx, "USD", nil, nil, 1)
	if err != nil {
		t.Fatalf("limited: %v", err)
	}
	if len(limited) != 1 || limited[0].SourceTS != 4 {
		t.Fatalf("limited = %+v", limited)
	}

	empty, err := database.RatesBetween(ctx, "USD", nil, nil, 0)
	if err != nil || empty != nil {
		t.Fatalf("empty = %+v, err = %v", empty, err)
	}
}

func TestAPIKeysLifecycle(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	id, err := database.CreateAPIKey(ctx, "tk_live_test", "hash-1", "bot", 60)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	key, err := database.GetAPIKeyByKeyID(ctx, "tk_live_test")
	if err != nil {
		t.Fatalf("get by key id: %v", err)
	}
	if key.ID != id || !key.Enabled || key.RateLimitPerMinute != 60 {
		t.Fatalf("key = %+v", key)
	}

	if err := database.RevokeAPIKey(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	key, err = database.GetAPIKeyByID(ctx, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if key.Enabled || key.RevokedAt == "" {
		t.Fatalf("key not revoked: %+v", key)
	}

	if err := database.EnableAPIKey(ctx, id); err != nil {
		t.Fatalf("enable: %v", err)
	}
	key, err = database.GetAPIKeyByID(ctx, id)
	if err != nil {
		t.Fatalf("get after enable: %v", err)
	}
	if !key.Enabled || key.RevokedAt != "" {
		t.Fatalf("key not enabled: %+v", key)
	}

	if err := database.RotateAPIKeySecret(ctx, id, "hash-2"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	key, err = database.GetAPIKeyByID(ctx, id)
	if err != nil {
		t.Fatalf("get after rotate: %v", err)
	}
	if key.SecretHash != "hash-2" {
		t.Fatalf("secret hash = %q", key.SecretHash)
	}

	if _, err := database.GetAPIKeyByID(ctx, 9999); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUsageAndPruning(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	id, err := database.CreateAPIKey(ctx, "tk_live_usage", "hash", "usage", 60)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := database.InsertAPIRequest(ctx, APIRequest{
		APIKeyID:      id,
		Method:        "GET",
		Path:          "/v1/rates",
		Query:         "fixed=true",
		StatusCode:    200,
		RequestBytes:  10,
		ResponseBytes: 100,
		DurationMS:    5,
		RemoteIP:      "127.0.0.1",
		UserAgent:     "test",
	}); err != nil {
		t.Fatalf("insert request: %v", err)
	}

	since := time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05")
	usage, err := database.Usage(ctx, id, since)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.Requests != 1 || usage.RequestBytes != 10 || usage.ResponseBytes != 100 {
		t.Fatalf("usage = %+v", usage)
	}

	keys, err := database.ListAPIKeysWithUsage(ctx, since)
	if err != nil {
		t.Fatalf("list with usage: %v", err)
	}
	if len(keys) != 1 || keys[0].Requests != 1 || keys[0].RequestBytes != 10 {
		t.Fatalf("keys = %+v", keys)
	}

	if err := database.InsertAuthFailure(ctx, AuthFailure{
		KeyID: "tk_live_usage", Reason: "bad_credentials", RemoteIP: "127.0.0.1",
	}); err != nil {
		t.Fatalf("insert auth failure: %v", err)
	}

	if err := database.InsertSyncError(ctx, "fetch failed", "timeout"); err != nil {
		t.Fatalf("insert sync error: %v", err)
	}

	future := time.Now().UTC().Add(time.Hour).Format("2006-01-02 15:04:05")
	if deleted, err := database.PruneAPIRequests(ctx, future); err != nil || deleted != 1 {
		t.Fatalf("prune requests: deleted=%d err=%v", deleted, err)
	}
	if deleted, err := database.PruneAuthFailures(ctx, future); err != nil || deleted != 1 {
		t.Fatalf("prune auth failures: deleted=%d err=%v", deleted, err)
	}
}
