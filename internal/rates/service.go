package rates

import (
	"context"
	"errors"
	"math"
	"sort"

	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

var (
	ErrNoData           = errors.New("rates: no data")
	ErrCurrencyNotFound = errors.New("rates: currency not found")
)

type RateStore interface {
	RecentRates(ctx context.Context, currencyCode string, limit int) ([]store.Rate, error)
	LatestRate(ctx context.Context, currencyCode string) (store.Rate, error)
	CurrencyCodes(ctx context.Context) ([]string, error)
}

type Service struct {
	store      RateStore
	sampleSize int
}

type Result struct {
	CurrencyCode string  `json:"currency_code"`
	Rate         float64 `json:"rate"`
	RecordsUsed  int     `json:"records_used"`
	SourceDate   string  `json:"source_date"`
	Min          float64 `json:"min"`
	Max          float64 `json:"max"`
	LatestRate   float64 `json:"latest_rate"`
}

func NewService(store RateStore, sampleSize int) *Service {
	return &Service{store: store, sampleSize: sampleSize}
}

func (s *Service) SampleSize() int {
	return s.sampleSize
}

func (s *Service) Get(ctx context.Context, currencyCode string, fixed bool) (Result, error) {
	if !fixed {
		return s.latest(ctx, currencyCode)
	}

	recent, err := s.store.RecentRates(ctx, currencyCode, s.sampleSize)
	if err != nil {
		return Result{}, err
	}
	if len(recent) == 0 {
		return Result{}, ErrCurrencyNotFound
	}

	values := make([]float64, len(recent))
	minValue := recent[0].Rate
	maxValue := recent[0].Rate
	for i, rate := range recent {
		values[i] = rate.Rate
		if rate.Rate < minValue {
			minValue = rate.Rate
		}
		if rate.Rate > maxValue {
			maxValue = rate.Rate
		}
	}

	return Result{
		CurrencyCode: currencyCode,
		Rate:         round2(median(values)),
		RecordsUsed:  len(recent),
		SourceDate:   recent[0].SourceDate,
		Min:          round2(minValue),
		Max:          round2(maxValue),
		LatestRate:   round2(recent[0].Rate),
	}, nil
}

func (s *Service) latest(ctx context.Context, currencyCode string) (Result, error) {
	latest, err := s.store.LatestRate(ctx, currencyCode)
	if errors.Is(err, store.ErrNotFound) {
		return Result{}, ErrCurrencyNotFound
	}
	if err != nil {
		return Result{}, err
	}

	return Result{
		CurrencyCode: currencyCode,
		Rate:         round2(latest.Rate),
		RecordsUsed:  1,
		SourceDate:   latest.SourceDate,
		Min:          round2(latest.Rate),
		Max:          round2(latest.Rate),
		LatestRate:   round2(latest.Rate),
	}, nil
}

func (s *Service) GetAll(ctx context.Context, fixed bool) ([]Result, error) {
	codes, err := s.store.CurrencyCodes(ctx)
	if err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, ErrNoData
	}

	results := make([]Result, 0, len(codes))
	for _, code := range codes {
		result, err := s.Get(ctx, code, fixed)
		if errors.Is(err, ErrCurrencyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		return nil, ErrNoData
	}
	return results, nil
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
