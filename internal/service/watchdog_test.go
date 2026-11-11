package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func watchdogSlotSchedule(day string) domain.Schedule {
	return domain.Schedule{
		Days: map[string][]domain.ScheduleSlot{
			day: {{Hour: 2, Minute: 0, DurationMin: 60, Discharge: false}},
		},
		PinW:  6000,
		PoutW: 3000,
	}
}

func scheduleSlotCount(s domain.Schedule) int {
	total := 0
	for _, slots := range s.Days {
		total += len(slots)
	}
	return total
}

func TestWatchdogClearsOurStaleSchedule(t *testing.T) {
	ctx := context.Background()
	poller, fake, cfg, st, _ := newTelemetryTest(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, poller, log, nil)
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety,
		[]byte(`{"writes_armed_ui":true,"watchdog_clear_after_s":60}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	desired := watchdogSlotSchedule("Mon")
	if _, err := plannerSvc.writeSchedule(ctx, desired, nil, 0, "apply", true); err != nil {
		t.Fatal(err)
	}
	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 1 {
		t.Fatalf("setup wrote %d slots, want 1", got)
	}

	base := poller.now()
	plannerSvc.now = func() time.Time { return base.Add(10 * time.Minute) }
	plannerSvc.CheckWatchdog(ctx)

	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 0 {
		t.Fatalf("watchdog left %d slot(s) on the inverter", got)
	}
	events, err := st.ListEvents(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Code != "watchdog_cleared" || events[0].Severity != "warning" {
		t.Fatalf("events = %+v", events)
	}
}

func TestWatchdogKeepsScheduleWithFreshTelemetry(t *testing.T) {
	ctx := context.Background()
	poller, fake, cfg, st, _ := newTelemetryTest(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, poller, log, nil)
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety,
		[]byte(`{"writes_armed_ui":true,"watchdog_clear_after_s":60}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plannerSvc.writeSchedule(ctx, watchdogSlotSchedule("Mon"), nil, 0, "apply", true); err != nil {
		t.Fatal(err)
	}
	base := poller.now()
	plannerSvc.now = func() time.Time { return base.Add(30 * time.Second) }
	plannerSvc.CheckWatchdog(ctx)
	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 1 {
		t.Fatalf("fresh telemetry cleared the schedule (%d slots left)", got)
	}
}

func TestWatchdogLeavesForeignScheduleAlone(t *testing.T) {
	ctx := context.Background()
	poller, fake, cfg, st, _ := newTelemetryTest(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, poller, log, nil)
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety,
		[]byte(`{"writes_armed_ui":true,"watchdog_clear_after_s":60}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// Written behind the service's back: no schedule_writes row, so it must be
	// treated as foreign and left untouched.
	if _, err := fake.WriteSchedule(ctx, watchdogSlotSchedule("Mon")); err != nil {
		t.Fatal(err)
	}
	base := poller.now()
	plannerSvc.now = func() time.Time { return base.Add(10 * time.Minute) }
	plannerSvc.CheckWatchdog(ctx)
	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 1 {
		t.Fatalf("watchdog overwrote a foreign schedule (%d slots left)", got)
	}
}

func TestWatchdogRequiresArmedGates(t *testing.T) {
	ctx := context.Background()
	poller, fake, cfg, st, _ := newTelemetryTest(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, poller, log, nil)
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety,
		[]byte(`{"writes_armed_ui":true,"watchdog_clear_after_s":60}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plannerSvc.writeSchedule(ctx, watchdogSlotSchedule("Mon"), nil, 0, "apply", true); err != nil {
		t.Fatal(err)
	}
	base := poller.now()
	plannerSvc.now = func() time.Time { return base.Add(10 * time.Minute) }

	// Environment gate closed.
	plannerSvc.SetWriteGate(false)
	plannerSvc.CheckWatchdog(ctx)
	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 1 {
		t.Fatalf("disarmed environment cleared the schedule (%d slots left)", got)
	}

	// UI gate closed.
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety, []byte(`{"writes_armed_ui":false}`)); err != nil {
		t.Fatal(err)
	}
	plannerSvc.CheckWatchdog(ctx)
	if got := scheduleSlotCount(mustSchedule(t, fake)); got != 1 {
		t.Fatalf("disarmed UI cleared the schedule (%d slots left)", got)
	}
}

func mustSchedule(t *testing.T, fake *fakeInverter) domain.Schedule {
	t.Helper()
	schedule, err := fake.Schedule(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}
