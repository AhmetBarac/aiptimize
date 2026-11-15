package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

// windowDriver records the window it was asked to run.
type windowDriver struct {
	calls []struct {
		mode   int
		watts  float64
		window time.Duration
	}
	err error
}

func (w *windowDriver) SetPassive(ctx context.Context, mode int, watts float64) error {
	w.calls = append(w.calls, struct {
		mode   int
		watts  float64
		window time.Duration
	}{mode, watts, 0})
	return w.err
}

func (w *windowDriver) SetPassiveFor(ctx context.Context, mode int, watts float64, window time.Duration) error {
	w.calls = append(w.calls, struct {
		mode   int
		watts  float64
		window time.Duration
	}{mode, watts, window})
	return w.err
}

func newWindowController(plan domain.Plan, driver PassiveDriver, now func() time.Time) *PassiveController {
	values := passiveTestValues(true)
	values.Inverter.MaxChargeW = 10000
	controller := NewPassiveController(
		func() settings.Values { return values },
		func(ctx context.Context) (domain.Plan, error) { return plan, nil },
		func() PassiveDriver { return driver },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		now,
	)
	controller.SetWriteGate(true)
	return controller
}

// A driver with an expiry must be told how long the slot still runs.
func TestPassivePassesRemainingWindow(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-15 * time.Minute), End: now.Add(45 * time.Minute),
			Direction: domain.DirectionCharge, PowerW: 5000,
		}},
	}
	driver := &windowDriver{}
	newWindowController(plan, driver, func() time.Time { return now }).apply(context.Background())

	if len(driver.calls) != 1 {
		t.Fatalf("calls = %+v", driver.calls)
	}
	if driver.calls[0].window != 45*time.Minute {
		t.Fatalf("window = %v, want the remaining 45 minutes", driver.calls[0].window)
	}
}

// An unchanged state is re-sent before the refresh interval lapses, so a
// command with a one-hour expiry never silently stops a three-hour slot.
func TestPassiveRenewsUnchangedState(t *testing.T) {
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: start.Add(-time.Minute), End: start.Add(3 * time.Hour),
			Direction: domain.DirectionCharge, PowerW: 5000,
		}},
	}
	clock := start
	driver := &windowDriver{}
	controller := newWindowController(plan, driver, func() time.Time { return clock })

	controller.apply(context.Background())
	if len(driver.calls) != 1 {
		t.Fatalf("first apply = %+v", driver.calls)
	}

	// A tick a minute later must not rewrite.
	clock = start.Add(time.Minute)
	controller.apply(context.Background())
	if len(driver.calls) != 1 {
		t.Fatalf("rewrote too early: %+v", driver.calls)
	}

	// Past the refresh interval the same state is re-sent.
	clock = start.Add(11 * time.Minute)
	controller.apply(context.Background())
	if len(driver.calls) != 2 {
		t.Fatalf("calls = %d, want a renewal after the refresh interval", len(driver.calls))
	}
	if driver.calls[1].window <= 0 {
		t.Fatalf("renewal window = %v, want the remaining slot", driver.calls[1].window)
	}
}

// Plain drivers keep working and are not asked for a window.
func TestPassiveFallsBackForPlainDriver(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-time.Minute), End: now.Add(time.Hour),
			Direction: domain.DirectionDischarge, PowerW: 2000,
		}},
	}
	plain := &fakeDriver{}
	newWindowController(plan, plain, func() time.Time { return now }).apply(context.Background())

	if len(plain.calls) != 1 || plain.calls[0].mode != passiveDischarge {
		t.Fatalf("calls = %+v", plain.calls)
	}
}

var _ = errors.Is
