package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/store"
)

// watchdogInterval is how often the stale-telemetry watchdog evaluates the
// inverter schedule.
const watchdogInterval = 30 * time.Second

// watchdogWarnEvery throttles warnings the watchdog emits while it cannot act
// (unreadable inverter, foreign schedule).
const watchdogWarnEvery = 15 * time.Minute

// RunWatchdog periodically checks that the process still has fresh telemetry
// for the schedule it owns, and clears that schedule when it does not.
func (s *PlannerService) RunWatchdog(ctx context.Context) {
	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.CheckWatchdog(ctx)
		}
	}
}

// CheckWatchdog performs one watchdog pass: when telemetry has been stale for
// longer than safety.watchdog_clear_after_s and this process is the schedule
// owner, the schedule is cleared and an event is recorded. Clearing is a write,
// so it only happens when both gates are armed; a foreign schedule is left
// untouched. It is exported so tests can drive it directly.
func (s *PlannerService) CheckWatchdog(ctx context.Context) {
	values := s.cfg.Get()
	if !s.writesEnv || !values.Safety.WritesArmedUI {
		return
	}
	limit := time.Duration(values.Safety.WatchdogClearAfterS) * time.Second
	if limit <= 0 {
		limit = 30 * time.Minute
	}
	_, receivedAt, ok := s.telemetry.Latest()
	if !ok {
		// Nothing polled yet, so this process cannot own a schedule.
		return
	}
	age := s.now().Sub(receivedAt)
	if age <= limit {
		return
	}
	current, err := s.telemetry.ReadSchedule(ctx)
	if err != nil {
		s.watchdogWarn("watchdog could not read the inverter schedule", "err", err,
			"stale_for", age.Round(time.Second))
		return
	}
	if !hasScheduleSlots(current) {
		return
	}
	if s.foreignSchedule(ctx, current) {
		s.watchdogWarn("watchdog left a foreign schedule untouched",
			"stale_for", age.Round(time.Second))
		return
	}
	result, err := s.ClearSchedule(ctx)
	if err != nil {
		s.watchdogWarn("watchdog could not clear the inverter schedule", "err", err,
			"stale_for", age.Round(time.Second))
		return
	}
	s.wdLastWarn = time.Time{}
	details, _ := json.Marshal(map[string]any{
		"stale_for_s": int(age.Seconds()),
		"limit_s":     int(limit.Seconds()),
		"wrote":       result.Wrote,
	})
	event := store.Event{
		TS:       s.now().UTC(),
		Severity: "warning",
		Code:     "watchdog_cleared",
		Message:  "Telemetry went stale; the inverter schedule was cleared.",
		Details:  details,
	}
	if err := s.st.InsertEvent(ctx, event); err != nil {
		s.log.Warn("watchdog could not record the event", "err", err)
	}
	if s.publish != nil {
		s.publish("event", map[string]any{
			"code": event.Code, "severity": event.Severity,
			"message": event.Message, "details": json.RawMessage(details),
		})
	}
	s.log.Warn("watchdog cleared the inverter schedule",
		"stale_for", age.Round(time.Second), "wrote", result.Wrote)
}

// watchdogWarn logs at most once per watchdogWarnEvery so a persistent
// condition does not flood the log at the poll interval.
func (s *PlannerService) watchdogWarn(msg string, args ...any) {
	now := s.now()
	if !s.wdLastWarn.IsZero() && now.Sub(s.wdLastWarn) < watchdogWarnEvery {
		return
	}
	s.wdLastWarn = now
	s.log.Warn(msg, args...)
}
