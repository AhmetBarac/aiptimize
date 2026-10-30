package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/service"
)

type draftSlotRequest struct {
	Start     time.Time        `json:"start"`
	End       time.Time        `json:"end"`
	Direction domain.Direction `json:"direction"`
	PowerW    float64          `json:"power_w"`
}

type draftRequest struct {
	Date       string             `json:"date"`
	Revision   int                `json:"revision"`
	BasePlanID int64              `json:"base_plan_id"`
	Slots      []draftSlotRequest `json:"slots"`
	Note       string             `json:"note"`
}

func (s *Server) handlePlanView(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	view, err := s.planner.PlanView(r.Context(), r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_date", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	var req draftRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	slots := make([]service.DraftSlot, 0, len(req.Slots))
	for _, slot := range req.Slots {
		slots = append(slots, service.DraftSlot{
			Start: slot.Start, End: slot.End, Direction: slot.Direction, PowerW: slot.PowerW,
		})
	}
	plan, validation, err := s.planner.SaveDraft(r.Context(), req.Date, req.Revision, req.BasePlanID, slots, req.Note, s.actor(r))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrDraftInvalid):
			writeError(w, http.StatusUnprocessableEntity, "draft_invalid", "The draft violates planner constraints.",
				map[string]any{"validation": validation})
		case errors.Is(err, service.ErrRevisionConflict):
			writeError(w, http.StatusConflict, "revision_conflict", "The plan was changed in another tab.", nil)
		case errors.Is(err, service.ErrSOCUnavailable):
			writeError(w, http.StatusConflict, "soc_stale", "Battery SOC is unavailable or older than 5 minutes.", nil)
		case errors.Is(err, service.ErrPricesUnavailable):
			writeError(w, http.StatusServiceUnavailable, "price_stale", "No prices are available.", nil)
		default:
			s.log.Warn("save draft failed", "err", err)
			writeError(w, http.StatusBadRequest, "draft_failed", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"plan": plan, "validation": validation})
}

func (s *Server) handlePreviewDraft(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid draft id.", nil)
		return
	}
	plan, result, validation, err := s.planner.PreviewDraft(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "draft_not_found", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": plan, "preview": result, "validation": validation,
	})
}

func (s *Server) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid draft id.", nil)
		return
	}
	if err := s.planner.DiscardDraft(r.Context(), id); err != nil {
		writeError(w, http.StatusConflict, "draft_discard_failed", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type applyRequest struct {
	Confirm  bool `json:"confirm"`
	Revision int  `json:"revision"`
}

func (s *Server) handleApplyDraft(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed",
			"Arm inverter writes (environment and UI gate) before resubmitting.", nil)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid draft id.", nil)
		return
	}
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required",
			"Resubmitting to the inverter requires confirm: true.", nil)
		return
	}
	result, err := s.planner.ApplyDraft(r.Context(), id, req.Revision, s.actor(r))
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleApplyAuto(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed",
			"Arm inverter writes (environment and UI gate) before applying.", nil)
		return
	}
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required",
			"Applying the automatic plan requires confirm: true.", nil)
		return
	}
	result, err := s.planner.ApplyAuto(r.Context(), r.URL.Query().Get("date"), s.actor(r))
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleDownloadPlan(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed",
			"Arm inverter writes (environment and UI gate) before downloading a plan.", nil)
		return
	}
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required",
			"Downloading the plan to the inverter requires confirm: true.", nil)
		return
	}
	result, err := s.planner.DownloadPlan(r.Context(), r.URL.Query().Get("date"), s.actor(r))
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePlanRevisions(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	revisions, err := s.planner.Revisions(r.Context(), r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, http.StatusNotFound, "revisions_unavailable", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": revisions})
}

func (s *Server) handleScheduleClear(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed", "Arm inverter writes before clearing the schedule.", nil)
		return
	}
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil || !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required", "Clearing requires confirm: true.", nil)
		return
	}
	result, err := s.planner.ClearSchedule(r.Context())
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleScheduleTakeover(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	if !s.writesArmed() {
		writeError(w, http.StatusConflict, "writes_disarmed", "Arm inverter writes before taking control.", nil)
		return
	}
	var req applyRequest
	if err := decodeJSON(w, r, &req); err != nil || !req.Confirm {
		writeError(w, http.StatusUnprocessableEntity, "confirmation_required", "Takeover requires confirm: true.", nil)
		return
	}
	result, err := s.planner.TakeControl(r.Context(), r.URL.Query().Get("date"), s.actor(r))
	if err != nil {
		s.planApplyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) scheduleStatus(ctx context.Context) map[string]any {
	if s.telemetry == nil {
		return map[string]any{"available": false}
	}
	current, err := s.telemetry.ReadSchedule(ctx)
	if err != nil {
		return map[string]any{"available": true, "read_ok": false, "error": err.Error()}
	}
	out := map[string]any{"available": true, "read_ok": true, "schedule": current}
	if view, err := s.planner.PlanView(ctx, ""); err == nil {
		out["matches"] = view.InverterMatch
		out["diff"] = view.ScheduleDiff
		out["foreign_schedule"] = view.Foreign
	}
	return out
}

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if s.telemetry == nil {
		writeError(w, http.StatusServiceUnavailable, "schedule_unavailable", "Telemetry is not configured.", nil)
		return
	}
	current, err := s.telemetry.ReadSchedule(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "schedule_read_failed", err.Error(), nil)
		return
	}
	out := map[string]any{"schedule": current, "matches": nil, "diff": []string{}, "last_write": nil}
	if view, err := s.planner.PlanView(r.Context(), ""); err == nil {
		out["matches"] = view.InverterMatch
		out["diff"] = view.ScheduleDiff
		out["foreign_schedule"] = view.Foreign
		if view.LastWrite != nil {
			out["last_write"] = view.LastWrite
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) planApplyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrForeignSchedule):
		writeError(w, http.StatusConflict, "foreign_schedule",
			"The inverter has a schedule this app did not write; take control first.", nil)
	case errors.Is(err, service.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "revision_conflict", "The plan revision is stale.", nil)
	case errors.Is(err, service.ErrDraftInvalid):
		writeError(w, http.StatusUnprocessableEntity, "draft_invalid", err.Error(), nil)
	case errors.Is(err, service.ErrActiveSlot):
		writeError(w, http.StatusConflict, "active_slot", err.Error(), nil)
	case errors.Is(err, service.ErrSOCUnavailable):
		writeError(w, http.StatusConflict, "soc_stale", "Battery SOC is unavailable or older than 5 minutes.", nil)
	case strings.Contains(err.Error(), "rate limit"):
		writeError(w, http.StatusConflict, "write_rate_limited", err.Error(), nil)
	default:
		s.log.Warn("plan apply failed", "err", err)
		writeError(w, http.StatusBadGateway, "apply_failed", err.Error(), nil)
	}
}

func (s *Server) writesArmed() bool {
	armed, _ := s.gates()["writes_armed"].(bool)
	return armed
}

func (s *Server) actor(r *http.Request) string {
	if a, ok := authFrom(r.Context()); ok && a.User.Username != "" {
		return a.User.Username
	}
	return "admin"
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
