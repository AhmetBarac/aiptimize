package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

func TestPlannerRequiresFreshSOCAndPrices(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "planner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	priceSvc := prices.New(st, cfg, nil, log)
	fake := &fakeInverter{reading: inverter.Reading{At: time.Now(), SOC: 50}}
	poller := NewTelemetryPoller(cfg, st, log, nil, TelemetryOptions{
		NewReader: func(settings.Values) (inverter.Reader, error) { return fake, nil },
	})
	plannerSvc := NewPlannerService(st, cfg, priceSvc, nil, poller, log, nil)

	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrSOCUnavailable) {
		t.Fatalf("without telemetry = %v", err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	plannerSvc.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrSOCUnavailable) {
		t.Fatalf("stale telemetry = %v", err)
	}
	plannerSvc.now = time.Now
	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrPricesUnavailable) {
		t.Fatalf("without prices = %v", err)
	}
}

func TestAutoApplyWritesOnlyChangedSchedule(t *testing.T) {
	ctx := context.Background()
	poller, fake, cfg, st, _ := newTelemetryTest(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, poller, log, nil)
	plannerSvc.SetWriteGate(true)
	if _, err := cfg.Patch(ctx, settings.SectionSafety, []byte(`{"writes_armed_ui":true,"auto_schedule_write":"when_changed"}`)); err != nil {
		t.Fatal(err)
	}
	loc := cfg.Location()
	now := time.Now().In(loc)
	start := now.Truncate(30 * time.Minute).Add(30 * time.Minute)
	day := start.Format("2006-01-02")
	plan, err := st.SavePlan(ctx, domain.Plan{
		CreatedAt: now.UTC(), HorizonStart: start.UTC(), HorizonEnd: start.Add(30 * time.Minute).UTC(),
		Timezone: loc.String(), Status: domain.PlanStatusSuggestion, Origin: domain.OriginAuto, Revision: 1,
		Summary: domain.PlanSummary{Currency: "SEK"},
		Slots: []domain.Slot{{
			DayLocal: day, Start: start.UTC(), End: start.Add(30 * time.Minute).UTC(),
			Direction: domain.DirectionCharge, PowerW: 1000, EnergyKWh: 0.5,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plannerSvc.autoApplyIfChanged(ctx, plan)
	schedule, err := fake.Schedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slots := 0
	for _, entries := range schedule.Days {
		slots += len(entries)
	}
	if slots == 0 {
		t.Fatalf("schedule was not written: %+v", schedule)
	}
	// A second identical pass must not write again.
	plannerSvc.autoApplyIfChanged(ctx, plan)
	again, err := fake.Schedule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ScheduleEqual(schedule, again) {
		t.Fatalf("schedule changed on an identical pass: %+v", again)
	}
}

func TestPlannerLatestUnavailable(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "planner-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, _ := settings.New(ctx, st)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, nil, log, nil)
	if _, err := plannerSvc.Latest(ctx); !errors.Is(err, ErrPlanUnavailable) {
		t.Fatalf("latest = %v", err)
	}
}
