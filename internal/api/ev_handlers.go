package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func (s *Server) evAvailable(w http.ResponseWriter) bool {
	if s.ev == nil || s.ev.Charger() == nil {
		writeError(w, http.StatusServiceUnavailable, "ev_unavailable", "No EV charger adapter is configured.", nil)
		return false
	}
	return true
}

func (s *Server) evControlArmed(w http.ResponseWriter) bool {
	armed, _ := s.gates()["ev_control_armed"].(bool)
	if !armed {
		writeError(w, http.StatusConflict, "ev_control_disarmed",
			"Arm EV control (environment and UI gate) before changing the charger.", nil)
		return false
	}
	return true
}

func (s *Server) evPayload(ctx context.Context) (map[string]any, error) {
	status, err := s.ev.Status(ctx)
	if err != nil {
		return nil, err
	}
	preview, _ := s.ev.LatestPreview()
	return map[string]any{
		"id": s.ev.ID(), "type": s.ev.Charger().Type(),
		"capabilities": s.ev.Charger().Capabilities(), "status": status,
		"config": s.ev.Config(), "preview": preview,
	}, nil
}

func (s *Server) handleChargers(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	item, err := s.evPayload(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "ev_status_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chargers": []any{item}})
}

func (s *Server) handleCharger(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	if id := r.PathValue("id"); id != "" && id != s.ev.ID() {
		writeError(w, http.StatusNotFound, "charger_not_found", "Unknown charger.", nil)
		return
	}
	item, err := s.evPayload(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "ev_status_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleChargerStatus(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	status, err := s.ev.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "ev_status_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleChargerConfig(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	var payload map[string]any
	if err := decodeJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	config, err := s.ev.PatchConfig(r.Context(), raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_config", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleChargerMode(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) || !s.evControlArmed(w) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	status, err := s.ev.SetMode(r.Context(), req.Mode)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_mode", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleChargerCurrent(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) || !s.evControlArmed(w) {
		return
	}
	var req struct {
		Amps float64 `json:"amps"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	status, err := s.ev.SetCurrent(r.Context(), req.Amps)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_current", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleChargerPause(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) || !s.evControlArmed(w) {
		return
	}
	status, err := s.ev.Pause(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "pause_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleChargerResume(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) || !s.evControlArmed(w) {
		return
	}
	status, err := s.ev.Resume(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "resume_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleChargerPlan(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	plans, err := s.ev.Plans(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ev_plan_failed", err.Error(), nil)
		return
	}
	preview, ok := s.ev.LatestPreview()
	session, _ := s.ev.Session(r.Context())
	out := map[string]any{"plans": plans, "session": session, "has_preview": ok}
	if ok {
		out["preview"] = preview
	}
	writeJSON(w, http.StatusOK, out)
}

type evPlanRequest struct {
	TargetKWh       float64 `json:"target_energy_kwh"`
	Deadline        string  `json:"deadline"`
	PreconditionMin int     `json:"precondition_min"`
	Strategy        string  `json:"strategy"`
	Active          *bool   `json:"active"`
	Weekdays        []int   `json:"weekdays"`
}

func (s *Server) planFromRequest(req evPlanRequest) domain.EVPlan {
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	return domain.EVPlan{
		TargetKWh: req.TargetKWh, DeadlineLocal: req.Deadline,
		PreconditionMin: req.PreconditionMin, Strategy: req.Strategy,
		Active: active, Weekdays: req.Weekdays,
	}
}

func (s *Server) handleStaticPlan(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	var req evPlanRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	plan, preview, err := s.ev.SaveStaticPlan(r.Context(), s.planFromRequest(req))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "ev_plan_invalid", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"plan": plan, "preview": preview})
}

func (s *Server) handleDeleteStaticPlan(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	id, err := parseID(r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Plan id is required.", nil)
		return
	}
	if err := s.ev.DeletePlan(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "ev_plan_not_found", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRepeatingPlans(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	var reqs []evPlanRequest
	if err := decodeJSON(w, r, &reqs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	plans := make([]domain.EVPlan, 0, len(reqs))
	for _, req := range reqs {
		plan := s.planFromRequest(req)
		plan.Kind = "repeating"
		plans = append(plans, plan)
	}
	saved, err := s.ev.SaveRepeatingPlans(r.Context(), plans)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "ev_plan_invalid", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": saved})
}

func (s *Server) handlePlanPreview(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	var req evPlanRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	preview, err := s.ev.PreviewPlan(r.Context(), s.planFromRequest(req))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "ev_plan_invalid", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleEVSession(w http.ResponseWriter, r *http.Request) {
	if !s.evAvailable(w) {
		return
	}
	session, err := s.ev.Session(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "ev_session_unavailable", "No EV session yet.", nil)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

// evStatus is the compact dashboard/status payload.
func (s *Server) evStatus(ctx context.Context) map[string]any {
	if s.ev == nil || s.ev.Charger() == nil {
		return map[string]any{"available": false}
	}
	status, err := s.ev.Status(ctx)
	if err != nil {
		return map[string]any{"available": true, "online": false, "error": err.Error()}
	}
	out := map[string]any{"available": true, "status": status, "id": s.ev.ID()}
	if preview, ok := s.ev.LatestPreview(); ok {
		out["plan"] = preview
	}
	return out
}
