package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/planner"
	"github.com/AhmetBarac/aiptimize/internal/service"
)

func (s *Server) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	plan, err := s.planner.Latest(r.Context())
	if errors.Is(err, service.ErrPlanUnavailable) {
		writeError(w, http.StatusNotFound, "plan_unavailable", "No plan has been computed yet.", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read the plan.", nil)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleRecomputePlan(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeError(w, http.StatusServiceUnavailable, "plan_unavailable", "The planner is not configured.", nil)
		return
	}
	plan, err := s.planner.Compute(r.Context())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSOCUnavailable):
			writeError(w, http.StatusConflict, "soc_stale",
				"Battery SOC is unavailable or older than 5 minutes; keeping the previous plan.", nil)
		case errors.Is(err, service.ErrPricesUnavailable):
			writeError(w, http.StatusServiceUnavailable, "price_stale",
				"No price blocks are available for the planning horizon.", nil)
		case errors.Is(err, planner.ErrInfeasible):
			writeError(w, http.StatusUnprocessableEntity, "plan_infeasible",
				"No schedule satisfies the SOC and slot constraints.", nil)
		default:
			s.log.Error("plan recompute failed", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not compute the plan.", nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// planStatus is the compact plan object embedded in /status.
func (s *Server) planStatus(ctx context.Context) map[string]any {
	if s.planner == nil {
		return map[string]any{"available": false}
	}
	plan, err := s.planner.Latest(ctx)
	if err != nil {
		return map[string]any{"available": false}
	}
	var next *domain.Slot
	now := s.now()
	for i := range plan.Slots {
		if plan.Slots[i].End.After(now) {
			next = &plan.Slots[i]
			break
		}
	}
	return map[string]any{
		"available": true, "id": plan.ID, "created_at": plan.CreatedAt,
		"horizon_start": plan.HorizonStart, "horizon_end": plan.HorizonEnd,
		"status": plan.Status, "summary": plan.Summary, "slots": plan.Slots,
		"next_slot": next, "warnings": plan.Warnings, "no_plan_reason": plan.NoPlanReason,
	}
}
