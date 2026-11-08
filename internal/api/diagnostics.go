package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LeivSuaxy/toque-api-provider/internal/auth"
	"github.com/LeivSuaxy/toque-api-provider/internal/store"
)

const (
	diagnosticsMinLimit     = 10
	diagnosticsDefaultLimit = 50
	diagnosticsMaxLimit     = 200
)

type diagnosticsView struct {
	Summary            store.DiagnosticsSummary
	LatestRates        []store.Rate
	RecentRates        []store.Rate
	Requests           []store.APIRequestRecord
	AuthFailures       []store.AuthFailureRecord
	SyncErrors         []store.SyncError
	Currencies         []string
	FilterKeys         []store.APIKey
	Limit              int
	Currency           string
	KeyID              int64
	ElToqueBaseURL     string
	FetchInterval      time.Duration
	SampleSize         int
	CollectorAvailable bool
}

func (h *adminHandlers) diagnostics(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.SessionFromRequest(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	w.Header().Set("Cache-Control", "no-store")

	limit := parseDiagnosticsLimit(r.URL.Query().Get("limit"))
	currency := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("currency")))
	keyID := parseKeyFilter(r.URL.Query().Get("key"))

	view, err := h.loadDiagnostics(r.Context(), currency, keyID, limit)
	if err != nil {
		h.logger.Error("load diagnostics", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	view.ElToqueBaseURL = h.cfg.ElToqueBaseURL
	view.FetchInterval = h.cfg.FetchInterval
	view.SampleSize = h.cfg.SampleSize
	view.CollectorAvailable = h.collector != nil

	h.render(w, http.StatusOK, "diagnostics.html", pageData{
		Title:       "Diagnóstico",
		Username:    session.Username,
		CSRF:        session.CSRF,
		Error:       r.URL.Query().Get("error"),
		OK:          r.URL.Query().Get("ok"),
		Diagnostics: &view,
	})
}

func (h *adminHandlers) collectNow(w http.ResponseWriter, r *http.Request) {
	if !h.form(w, r) {
		return
	}

	if h.collector == nil {
		redirectDiagnostics(w, r, "error", "El collector no está disponible.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	if err := h.collector.RunOnce(ctx); err != nil {
		h.logger.Error("manual collect", "err", err)
		redirectDiagnostics(w, r, "error", "Error al recolectar: "+err.Error())
		return
	}

	redirectDiagnostics(w, r, "ok", "Recolección ejecutada correctamente.")
}

func (h *adminHandlers) loadDiagnostics(ctx context.Context, currency string, keyID int64, limit int) (diagnosticsView, error) {
	var view diagnosticsView

	var err error
	if view.Summary, err = h.store.DiagnosticsSummary(ctx); err != nil {
		return view, err
	}
	if view.LatestRates, err = h.store.LatestRates(ctx); err != nil {
		return view, err
	}
	if view.RecentRates, err = h.store.RecentRatesFiltered(ctx, currency, limit); err != nil {
		return view, err
	}
	if view.Requests, err = h.store.RecentAPIRequests(ctx, keyID, limit); err != nil {
		return view, err
	}
	if view.AuthFailures, err = h.store.RecentAuthFailures(ctx, limit); err != nil {
		return view, err
	}
	if view.SyncErrors, err = h.store.RecentSyncErrors(ctx, limit); err != nil {
		return view, err
	}
	if view.Currencies, err = h.store.CurrencyCodes(ctx); err != nil {
		return view, err
	}
	if view.FilterKeys, err = h.store.ListAPIKeys(ctx); err != nil {
		return view, err
	}

	view.Limit = limit
	view.Currency = currency
	view.KeyID = keyID
	return view, nil
}

func redirectDiagnostics(w http.ResponseWriter, r *http.Request, param, message string) {
	http.Redirect(w, r, "/admin/diagnostics?"+param+"="+url.QueryEscape(message), http.StatusSeeOther)
}

func parseDiagnosticsLimit(raw string) int {
	if raw == "" {
		return diagnosticsDefaultLimit
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < diagnosticsMinLimit || value > diagnosticsMaxLimit {
		return diagnosticsDefaultLimit
	}
	return value
}

func parseKeyFilter(raw string) int64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
