package service

import (
	"context"
	"fmt"
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
	if s.evHoldActive() {
		return nil
	}
	loc := s.cfg.Location()
	now := s.now().In(loc)
	day := now.Format("2006-01-02")
	values := s.cfg.Get()

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
	if active == nil {
		if len(s.suppressionSnapshot(day)) > 0 {
			desired, err := BuildSchedule(values, day, manualForSchedule, autoForSchedule)
			if err != nil {
				return err
			}
			if _, err := s.writeSchedule(ctx, desired, plan, plan.Revision, "peak_restore", false); err != nil {
				return err
			}
			s.clearSuppressed(day)
			s.log.Info("peak shaving restored full schedule")
		}
		return nil
	}
	if active.PowerW <= 0 {
		return nil
	}

	values = s.cfg.Get()
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

	suppressed := s.suppressionSnapshot(day)
	desired = removeSuppressedSlots(desired, suppressed)
	if hasSuppressedSlots(current, suppressed) {
		if _, err := s.writeSchedule(ctx, desired, plan, plan.Revision, "peak_shaving", false); err != nil {
			return err
		}
		s.log.Info("peak shaving removed suppressed charge slot", "day", day)
		return nil
	}

	activeKey := scheduleSlotKey(day, active, loc)
	if values.Plan.PeakShavingEnabled && values.Plan.PeakShavingLimitW > 0 &&
		active.Direction == domain.DirectionCharge && s.peakReached(now, values.Plan.PeakShavingLimitW) &&
		!suppressed[activeKey] {
		desired = removeScheduleSlot(desired, day, active, loc)
		if _, err := s.writeSchedule(ctx, desired, plan, plan.Revision, "peak_shaving", false); err != nil {
			return err
		}
		s.markSuppressed(day, activeKey)
		s.log.Info("peak shaving removed active charge slot", "slot_start", active.Start.In(loc).Format(time.RFC3339))
		return nil
	}
	if suppressed[activeKey] {
		return nil
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

func (s *PlannerService) peakReached(now time.Time, limit float64) bool {
	latest, ts, ok := s.telemetry.Latest()
	if !ok {
		return false
	}
	hour := ts.In(now.Location()).Format("2006-01-02T15")
	s.peakMu.Lock()
	defer s.peakMu.Unlock()
	if hour != s.peakHour {
		s.peakHour = hour
		s.peakW = 0
	}
	if latest.GridW > s.peakW {
		s.peakW = latest.GridW
	}
	return s.peakW >= limit
}

func (s *PlannerService) suppressionSnapshot(day string) map[string]bool {
	s.peakMu.Lock()
	defer s.peakMu.Unlock()
	if day != s.peakDay {
		s.peakDay = day
		s.suppressed = map[string]bool{}
	}
	out := make(map[string]bool, len(s.suppressed))
	for key := range s.suppressed {
		out[key] = true
	}
	return out
}

func (s *PlannerService) markSuppressed(day, key string) {
	s.peakMu.Lock()
	defer s.peakMu.Unlock()
	if day != s.peakDay {
		s.peakDay = day
		s.suppressed = map[string]bool{}
	}
	if s.suppressed == nil {
		s.suppressed = map[string]bool{}
	}
	s.suppressed[key] = true
}

func (s *PlannerService) clearSuppressed(day string) {
	s.peakMu.Lock()
	defer s.peakMu.Unlock()
	s.peakDay = day
	s.suppressed = map[string]bool{}
}

func scheduleSlotKey(day string, slot *domain.Slot, loc *time.Location) string {
	start := slot.Start.In(loc)
	return fmt.Sprintf("%s|%02d:%02d|%d", day, start.Hour(), start.Minute(),
		int(slot.End.Sub(slot.Start).Minutes()))
}

func scheduleEntryKey(day string, entry domain.ScheduleSlot) string {
	return fmt.Sprintf("%s|%02d:%02d|%d", day, entry.Hour, entry.Minute, entry.DurationMin)
}

func removeScheduleSlot(schedule domain.Schedule, day string, slot *domain.Slot, loc *time.Location) domain.Schedule {
	key := scheduleSlotKey(day, slot, loc)
	entries := schedule.Days[day]
	for i, entry := range entries {
		if scheduleEntryKey(day, entry) == key {
			schedule.Days[day] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	return schedule
}

func removeSuppressedSlots(schedule domain.Schedule, suppressed map[string]bool) domain.Schedule {
	if len(suppressed) == 0 {
		return schedule
	}
	for day, entries := range schedule.Days {
		kept := entries[:0]
		for _, entry := range entries {
			if !suppressed[scheduleEntryKey(day, entry)] {
				kept = append(kept, entry)
			}
		}
		schedule.Days[day] = kept
	}
	return schedule
}

func hasSuppressedSlots(schedule domain.Schedule, suppressed map[string]bool) bool {
	if len(suppressed) == 0 {
		return false
	}
	for day, entries := range schedule.Days {
		for _, entry := range entries {
			if suppressed[scheduleEntryKey(day, entry)] {
				return true
			}
		}
	}
	return false
}
