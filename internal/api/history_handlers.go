package api

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		writeError(w, http.StatusServiceUnavailable, "history_unavailable", "History is not configured.", nil)
		return
	}
	loc := s.settings.Location()
	parse := func(value string, fallback time.Time) (time.Time, error) {
		if value == "" {
			return fallback, nil
		}
		if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("invalid date %q", value)
	}
	from, err := parse(r.URL.Query().Get("from"), time.Time{})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error(), nil)
		return
	}
	to, err := parse(r.URL.Query().Get("to"), time.Time{})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error(), nil)
		return
	}
	snapshot, err := s.history.Snapshot(r.Context(), from, to, r.URL.Query().Get("resolution"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
