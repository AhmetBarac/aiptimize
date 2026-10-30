package service

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// buildInverterPlan reconstructs the active plan for one local day from the
// schedule read back from the inverter. Savings are deliberately a simple,
// transparent estimate: energy put into the battery is assumed to be used
// immediately after the charge window, at the grid price of that next block.
// This avoids pretending the inverter follows the app's later discharge slots.
func (s *PlannerService) buildInverterPlan(ctx context.Context, day string, schedule domain.Schedule) *domain.Plan {
	loc := s.cfg.Location()
	date, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return nil
	}
	entries := schedule.Days[scheduleDayKey(date)]
	if len(entries) == 0 {
		return nil
	}
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	priceSnap, priceErr := s.prices.Snapshot(ctx, dayStart, dayEnd.AddDate(0, 0, 1))
	priceAt := func(t time.Time) (float64, bool) {
		for _, block := range priceSnap.Blocks {
			if !t.Before(block.Start) && t.Before(block.End) {
				return block.ImportPrice, true
			}
		}
		return 0, false
	}

	values := s.cfg.Get()
	chargeEff := values.Inverter.ChargeEfficiency
	if chargeEff <= 0 || chargeEff > 1 {
		chargeEff = 0.95
	}
	dischargeEff := values.Inverter.DischargeEfficiency
	if dischargeEff <= 0 || dischargeEff > 1 {
		dischargeEff = 0.95
	}
	cycleCost := values.Inverter.CycleCost

	plan := &domain.Plan{
		CreatedAt: s.now().UTC(), HorizonStart: dayStart.UTC(), HorizonEnd: dayEnd.UTC(),
		Timezone: values.Site.Timezone, Status: domain.PlanStatusApplied,
		Origin: domain.OriginAuto, Revision: 1, LockedDay: day,
		Summary: domain.PlanSummary{Currency: values.Price.Currency},
		Slots:   []domain.Slot{}, Warnings: []string{},
	}
	if priceErr != nil {
		plan.Warnings = append(plan.Warnings, "Savings estimate unavailable: "+priceErr.Error())
	}
	hasCharge := false
	chargeCost := 0.0
	for _, entry := range entries {
		start := dayStart.Add(time.Duration(entry.Hour)*time.Hour + time.Duration(entry.Minute)*time.Minute)
		end := start.Add(time.Duration(entry.DurationMin) * time.Minute)
		direction := domain.DirectionCharge
		power := schedule.PinW
		if entry.Discharge {
			direction = domain.DirectionDischarge
			power = schedule.PoutW
		}
		if power <= 0 {
			if direction == domain.DirectionCharge {
				power = values.Plan.ChargeW
			} else {
				power = values.Plan.DischargeW
			}
		}
		power = math.Round(power)
		energy := power * end.Sub(start).Hours() / 1000
		slot := domain.Slot{
			DayLocal: day, Start: start.UTC(), End: end.UTC(), Direction: direction,
			PowerW: power, EnergyKWh: round2(energy), Reason: "inverter",
		}
		price, ok := priceAt(start)
		if ok {
			slot.AvgImportPrice = price
		}
		if direction == domain.DirectionCharge {
			hasCharge = true
			usable := energy * chargeEff * dischargeEff
			nextPrice := price
			if after, ok := priceAt(end); ok {
				nextPrice = after
			}
			if ok {
				slot.ExpectedSaving = round2(usable*(nextPrice-price) - usable*cycleCost)
				plan.Summary.Savings += slot.ExpectedSaving
				chargeCost += energy * price
			} else {
				plan.Warnings = append(plan.Warnings, "Savings estimate unavailable for "+start.In(loc).Format("15:04")+".")
			}
			plan.Summary.ChargeKWh += energy
		} else {
			plan.Summary.DischargeKWh += energy
		}
		plan.Slots = append(plan.Slots, slot)
	}
	// If the inverter only discharges (for example from PV pre-charge), value
	// the avoided import at the discharge block instead of assuming a charge.
	if !hasCharge {
		plan.Summary.Savings = 0
		for i := range plan.Slots {
			slot := &plan.Slots[i]
			if slot.Direction != domain.DirectionDischarge {
				continue
			}
			price := slot.AvgImportPrice
			usable := slot.EnergyKWh * dischargeEff
			slot.ExpectedSaving = round2(usable*price - usable*cycleCost)
			plan.Summary.Savings += slot.ExpectedSaving
		}
	}
	plan.Summary.Savings = round2(plan.Summary.Savings)
	plan.Summary.Cost = round2(chargeCost)
	plan.Summary.ChargeKWh = round2(plan.Summary.ChargeKWh)
	plan.Summary.DischargeKWh = round2(plan.Summary.DischargeKWh)
	plan.Summary.SlotCount = len(plan.Slots)
	if values.Inverter.CapacityKWh > 0 {
		plan.Summary.Cycles = round2(plan.Summary.DischargeKWh / values.Inverter.CapacityKWh)
	}
	sort.Slice(plan.Slots, func(i, j int) bool { return plan.Slots[i].Start.Before(plan.Slots[j].Start) })
	return plan
}
