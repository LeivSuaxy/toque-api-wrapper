package api

import (
	"net/http"

	"github.com/LeivSuaxy/toque-api-provider/internal/httpx"
)

type healthResponse struct {
	Status          string `json:"status"`
	DB              string `json:"db"`
	Records         int64  `json:"records"`
	LastCollectedAt string `json:"last_collected_at"`
}

func (deps Deps) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	ctx := r.Context()
	if err := deps.Store.Ping(ctx); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "error",
			DB:     "error",
		})
		return
	}

	records, err := deps.Store.CountRates(ctx)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "error",
			DB:     "error",
		})
		return
	}

	lastCollected, err := deps.Store.LastCollectedAt(ctx)
	if err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "error",
			DB:     "error",
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, healthResponse{
		Status:          "ok",
		DB:              "ok",
		Records:         records,
		LastCollectedAt: lastCollected,
	})
}
