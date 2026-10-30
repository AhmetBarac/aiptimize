package service

import (
	"context"
	"math"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// RunSlotPower keeps the inverter's global Pin/Pout aligned with the plan slot
// that is currently active. The inverter schedule itself is left untouched;
// only a setpoint change is written, and only when the seven-day structure
// already matches.
func (s *PlannerService) RunSlotPower(ctx context.Context) {
	// Let telemetry and the initial plan settle before evaluating a slot.
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := s.SyncSlotPower(ctx); err != nil {
				s.log.Debug("slot power update skipped", "err", err)
			}
			timer.Reset(15 * time.Second)
		}
	}
}

// SyncSlotPower performs one slot-power synchronization pass.
func (s *PlannerService) SyncSlotPower(ctx context.Context) error {
	if !s.writesEnv || !s.cfg.Get().Safety.WritesArmedUI {
		return nil
	}
	loc := s.cfg.Location()
	now := s.now().In(loc)
	day := now.Format("2006-01-02")

	manual, manualErr := s.st.LatestAppliedForDay(ctx, day)
	appliedAuto, autoErr := s.st.LatestPlanByStatus(ctx, domain.PlanStatusApplied)
	var plan *domain.Plan
	var manualForSchedule *domain.Plan
	var autoForSchedule *domain.Plan
	if manualErr == nil {
		plan = &manual
		manualForSchedule = &manual
		if suggestion, err := s.st.LatestPlanByStatus(ctx, domain.PlanStatusSuggestion); err == nil {
			autoForSchedule = &suggestion
		}
	} else if autoErr == nil {
		plan = &appliedAuto
		autoForSchedule = &appliedAuto
	}
	if plan == nil {
		return nil
	}

	var active *domain.Slot
	for i := range plan.Slots {
		slot := &plan.Slots[i]
		if !now.Before(slot.Start) && now.Before(slot.End) {
			active = slot
			break
		}
	}
	if active == nil || active.PowerW <= 0 {
		return nil
	}

	values := s.cfg.Get()
	desired, err := BuildSchedule(values, day, manualForSchedule, autoForSchedule)
	if err != nil {
		return err
	}
	if active.Direction == domain.DirectionCharge {
		desired.PinW = math.Round(active.PowerW)
	} else {
		desired.PoutW = math.Round(active.PowerW)
	}
	current, err := s.telemetry.ReadSchedule(ctx)
	if err != nil {
		return err
	}
	diff := ScheduleDiff(desired, current)
	if len(diff) != 0 {
		// Never overwrite a schedule that differs structurally. Per-slot power
		// is only a setpoint adjustment on top of an already matching schedule.
		return nil
	}
	if desired.PinW == current.PinW && desired.PoutW == current.PoutW {
		return nil
	}
	if _, err := s.writeSchedule(ctx, desired, plan, plan.Revision, "slot_power", false); err != nil {
		return err
	}
	s.log.Info("slot power applied", "slot_start", active.Start.In(loc).Format(time.RFC3339),
		"direction", active.Direction, "power_w", active.PowerW)
	return nil
}
