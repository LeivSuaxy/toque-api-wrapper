package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
	"github.com/LeivSuaxy/toque-api-provider/internal/rates"
)

type ratesResponse struct {
	Fixed       bool           `json:"fixed"`
	SampleSize  int            `json:"sample_size"`
	GeneratedAt string         `json:"generated_at"`
	Data        []rates.Result `json:"data"`
}

func (deps Deps) rates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	fixed := true
	if raw := r.URL.Query().Get("fixed"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_fixed", "fixed must be a boolean")
			return
		}
		fixed = value
	}

	codes := parseCurrencyFilters(r.URL.Query()["currency"])

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
		Fixed:       fixed,
		SampleSize:  deps.Rates.SampleSize(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Data:        results,
	})
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
