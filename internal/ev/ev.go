// Package ev contains the charger adapter contract and the EV charging planner.
package ev

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// Capabilities describes a charger's electrical limits.
type Capabilities struct {
	MinCurrentA    float64       `json:"min_current_a"`
	MaxCurrentA    float64       `json:"max_current_a"`
	Phases         int           `json:"phases"`
	CurrentScope   string        `json:"current_scope"`
	SupportsPause  bool          `json:"supports_pause"`
	SupportsPhases bool          `json:"supports_phases"`
	ControlLatency time.Duration `json:"control_latency"`
}

// Charger is the adapter contract.
type Charger interface {
	ID() string
	Type() string
	Capabilities() Capabilities
	Status(ctx context.Context) (domain.ChargerStatus, error)
	SetCurrent(ctx context.Context, amps float64) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Close() error
}

// SessionStopper is implemented by chargers that can explicitly end an active
// charging session. Provider changes use it before restarting the process.
type SessionStopper interface {
	StopSession(ctx context.Context) error
}

// Slot is one 15-minute planning candidate.
type Slot struct {
	Start       time.Time
	End         time.Time
	ImportPrice float64
	ExportPrice float64
	PVSurplusW  float64
}

// PlanInputs are the exact inputs of one EV planning run.
type PlanInputs struct {
	Now             time.Time
	Deadline        time.Time
	TargetKWh       float64
	SessionKWh      float64
	EnergyOffsetKWh float64
	PowerW          float64
	MinPowerW       float64
	GridAllowed     bool
	Strategy        string
	PreconditionMin int
	Slots           []Slot
}

// Optimize builds a cheapest or continuous charging plan.
func Optimize(in PlanInputs) domain.EVPlanPreview {
	preview := domain.EVPlanPreview{
		ComputedAt: in.Now.UTC(), Deadline: in.Deadline.UTC(), TargetKWh: in.TargetKWh,
		Windows: []domain.EVWindow{}, Warnings: []string{},
	}
	remaining := math.Max(0, in.TargetKWh-(in.SessionKWh-in.EnergyOffsetKWh))
	preview.RemainingKWh = round2(remaining)
	if remaining <= 0 {
		preview.State = domain.EVStateTargetReached
		return preview
	}
	if !in.Deadline.After(in.Now) {
		preview.State = domain.EVStateExpired
		preview.Warnings = append(preview.Warnings, "Target deadline is in the past.")
		return preview
	}
	energyPerSlot := in.PowerW * 0.25 / 1000
	if energyPerSlot <= 0 {
		preview.State = domain.EVStateUnreachable
		preview.Warnings = append(preview.Warnings, "Charging power is zero.")
		return preview
	}
	// A slot that is already delivering energy is pinned into the plan, clamped
	// to now. Dropping it because a recompute ranked it differently would tell
	// the controller that no window is active and stop a charge mid-session.
	var running []Slot
	runningEnergy := 0.0
	eligible := make([]Slot, 0, len(in.Slots))
	for _, slot := range in.Slots {
		if !slot.End.After(in.Now) || !slot.Start.Before(in.Deadline) {
			continue
		}
		if slot.Start.Before(in.Now) {
			clamped := slot
			clamped.Start = in.Now
			running = append(running, clamped)
			runningEnergy += in.PowerW * clamped.End.Sub(clamped.Start).Hours() / 1000
			continue
		}
		if !in.GridAllowed && slot.PVSurplusW < in.MinPowerW {
			continue
		}
		eligible = append(eligible, slot)
	}
	if runningEnergy > remaining {
		runningEnergy = remaining
	}
	required := int(math.Ceil(math.Max(0, remaining-runningEnergy) / energyPerSlot))
	preview.RequiredMin = required*15 + int(math.Round(runningEnergy/in.PowerW*1000*60))
	if len(eligible) == 0 && len(running) == 0 {
		preview.State = domain.EVStateUnreachable
		preview.Warnings = append(preview.Warnings, "No eligible charging slots before the deadline.")
		return preview
	}
	if len(eligible) < required {
		preview.State = domain.EVStateUnreachable
		preview.Warnings = append(preview.Warnings,
			"Not enough time or PV surplus to reach the target; a partial plan is shown.")
		required = len(eligible)
	}
	partial := len(eligible)+len(running) < int(math.Ceil(remaining/energyPerSlot))
	var forced []Slot
	remainingRequired := required
	if in.PreconditionMin > 0 {
		reserveSlots := int(math.Ceil(float64(in.PreconditionMin) / 15))
		reserveStart := in.Deadline.Add(-time.Duration(in.PreconditionMin) * time.Minute)
		for _, slot := range eligible {
			if !slot.Start.Before(reserveStart) {
				forced = append(forced, slot)
			}
		}
		if len(forced) > reserveSlots {
			forced = forced[len(forced)-reserveSlots:]
		}
		remainingRequired = required - len(forced)
		if remainingRequired < 0 {
			remainingRequired = 0
		}
	}
	pool := eligible
	if len(forced) > 0 {
		pool = make([]Slot, 0, len(eligible))
		for _, slot := range eligible {
			if slot.Start.Before(forced[0].Start) {
				pool = append(pool, slot)
			}
		}
	}
	selected := selectSlots(pool, remainingRequired, in.Strategy, in.GridAllowed, in.PowerW)
	selected = append(selected, forced...)
	selected = append(selected, running...)
	sort.Slice(selected, func(i, j int) bool { return selected[i].Start.Before(selected[j].Start) })
	if len(selected) < required {
		partial = true
	}
	if len(selected) == 0 {
		preview.State = domain.EVStateUnreachable
		preview.Warnings = append(preview.Warnings, "No charging window could be selected.")
		return preview
	}
	preview.Windows = mergeWindows(selected, in.PowerW, in.GridAllowed)
	preview.Windows = trimWindows(preview.Windows, remaining)
	if partial {
		preview.State = domain.EVStateUnreachable
	} else {
		preview.State = domain.EVStateScheduled
	}
	for _, window := range preview.Windows {
		preview.Cost += window.Cost
	}
	preview.Cost = round2(preview.Cost)
	return preview
}

