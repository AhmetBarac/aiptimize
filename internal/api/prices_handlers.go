package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/prices"
)

// priceStatus summarizes the current price situation for /status and SSE.
func (s *Server) priceStatus(ctx context.Context) map[string]any {
	out := map[string]any{
		"available": false,
		"points":    0,
		"blocks":    0,
		"state":     domain.PriceStateMissing,
	}
	if s.prices == nil {
		return out
	}
	loc := s.settings.Location()
	now := s.now()
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	snap, err := s.prices.Snapshot(queryCtx, today, today.AddDate(0, 0, 1))
	if err != nil {
		out["error"] = "price_unavailable"
		return out
	}
	out["available"] = len(snap.Points) > 0
	out["points"] = len(snap.Points)
	out["blocks"] = len(snap.Blocks)
	out["state"] = snap.State
	out["area"] = snap.Area
	out["currency"] = snap.Currency
	out["stale"] = snap.Stale
	if snap.FetchedAt != nil {
		out["updated_at"] = *snap.FetchedAt
	}
	for _, b := range snap.Blocks {
		if !b.Start.After(now) && b.End.After(now) {
			out["now_import"] = b.ImportPrice
			out["now_spot"] = b.PriceKWh
			out["now_state"] = b.State
			break
		}
	}
	for _, b := range snap.Blocks {
		if b.Start.After(now) {
			out["next_import"] = b.ImportPrice
			out["next_start"] = b.Start
			break
		}
	}
	if len(snap.Summary.Cheapest) > 0 {
		out["cheapest_window"] = snap.Summary.Cheapest[0]
	}
	return out
}

func (s *Server) handlePrices(w http.ResponseWriter, r *http.Request) {
	if s.prices == nil {
		writeError(w, http.StatusServiceUnavailable, "price_unavailable", "Price ingestion is not configured.", nil)
		return
	}
	from, to, err := s.priceRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	snap, err := s.prices.Snapshot(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read prices.", nil)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// priceRange resolves ?date=, ?from=&to= or the default two-day horizon.
func (s *Server) priceRange(r *http.Request) (time.Time, time.Time, error) {
	loc := s.settings.Location()
	now := s.now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	q := r.URL.Query()

	if raw := q.Get("date"); raw != "" {
		day, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("date must be YYYY-MM-DD")
		}
		return day, day.AddDate(0, 0, 1), nil
	}

	fromRaw, toRaw := q.Get("from"), q.Get("to")
	if fromRaw == "" && toRaw == "" {
		return today, today.AddDate(0, 0, 2), nil
	}
	var from, to time.Time
	var err error
	if fromRaw != "" {
		from, err = time.Parse(time.RFC3339, fromRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from must be RFC3339")
		}
	}
	if toRaw != "" {
		to, err = time.Parse(time.RFC3339, toRaw)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be RFC3339")
		}
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = from.Add(24 * time.Hour)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to must be after from")
	}
	if to.Sub(from) > 7*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("range must be at most 7 days")
	}
	return from, to, nil
}

type refreshPricesRequest struct {
	Date string `json:"date"`
}

type refreshResult struct {
	Date   string            `json:"date"`
	OK     bool              `json:"ok"`
	Points int               `json:"points"`
	State  domain.PriceState `json:"state"`
	Error  string            `json:"error,omitempty"`
	Code   string            `json:"code,omitempty"`
}

func (s *Server) handleRefreshPrices(w http.ResponseWriter, r *http.Request) {
	if s.prices == nil {
		writeError(w, http.StatusServiceUnavailable, "price_unavailable", "Price ingestion is not configured.", nil)
		return
	}
	var req refreshPricesRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}

	loc := s.settings.Location()
	now := s.now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	days := []time.Time{today, today.AddDate(0, 0, 1)}
	if req.Date != "" {
		day, err := time.ParseInLocation("2006-01-02", req.Date, loc)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "date must be YYYY-MM-DD", nil)
			return
		}
		days = []time.Time{day}
	}

	refreshCtx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	results := make([]refreshResult, 0, len(days))
	anyOK := false
	anyHardFailure := false
	for _, date := range days {
		fetched, err := s.prices.Refresh(refreshCtx, date)
		result := refreshResult{Date: date.Format("2006-01-02")}
		if err != nil {
			result.Error = err.Error()
			switch {
			case errors.Is(err, prices.ErrNotPublished):
				result.Code = "price_not_published"
			case errors.Is(err, prices.ErrNoData):
				result.Code = "price_missing"
			default:
				result.Code = "price_refresh_failed"
				anyHardFailure = true
			}
			results = append(results, result)
			continue
		}
		anyOK = true
		result.OK = true
		result.Date = fetched.Date
		result.Points = len(fetched.Points)
		result.State = fetched.State
		results = append(results, result)
		s.broker.Publish("prices", map[string]any{
			"date": fetched.Date, "area": fetched.Area, "points": len(fetched.Points), "state": fetched.State,
		})
	}

	status := http.StatusOK
	if !anyOK && anyHardFailure {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"results": results})
}
