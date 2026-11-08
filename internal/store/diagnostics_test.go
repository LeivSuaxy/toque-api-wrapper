package store

import (
	"context"
	"testing"
)

func TestDiagnosticsQueries(t *testing.T) {
	database := newTestStore(t)
	ctx := context.Background()

	if err := database.UpsertRates(ctx, []RateRecord{
		{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:00:00", SourceTS: 1},
		{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:05:00", SourceTS: 2},
		{CurrencyCode: "ECU", Rate: 30, SourceDate: "2026-11-08 08:05:00", SourceTS: 2},
	}); err != nil {
		t.Fatalf("upsert rates: %v", err)
	}

	keyID, err := database.CreateAPIKey(ctx, "tk_live_diag", "hash", "diag", 60)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := database.InsertAPIRequest(ctx, APIRequest{
		APIKeyID:   keyID,
		Method:     "GET",
		Path:       "/v1/rates",
		StatusCode: 200,
		DurationMS: 5,
		RemoteIP:   "127.0.0.1",
		UserAgent:  "test-agent",
	}); err != nil {
		t.Fatalf("insert request: %v", err)
	}
	if err := database.InsertAuthFailure(ctx, AuthFailure{
		KeyID:    "tk_bad",
		Reason:   "invalid key",
		RemoteIP: "127.0.0.1",
	}); err != nil {
		t.Fatalf("insert auth failure: %v", err)
	}
	if err := database.InsertSyncError(ctx, "fetch eltoque rates", "timeout"); err != nil {
		t.Fatalf("insert sync error: %v", err)
	}

	latest, err := database.LatestRates(ctx)
	if err != nil {
		t.Fatalf("latest rates: %v", err)
	}
	if len(latest) != 2 || latest[0].CurrencyCode != "ECU" || latest[1].Rate != 20 {
		t.Fatalf("latest rates = %+v", latest)
	}

	recentUSD, err := database.RecentRatesFiltered(ctx, "USD", 10)
	if err != nil {
		t.Fatalf("recent filtered: %v", err)
	}
	if len(recentUSD) != 2 || recentUSD[0].Rate != 20 {
		t.Fatalf("recent USD = %+v", recentUSD)
	}

	recentAll, err := database.RecentRatesFiltered(ctx, "", 10)
	if err != nil {
		t.Fatalf("recent all: %v", err)
	}
	if len(recentAll) != 3 {
		t.Fatalf("recent all len = %d, want 3", len(recentAll))
	}

	requests, err := database.RecentAPIRequests(ctx, keyID, 10)
	if err != nil {
		t.Fatalf("recent requests: %v", err)
	}
	if len(requests) != 1 || requests[0].KeyName != "diag" || requests[0].Path != "/v1/rates" {
		t.Fatalf("requests = %+v", requests)
	}

	allRequests, err := database.RecentAPIRequests(ctx, 0, 10)
	if err != nil {
		t.Fatalf("recent all requests: %v", err)
	}
	if len(allRequests) != 1 {
		t.Fatalf("all requests len = %d, want 1", len(allRequests))
	}

	failures, err := database.RecentAuthFailures(ctx, 10)
	if err != nil {
		t.Fatalf("recent failures: %v", err)
	}
	if len(failures) != 1 || failures[0].Reason != "invalid key" {
		t.Fatalf("failures = %+v", failures)
	}

	syncErrors, err := database.RecentSyncErrors(ctx, 10)
	if err != nil {
		t.Fatalf("recent sync errors: %v", err)
	}
	if len(syncErrors) != 1 || syncErrors[0].Detail != "timeout" {
		t.Fatalf("sync errors = %+v", syncErrors)
	}

	summary, err := database.DiagnosticsSummary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.RatesCount != 3 || summary.APIRequestsCount != 1 ||
		summary.AuthFailuresCount != 1 || summary.SyncErrorsCount != 1 {
		t.Fatalf("summary counts = %+v", summary)
	}
	if summary.LastRateSourceDate == "" || summary.LastSyncErrorAt == "" {
		t.Fatalf("summary timestamps = %+v", summary)
	}

	keys, err := database.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 1 || keys[0].Name != "diag" {
		t.Fatalf("keys = %+v", keys)
	}
}
