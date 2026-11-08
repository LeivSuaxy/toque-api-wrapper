package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

func TestRenderDiagnosticsTemplate(t *testing.T) {
	templates, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	data := map[string]any{
		"Title":    "Diagnóstico",
		"Username": "admin",
		"CSRF":     "token",
		"Diagnostics": map[string]any{
			"Summary": map[string]any{
				"RatesCount":          int64(2),
				"APIRequestsCount":    int64(1),
				"AuthFailuresCount":   int64(0),
				"SyncErrorsCount":     int64(1),
				"LastRateSourceDate":  "2026-11-08 08:05:00",
				"LastRateCollectedAt": "2026-11-08 08:06:00",
				"LastRequestAt":       "2026-11-08 08:07:00",
				"LastSyncErrorAt":     "2026-11-08 08:08:00",
			},
			"LatestRates": []store.Rate{
				{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:05:00", CollectedAt: "2026-11-08 08:06:00"},
			},
			"RecentRates": []store.Rate{
				{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:05:00", SourceTS: 2, CollectedAt: "2026-11-08 08:06:00"},
			},
			"Requests": []store.APIRequestRecord{
				{KeyName: "bot", Method: "GET", Path: "/v1/rates", StatusCode: 200, DurationMS: 5, RequestBytes: 10, ResponseBytes: 20, RemoteIP: "127.0.0.1", CreatedAt: "2026-11-08 08:07:00"},
			},
			"AuthFailures":       []store.AuthFailureRecord{},
			"SyncErrors":         []store.SyncError{{CreatedAt: "2026-11-08 08:08:00", Message: "fetch eltoque rates", Detail: "timeout"}},
			"Currencies":         []string{"USD"},
			"FilterKeys":         []store.APIKey{{ID: 1, Name: "bot"}},
			"Limit":              50,
			"Currency":           "USD",
			"KeyID":              int64(0),
			"ElToqueBaseURL":     "https://tasas.eltoque.com/v1/trmi",
			"FetchInterval":      5 * time.Minute,
			"SampleSize":         25,
			"CollectorAvailable": true,
		},
	}

	recorder := httptest.NewRecorder()
	if err := templates.Render(recorder, 200, "diagnostics.html", data); err != nil {
		t.Fatalf("render diagnostics: %v", err)
	}

	body := recorder.Body.String()
	for _, want := range []string{"Diagnóstico", "Estado de ElToque", "Errores de sincronización", "Peticiones API recientes", "USD", "timeout"} {
		if !strings.Contains(body, want) {
			t.Fatalf("diagnostics body missing %q", want)
		}
	}
}
