package api

import (
	"net/http"
)

func (s *Server) handleBatteryMode(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "battery_unavailable", "Battery telemetry is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed", "Arm inverter writes before changing the battery mode.", nil)
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	mode := 0
	switch req.Mode {
	case "self_consumption":
		mode = 2
	case "reserve":
		mode = 3
	case "custom":
		mode = 4
	case "off_grid":
		mode = 1
	default:
		writeError(w, http.StatusUnprocessableEntity, "invalid_mode", "Mode must be self_consumption, reserve, custom or off_grid.", nil)
		return
	}
	if err := s.telemetry.SetWorkMode(r.Context(), mode); err != nil {
		writeError(w, http.StatusBadGateway, "battery_mode_failed", err.Error(), nil)
		return
	}
	if _, err := s.telemetry.PollOnce(r.Context()); err != nil {
		s.log.Warn("battery mode telemetry refresh failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": req.Mode, "inverter_mode": mode})
}

func (s *Server) handleBatteryForce(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed", "Arm inverter writes before forcing a battery window.", nil)
		return
	}
	var req struct {
		Direction string  `json:"direction"`
		PowerW    float64 `json:"power_w"`
		Duration  int     `json:"duration_min"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	result, err := s.planner.ForcedSchedule(r.Context(), req.Direction, req.PowerW, req.Duration)
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
