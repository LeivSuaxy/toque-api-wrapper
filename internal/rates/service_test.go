package rates

import (
	"context"
	"errors"
	"testing"

	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

type fakeStore struct {
	recent map[string][]store.Rate
	latest map[string]store.Rate
	codes  []string
	err    error
}

func (f fakeStore) RecentRates(_ context.Context, code string, limit int) ([]store.Rate, error) {
	if f.err != nil {
		return nil, f.err
	}
	rates := f.recent[code]
	return rates, nil
}

func (f fakeStore) LatestRate(_ context.Context, code string) (store.Rate, error) {
	if f.err != nil {
		return store.Rate{}, f.err
	}
	rate, ok := f.latest[code]
	if !ok {
		return store.Rate{}, store.ErrNotFound
	}
	return rate, nil
}

func (f fakeStore) CurrencyCodes(_ context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.codes, nil
}

func TestGetMedian(t *testing.T) {
	service := NewService(fakeStore{
		recent: map[string][]store.Rate{
			"USD": {
				{CurrencyCode: "USD", Rate: 30, SourceDate: "2026-11-08 08:45:00"},
				{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:40:00"},
				{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:35:00"},
			},
		},
	}, 3)

	result, err := service.Get(context.Background(), "USD", true)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if result.Rate != 20 {
		t.Fatalf("rate = %v, want 20", result.Rate)
	}
	if result.RecordsUsed != 3 {
		t.Fatalf("records used = %d, want 3", result.RecordsUsed)
	}
	if result.Min != 10 || result.Max != 30 || result.LatestRate != 30 {
		t.Fatalf("unexpected stats: %+v", result)
	}
	if result.SourceDate != "2026-11-08 08:45:00" {
		t.Fatalf("source date = %q", result.SourceDate)
	}
}

func TestGetEvenMedian(t *testing.T) {
	service := NewService(fakeStore{
		recent: map[string][]store.Rate{
			"USD": {
				{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:45:00"},
				{CurrencyCode: "USD", Rate: 20, SourceDate: "2026-11-08 08:40:00"},
			},
		},
	}, 25)

	result, err := service.Get(context.Background(), "USD", true)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result.Rate != 15 {
		t.Fatalf("rate = %v, want 15", result.Rate)
	}
}

func TestGetLatest(t *testing.T) {
	service := NewService(fakeStore{
		latest: map[string]store.Rate{
			"USD": {CurrencyCode: "USD", Rate: 930, SourceDate: "2026-11-08 08:45:00"},
		},
	}, 25)

	result, err := service.Get(context.Background(), "USD", false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result.Rate != 930 || result.RecordsUsed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGetMissingCurrency(t *testing.T) {
	service := NewService(fakeStore{}, 25)

	_, err := service.Get(context.Background(), "USD", true)
	if !errors.Is(err, ErrCurrencyNotFound) {
		t.Fatalf("err = %v, want ErrCurrencyNotFound", err)
	}

	_, err = service.Get(context.Background(), "USD", false)
	if !errors.Is(err, ErrCurrencyNotFound) {
		t.Fatalf("err = %v, want ErrCurrencyNotFound", err)
	}
}

func TestGetAll(t *testing.T) {
	service := NewService(fakeStore{
		recent: map[string][]store.Rate{
			"USD": {{CurrencyCode: "USD", Rate: 10, SourceDate: "2026-11-08 08:45:00"}},
			"ECU": {{CurrencyCode: "ECU", Rate: 20, SourceDate: "2026-11-08 08:45:00"}},
		},
		codes: []string{"USD", "ECU"},
	}, 25)

	results, err := service.GetAll(context.Background(), true)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}
}

func TestGetAllNoData(t *testing.T) {
	service := NewService(fakeStore{}, 25)

	_, err := service.GetAll(context.Background(), true)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("err = %v, want ErrNoData", err)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		want   float64
	}{
		{name: "empty", values: nil, want: 0},
		{name: "odd", values: []float64{3, 1, 2}, want: 2},
		{name: "even", values: []float64{4, 1, 3, 2}, want: 2.5},
		{name: "single", values: []float64{7}, want: 7},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := median(test.values); got != test.want {
				t.Fatalf("median = %v, want %v", got, test.want)
			}
		})
	}
}
