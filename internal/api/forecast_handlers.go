package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) forecastStatus() map[string]any {
	out := map[string]any{"available": false, "stale": true}
	if s.forecast == nil {
		return out
	}
	snap, ok := s.forecast.Latest()
	if !ok {
		return out
	}
	out["available"] = true
	out["stale"] = snap.Stale
	out["generated_at"] = snap.GeneratedAt
	out["pv_total_kwh"] = snap.PVTotalKWh
	out["load_total_kwh"] = snap.LoadTotalKWh
	out["source"] = snap.Source
	return out
}

func (s *Server) handleForecast(w http.ResponseWriter, r *http.Request) {
	if s.forecast == nil {
		writeError(w, http.StatusServiceUnavailable, "forecast_unavailable", "Forecasting is not configured.", nil)
		return
	}
	snap, ok := s.forecast.Latest()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "forecast_unavailable", "The first forecast is not ready yet.", nil)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleRefreshForecast(w http.ResponseWriter, r *http.Request) {
	if s.forecast == nil {
		writeError(w, http.StatusServiceUnavailable, "forecast_unavailable", "Forecasting is not configured.", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	snap, err := s.forecast.Refresh(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "forecast_failed", "Could not refresh the forecast.", map[string]any{
			"error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
