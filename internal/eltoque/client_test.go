package eltoque

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if r.URL.Query().Get("date_from") == "" || r.URL.Query().Get("date_to") == "" {
			t.Errorf("missing date range")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"date":"2026-11-08","hour":8,"minutes":45,"seconds":5,"tasas":{"USD":930.0,"ECU":1020.5,"BTC":null}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	client.now = func() time.Time {
		return time.Date(2026, 11, 8, 8, 50, 0, 0, havanaLocation)
	}

	rates, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if rates.SourceDate != "2026-11-08 08:45:05" {
		t.Fatalf("source date = %q", rates.SourceDate)
	}
	wantTS := time.Date(2026, 11, 8, 8, 45, 5, 0, havanaLocation).Unix()
	if rates.SourceTS != wantTS {
		t.Fatalf("source ts = %d, want %d", rates.SourceTS, wantTS)
	}
	if rates.Tasas["USD"] != 930 || rates.Tasas["ECU"] != 1020.5 {
		t.Fatalf("tasas = %+v", rates.Tasas)
	}
	if _, exists := rates.Tasas["BTC"]; exists {
		t.Fatalf("null rate should be skipped")
	}
}

func TestFetchRetriesAndFails(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	client.attempts = 3
	client.backoff = time.Millisecond

	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestFetchRejectsEmptyRates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"date":"2026-11-08","hour":8,"minutes":45,"seconds":5,"tasas":{}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	client.attempts = 1

	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchRejectsMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 10))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	client.attempts = 1

	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