func selectSlots(slots []Slot, count int, strategy string, gridAllowed bool, powerW float64) []Slot {
	if count > len(slots) {
		count = len(slots)
	}
	if count <= 0 {
		return nil
	}
	if strategy == "continuous" {
		return cheapestContiguous(slots, count, gridAllowed, powerW)
	}
	ranked := append([]Slot(nil), slots...)
	sort.SliceStable(ranked, func(i, j int) bool {
		ci := slotCost(ranked[i], gridAllowed, powerW)
		cj := slotCost(ranked[j], gridAllowed, powerW)
		if ci != cj {
			return ci < cj
		}
		return ranked[i].Start.After(ranked[j].Start)
	})
	chosen := ranked[:count]
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].Start.Before(chosen[j].Start) })
	return chosen
}

func cheapestContiguous(slots []Slot, count int, gridAllowed bool, powerW float64) []Slot {
	bestStart := -1
	bestCost := math.Inf(1)
	for i := 0; i+count <= len(slots); i++ {
		contiguous := true
		for j := i + 1; j < i+count; j++ {
			if !slots[j].Start.Equal(slots[j-1].End) {
				contiguous = false
				break
			}
		}
		if !contiguous {
			continue
		}
		cost := 0.0
		for j := i; j < i+count; j++ {
			cost += slotCost(slots[j], gridAllowed, powerW)
		}
		if cost < bestCost {
			bestCost = cost
			bestStart = i
		}
	}
	if bestStart < 0 {
		return nil
	}
	return append([]Slot(nil), slots[bestStart:bestStart+count]...)
}

func mergeWindows(slots []Slot, powerW float64, gridAllowed bool) []domain.EVWindow {
	windows := make([]domain.EVWindow, 0)
	for _, slot := range slots {
		source := "grid"
		if !gridAllowed || slot.PVSurplusW >= powerW {
			source = "pv"
		}
		// Derive the energy from the slot's real span so a clamped in-progress
		// slot only counts the time that is left in it.
		hours := slot.End.Sub(slot.Start).Hours()
		if hours <= 0 {
			continue
		}
		energy := powerW * hours / 1000
		cost := 0.0
		if source == "grid" {
			cost = energy * slot.ImportPrice
		}
		if len(windows) > 0 && windows[len(windows)-1].End.Equal(slot.Start) &&
			windows[len(windows)-1].Source == source {
			windows[len(windows)-1].End = slot.End
			windows[len(windows)-1].EnergyKWh = round2(windows[len(windows)-1].EnergyKWh + energy)
			windows[len(windows)-1].Cost = round2(windows[len(windows)-1].Cost + cost)
			continue
		}
		windows = append(windows, domain.EVWindow{
			Start: slot.Start.UTC(), End: slot.End.UTC(), PowerW: powerW,
			EnergyKWh: round2(energy), Source: source, Cost: round2(cost),
		})
	}
	return windows
}

func slotCost(slot Slot, gridAllowed bool, powerW float64) float64 {
	if !gridAllowed && slot.PVSurplusW >= powerW {
		return 0
	}
	return slot.ImportPrice
}

// trimWindows shortens the final windows so the plan does not over-deliver the
// remaining energy target.
func trimWindows(windows []domain.EVWindow, remainingKWh float64) []domain.EVWindow {
	total := 0.0
	for _, window := range windows {
		total += window.EnergyKWh
	}
	excess := total - remainingKWh
	for excess > 0.001 && len(windows) > 0 {
		last := &windows[len(windows)-1]
		if last.EnergyKWh <= excess+0.001 {
			excess -= last.EnergyKWh
			windows = windows[:len(windows)-1]
			continue
		}
		trimKWh := excess
		minutes := trimKWh / last.PowerW * 1000 * 60
		last.End = last.End.Add(-time.Duration(math.Round(minutes)) * time.Minute)
		last.EnergyKWh = round2(last.EnergyKWh - trimKWh)
		if last.EnergyKWh > 0 {
			last.Cost = round2(last.Cost * (last.EnergyKWh / (last.EnergyKWh + trimKWh)))
		}
		excess = 0
	}
	return windows
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*100) / 100
}
