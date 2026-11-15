package ev

import (
	"context"
	"testing"
	"time"
)

func testSlots(now time.Time, count int) []Slot {
	out := make([]Slot, 0, count)
	for i := 0; i < count; i++ {
		start := now.Add(time.Duration(i) * 15 * time.Minute)
		price := 0.5
		if i%4 == 0 {
			price = 0.1
		}
		out = append(out, Slot{Start: start, End: start.Add(15 * time.Minute), ImportPrice: price, PVSurplusW: 0})
	}
	return out
}

func TestCheapestPlanTrimsToTarget(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	preview := Optimize(PlanInputs{
		Now: now, Deadline: now.Add(8 * time.Hour), TargetKWh: 20,
		PowerW: 11040, MinPowerW: 4140, GridAllowed: true, Strategy: "cheapest",
		Slots: testSlots(now, 32),
	})
	if preview.State != "scheduled" {
		t.Fatalf("state = %s warnings=%v", preview.State, preview.Warnings)
	}
	total := 0.0
	for _, w := range preview.Windows {
		total += w.EnergyKWh
	}
	if total < 19.5 || total > 20.5 {
		t.Errorf("planned energy = %v, want about 20", total)
	}
}

func TestContinuousAndPVOnlyPartial(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	continuous := Optimize(PlanInputs{
		Now: now, Deadline: now.Add(4 * time.Hour), TargetKWh: 10,
		PowerW: 11040, MinPowerW: 4140, GridAllowed: true, Strategy: "continuous",
		Slots: testSlots(now, 16),
	})
	if len(continuous.Windows) != 1 {
		t.Errorf("continuous windows = %+v", continuous.Windows)
	}

	pvSlots := testSlots(now, 8)
	for i := range pvSlots {
		pvSlots[i].PVSurplusW = 5000
		if i > 3 {
			pvSlots[i].PVSurplusW = 0
		}
	}
	partial := Optimize(PlanInputs{
		Now: now, Deadline: now.Add(2 * time.Hour), TargetKWh: 20,
		PowerW: 11040, MinPowerW: 4140, GridAllowed: false, Strategy: "cheapest",
		Slots: pvSlots,
	})
	if partial.State != "unreachable" {
		t.Errorf("state = %s", partial.State)
	}
	if len(partial.Windows) == 0 {
		t.Error("expected a partial PV plan")
	}
}

func TestTargetReachedAndExpired(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	reached := Optimize(PlanInputs{Now: now, Deadline: now.Add(time.Hour), TargetKWh: 10, SessionKWh: 12, PowerW: 10000})
	if reached.State != "target_reached" {
		t.Errorf("reached state = %s", reached.State)
	}
	expired := Optimize(PlanInputs{Now: now, Deadline: now.Add(-time.Hour), TargetKWh: 10, PowerW: 10000})
	if expired.State != "expired" {
		t.Errorf("expired state = %s", expired.State)
	}
}

func TestSimulatorStatusAndControl(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	clock := now
	sim := NewSimulator(SimulatorConfig{Now: func() time.Time { return clock }, Connected: true})
	if err := sim.SetCurrent(context.Background(), 16); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Minute)
	status, err := sim.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Charging || status.SessionKWh <= 0 || status.PowerW <= 0 {
		t.Fatalf("status = %+v", status)
	}
	if err := sim.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, _ = sim.Status(context.Background())
	if status.Charging || status.PowerW != 0 {
		t.Fatalf("paused status = %+v", status)
	}
}

func TestOptimizeKeepsRunningSlot(t *testing.T) {
	// A recompute that happens a few minutes into a charging slot must keep that
	// slot, clamped to now, instead of dropping it and stranding the controller.
	slotStart := time.Date(2026, 11, 15, 9, 30, 0, 0, time.UTC)
	now := slotStart.Add(7 * time.Minute)
	slots := testSlots(slotStart, 8)

	preview := Optimize(PlanInputs{
		Now: now, Deadline: now.Add(4 * time.Hour), TargetKWh: 10,
		PowerW: 11040, MinPowerW: 4140, GridAllowed: true, Strategy: "cheapest",
		Slots: slots,
	})

	if len(preview.Windows) == 0 {
		t.Fatal("running slot was dropped from the plan")
	}
	first := preview.Windows[0]
	if first.Start.After(now) {
		t.Fatalf("window starts at %s, after now %s", first.Start, now)
	}
	if !first.End.After(now) {
		t.Fatalf("window ends at %s, before now %s", first.End, now)
	}
	if first.Start.Before(slotStart) {
		t.Fatalf("window start %s precedes the slot", first.Start)
	}
	// The elapsed part of the slot is not counted as deliverable energy.
	full := 11040.0 * 0.25 / 1000
	if first.EnergyKWh >= full*float64(len(preview.Windows)) {
		t.Fatalf("energy %v suggests the elapsed minutes were counted", first.EnergyKWh)
	}
}

func TestOptimizePinsRunningSlotEvenWhenPricier(t *testing.T) {
	// The cheapest strategy would happily skip the slot it is already charging
	// in, which is what used to stop the car a minute after it started.
	slotStart := time.Date(2026, 11, 15, 9, 30, 0, 0, time.UTC)
	now := slotStart.Add(5 * time.Minute)
	slots := make([]Slot, 0, 8)
	for i := 0; i < 8; i++ {
		start := slotStart.Add(time.Duration(i) * 15 * time.Minute)
		price := 0.1
		if i == 0 {
			price = 5.0 // the running slot is by far the most expensive
		}
		slots = append(slots, Slot{
			Start: start, End: start.Add(15 * time.Minute), ImportPrice: price,
		})
	}

	preview := Optimize(PlanInputs{
		Now: now, Deadline: now.Add(2 * time.Hour), TargetKWh: 2,
		PowerW: 11040, MinPowerW: 4140, GridAllowed: true, Strategy: "cheapest",
		Slots: slots,
	})

	covered := false
	for _, window := range preview.Windows {
		if !window.Start.After(now) && window.End.After(now) {
			covered = true
			break
		}
	}
	if !covered {
		t.Fatalf("running slot was not pinned, windows = %+v", preview.Windows)
	}
}
