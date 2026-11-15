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

type fakeDriver struct {
	calls []struct {
		mode  int
		watts float64
	}
	err error
}

func (f *fakeDriver) SetPassive(ctx context.Context, mode int, watts float64) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, struct {
		mode  int
		watts float64
	}{mode, watts})
	return nil
}

func passiveTestValues(armed bool) settings.Values {
	var values settings.Values
	values.Safety.WritesArmedUI = armed
	values.Inverter.MaxChargeW = 6000
	values.Inverter.MaxDischargeW = 3000
	return values
}

func newPassiveTest(plan domain.Plan, armed bool, driver *fakeDriver, now time.Time) *PassiveController {
	values := passiveTestValues(armed)
	controller := NewPassiveController(
		func() settings.Values { return values },
		func(ctx context.Context) (domain.Plan, error) { return plan, nil },
		func() PassiveDriver { return driver },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func() time.Time { return now },
	)
	controller.SetWriteGate(true)
	return controller
}

func TestPassiveAppliesChargeSlot(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-10 * time.Minute), End: now.Add(20 * time.Minute),
			Direction: domain.DirectionCharge, PowerW: 5000,
		}},
	}
	driver := &fakeDriver{}
	newPassiveTest(plan, true, driver, now).apply(context.Background())

	if len(driver.calls) != 1 || driver.calls[0].mode != passiveCharge || driver.calls[0].watts != 5000 {
		t.Fatalf("calls = %+v, want one charge at 5000 W", driver.calls)
	}
}

func TestPassiveClampsToConfiguredLimit(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-time.Minute), End: now.Add(time.Minute),
			Direction: domain.DirectionDischarge, PowerW: 9000,
		}},
	}
	driver := &fakeDriver{}
	newPassiveTest(plan, true, driver, now).apply(context.Background())

	if len(driver.calls) != 1 || driver.calls[0].mode != passiveDischarge {
		t.Fatalf("calls = %+v, want one discharge", driver.calls)
	}
	if driver.calls[0].watts != 3000 {
		t.Fatalf("watts = %v, want clamped to 3000", driver.calls[0].watts)
	}
}

func TestPassiveStandbyOutsideSlotsAndWithoutPlan(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	driver := &fakeDriver{}
	newPassiveTest(domain.Plan{Status: domain.PlanStatusSuggestion}, true, driver, now).apply(context.Background())

	if len(driver.calls) != 1 || driver.calls[0].mode != passiveStandby {
		t.Fatalf("calls = %+v, want standby", driver.calls)
	}
}

func TestPassiveRespectsArmGate(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-time.Minute), End: now.Add(time.Minute),
			Direction: domain.DirectionCharge, PowerW: 4000,
		}},
	}
	driver := &fakeDriver{}
	controller := newPassiveTest(plan, true, driver, now)
	controller.SetWriteGate(false)
	controller.apply(context.Background())

	if len(driver.calls) != 0 {
		t.Fatalf("calls = %+v, want none while the environment gate is closed", driver.calls)
	}
}

func TestPassiveOnlyWritesOnChange(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		Status: domain.PlanStatusApplied,
		Slots: []domain.Slot{{
			Start: now.Add(-time.Minute), End: now.Add(time.Minute),
			Direction: domain.DirectionCharge, PowerW: 4000,
		}},
	}
	driver := &fakeDriver{}
	controller := newPassiveTest(plan, true, driver, now)
	controller.apply(context.Background())
	controller.apply(context.Background())

	if len(driver.calls) != 1 {
		t.Fatalf("calls = %+v, want a single write for an unchanged state", driver.calls)
	}
}
