package api

import (
	"errors"
	"net/http"

	"github.com/AhmetBarac/aiptimize/internal/service"
)

func (s *Server) handlePlanControl(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	var req struct {
		Locked bool   `json:"locked"`
		Date   string `json:"date"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	view, err := s.planner.SetManualControl(r.Context(), req.Date, req.Locked, s.actor(r))
	if err != nil {
		if errors.Is(err, service.ErrPlanUnavailable) {
			writeError(w, http.StatusConflict, "plan_unavailable", err.Error(), nil)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "manual_control_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
