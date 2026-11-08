package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/rates"
	"github.com/LeivSuaxy/toque-api-provider/internal/timeutil"
)

const (
	seriesDefaultLimit = 1000
	seriesMaxLimit     = 10000
)

type ratesResponse struct {
	Mode        string         `json:"mode"`
	Fixed       bool           `json:"fixed"`
	SampleSize  int            `json:"sample_size"`
	GeneratedAt string         `json:"generated_at"`
	Data        []rates.Result `json:"data"`
}

type seriesResponse struct {
	Mode        string         `json:"mode"`
	DateFrom    string         `json:"date_from,omitempty"`
	DateTo      string         `json:"date_to,omitempty"`
	Limit       int            `json:"limit"`
	GeneratedAt string         `json:"generated_at"`
	Data        []rates.Series `json:"data"`
}

func (deps Deps) rates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	query := r.URL.Query()
	from, to, hasRange, ok := parseDateRange(w, query.Get("date_from"), query.Get("date_to"))
	if !ok {
		return
	}
	if hasRange {
		deps.ratesSeries(w, r, from, to)
		return
	}

	fixed := true
	if raw := query.Get("fixed"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_fixed", "fixed must be a boolean")
			return
		}
		fixed = value
	}

	codes := parseCurrencyFilters(query["currency"])

	var (
		results []rates.Result
		err     error
	)
	if len(codes) == 0 {
		results, err = deps.Rates.GetAll(r.Context(), fixed)
	} else {
		seen := make(map[string]struct{}, len(codes))
		for _, code := range codes {
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}

			result, getErr := deps.Rates.Get(r.Context(), code, fixed)
			if errors.Is(getErr, rates.ErrCurrencyNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "currency_not_found", "currency not found: "+code)
				return
			}
			if getErr != nil {
				err = getErr
				break
			}
			results = append(results, result)
		}
	}

	if errors.Is(err, rates.ErrNoData) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_data", "no rate data available yet")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "could not read rates")
		return
	}
	if len(results) == 0 {
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_data", "no rate data available yet")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, ratesResponse{
		Mode:        "aggregate",
		Fixed:       fixed,
		SampleSize:  deps.Rates.SampleSize(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Data:        results,
	})
}

func (deps Deps) ratesSeries(w http.ResponseWriter, r *http.Request, from, to *time.Time) {
	query := r.URL.Query()

	limit, ok := parseSeriesLimit(query.Get("limit"))
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be an integer between 1 and 10000")
		return
	}

	options := rates.SeriesOptions{Limit: limit}
	if from != nil {
		start := timeutil.StartOfDay(*from)
		options.From = &start
	}
	if to != nil {
		end := timeutil.EndOfDayExclusive(*to)
		options.To = &end
	}

	codes := parseCurrencyFilters(query["currency"])

	var (
		results []rates.Series
		err     error
	)
	if len(codes) == 0 {
		results, err = deps.Rates.GetAllSeries(r.Context(), options)
	} else {
		seen := make(map[string]struct{}, len(codes))
		for _, code := range codes {
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}

			series, getErr := deps.Rates.GetSeries(r.Context(), code, options)
			if errors.Is(getErr, rates.ErrCurrencyNotFound) {
				httpx.WriteError(w, http.StatusNotFound, "currency_not_found", "currency not found in range: "+code)
				return
			}
			if getErr != nil {
				err = getErr
				break
			}
			results = append(results, series)
		}
	}

	if errors.Is(err, rates.ErrNoData) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_data", "no rate data available for the requested range")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal_error", "could not read rates")
		return
	}
	if len(results) == 0 {
		httpx.WriteError(w, http.StatusServiceUnavailable, "no_data", "no rate data available for the requested range")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, seriesResponse{
		Mode:        "series",
		DateFrom:    formatDate(from),
		DateTo:      formatDate(to),
		Limit:       limit,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Data:        results,
	})
}

func parseDateRange(w http.ResponseWriter, rawFrom, rawTo string) (*time.Time, *time.Time, bool, bool) {
	rawFrom = strings.TrimSpace(rawFrom)
	rawTo = strings.TrimSpace(rawTo)
	hasRange := rawFrom != "" || rawTo != ""

	var from, to *time.Time
	if rawFrom != "" {
		value, err := timeutil.ParseDate(rawFrom)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_date", "date_from must use YYYY-MM-DD")
			return nil, nil, false, false
		}
		from = &value
	}
	if rawTo != "" {
		value, err := timeutil.ParseDate(rawTo)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_date", "date_to must use YYYY-MM-DD")
			return nil, nil, false, false
		}
		to = &value
	}
	if from != nil && to != nil && from.After(*to) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_date_range", "date_from must be before or equal to date_to")
		return nil, nil, false, false
	}

	return from, to, hasRange, true
}

func parseSeriesLimit(raw string) (int, bool) {
	if raw == "" {
		return seriesDefaultLimit, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > seriesMaxLimit {
		return 0, false
	}
	return value, true
}

func formatDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(timeutil.Havana).Format(timeutil.DateLayout)
}

func parseCurrencyFilters(values []string) []string {
	var codes []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.ToUpper(part))
			if part != "" {
				codes = append(codes, part)
			}
		}
	}
	return codes
}
