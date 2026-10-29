package planner

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// Validation is the result of checking an edited slot list.
type Validation struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// Valid reports whether the edited plan can be applied.
func (v Validation) Valid() bool { return len(v.Errors) == 0 }

// Preview simulates a fixed slot list and returns its cost, SOC curve and slots.
func Preview(in Inputs, slots []domain.Slot) (Result, Validation, error) {
	validation := Validation{Errors: []string{}, Warnings: []string{}}
	loc := mustLoc(in.Timezone)
	now := in.Now
	if now.IsZero() {
		now = in.HorizonStart
	}
	byStart := make(map[int64]int, len(in.Blocks))
	for i, block := range in.Blocks {
		byStart[block.Start.Unix()] = i
	}
	inHorizon := func(t time.Time) bool {
		return !t.Before(in.HorizonStart) && !t.After(in.HorizonEnd)
	}

	sorted := append([]domain.Slot(nil), slots...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	perDay := map[string]int{}
	prevEnd := time.Time{}
	for i, slot := range sorted {
		label := fmt.Sprintf("slot %d", i+1)
		if !slot.Direction.Valid() {
			validation.Errors = append(validation.Errors, label+": invalid direction")
			continue
		}
		if slot.PowerW <= 0 || slot.PowerW > in.MaxChargeW && slot.Direction == domain.DirectionCharge {
			validation.Errors = append(validation.Errors, label+": power outside the battery limit")
		}
		if slot.PowerW <= 0 || slot.PowerW > in.MaxDischargeW && slot.Direction == domain.DirectionDischarge {
			validation.Errors = append(validation.Errors, label+": power outside the battery limit")
		}
		duration := slot.End.Sub(slot.Start)
		if duration < 30*time.Minute || duration > time.Duration(in.MaxSlotBlocks)*30*time.Minute ||
			duration%(30*time.Minute) != 0 {
			validation.Errors = append(validation.Errors, label+": duration must be 30–240 minutes in 30-minute steps")
		}
		localStart := slot.Start.In(loc)
		localEnd := slot.End.In(loc)
		if localStart.Second() != 0 || localStart.Nanosecond() != 0 || (localStart.Minute() != 0 && localStart.Minute() != 30) {
			validation.Errors = append(validation.Errors, label+": start must be on :00/:30")
		}
		if localStart.Format("2006-01-02") != localEnd.Format("2006-01-02") &&
			!(localEnd.Hour() == 0 && localEnd.Minute() == 0) {
			validation.Errors = append(validation.Errors, label+": slot crosses local midnight")
		}
		if !inHorizon(slot.Start) || slot.End.After(in.HorizonEnd) {
			validation.Errors = append(validation.Errors, label+": outside the planning horizon")
		}
		if slot.Start.Before(now) {
			if slot.End.After(now) {
				validation.Warnings = append(validation.Warnings, label+": active slot; it must remain unchanged")
			} else {
				validation.Errors = append(validation.Errors, label+": has already ended")
			}
		}
		if _, ok := byStart[slot.Start.Unix()]; !ok {
			validation.Errors = append(validation.Errors, label+": start is not a 30-minute block boundary")
		}
		if _, ok := byStart[slot.End.Unix()]; !ok && slot.End.Before(in.HorizonEnd) {
			validation.Errors = append(validation.Errors, label+": end is not a 30-minute block boundary")
		}
		if !prevEnd.IsZero() && slot.Start.Before(prevEnd) {
			validation.Errors = append(validation.Errors, label+": overlaps the previous slot")
		}
		if slot.End.After(prevEnd) {
			prevEnd = slot.End
		}
		day := localStart.Format("2006-01-02")
		perDay[day]++
		if perDay[day] > in.MaxSlotsPerDay {
			validation.Errors = append(validation.Errors, fmt.Sprintf("day %s: more than %d slots", day, in.MaxSlotsPerDay))
		}
	}
	if len(slots) == 0 {
		validation.Warnings = append(validation.Warnings, "The plan has no slots; it will run as self-consumption.")
	}

	// Simulate the fixed schedule block by block.
	result := Result{Slots: []domain.Slot{}, Curve: make([]CurvePoint, 0, len(in.Blocks)), Warnings: []string{}}
	soc := in.StartSOC
	baseline := 0.0
	total := 0.0
	chargeKWh := 0.0
	dischargeKWh := 0.0
	slotForBlock := make(map[int]domain.Slot)
	for _, slot := range sorted {
		start, okStart := byStart[slot.Start.Unix()]
		if !okStart {
			continue
		}
		end, okEnd := byStart[slot.End.Unix()]
		if !okEnd {
			end = len(in.Blocks)
		}
		for i := start; i < end && i < len(in.Blocks); i++ {
			slotForBlock[i] = slot
		}
	}
	for i, block := range in.Blocks {
		batteryW := 0.0
		if slot, ok := slotForBlock[i]; ok {
			if slot.Direction == domain.DirectionCharge {
				batteryW = slot.PowerW
				headroom := in.SiteImportW - (block.LoadW + block.EVW - math.Max(0, block.PVW*in.PVConfidence))
				if batteryW > headroom {
					validation.Errors = append(validation.Errors,
						fmt.Sprintf("%s: charge power exceeds site import headroom", block.Start.In(loc).Format("15:04")))
				}
			} else {
				batteryW = -slot.PowerW
				if i < len(in.EVWindows) && in.EVWindows[i] {
					validation.Errors = append(validation.Errors,
						fmt.Sprintf("%s: discharge overlaps an EV grid-charging window", block.Start.In(loc).Format("15:04")))
				}
				if !in.AllowDischargeToGrid {
					available := block.LoadW + block.EVW - math.Max(0, block.PVW*in.PVConfidence)
					if slot.PowerW > available {
						validation.Errors = append(validation.Errors,
							fmt.Sprintf("%s: discharge would export to the grid", block.Start.In(loc).Format("15:04")))
					}
				}
			}
		}
		if block.Missing {
			validation.Errors = append(validation.Errors, "missing price block inside the horizon")
		}
		net := block.LoadW + block.EVW + batteryW - math.Max(0, block.PVW*in.PVConfidence)
		cost := 0.0
		if net >= 0 {
			cost = net * 0.5 / 1000 * block.ImportPrice
		} else {
			cost = net * 0.5 / 1000 * block.ExportPrice
		}
		if batteryW < 0 {
			cost += in.CycleCost * (-batteryW) * 0.5 / 1000
		}
		baseline += baselineCost(block, in.PVConfidence)
		total += cost
		if batteryW > 0 {
			delta := batteryW * 0.5 / 1000 * in.ChargeEfficiency / in.CapacityKWh * 100
			soc += delta
			if soc > in.MaxSOC {
				validation.Errors = append(validation.Errors, "SOC would exceed the maximum")
				soc = in.MaxSOC
			}
			chargeKWh += batteryW * 0.5 / 1000 * in.ChargeEfficiency
		} else if batteryW < 0 {
			delta := -batteryW * 0.5 / 1000 / in.DischargeEfficiency / in.CapacityKWh * 100
			soc -= delta
			if soc < math.Max(in.ReserveSOC, in.MinSOC) {
				validation.Errors = append(validation.Errors, "SOC would fall below the reserve")
				soc = math.Max(in.ReserveSOC, in.MinSOC)
			}
			dischargeKWh += -batteryW * 0.5 / 1000 / in.DischargeEfficiency
		}
		result.Curve = append(result.Curve, CurvePoint{
			Start: block.Start.UTC(), End: block.End.UTC(),
			PriceImport: block.ImportPrice, PriceExport: block.ExportPrice,
			PVW: block.PVW, PVPlanW: round1(math.Max(0, block.PVW*in.PVConfidence)),
			LoadW: block.LoadW, EVW: block.EVW, BatteryW: round1(batteryW),
			SOC: round2(soc), Cost: round2(cost), Missing: block.Missing,
		})
	}
	if in.PreserveSOC {
		target := math.Min(in.EndSOCTarget, in.StartSOC)
		if soc+1e-9 < target {
			validation.Errors = append(validation.Errors, fmt.Sprintf("end SOC %.1f%% is below the target %.1f%%", soc, target))
		}
	}

	for _, slot := range sorted {
		energy := slot.PowerW * slot.End.Sub(slot.Start).Hours() / 1000
		copy := slot
		copy.DayLocal = slot.Start.In(loc).Format("2006-01-02")
		copy.EnergyKWh = round2(energy)
		avg := 0.0
		count := 0
		for i, block := range in.Blocks {
			if !block.Start.Before(slot.Start) && block.Start.Before(slot.End) {
				avg += block.ImportPrice
				count++
			}
			_ = i
		}
		if count > 0 {
			copy.AvgImportPrice = round2(avg / float64(count))
		}
		result.Slots = append(result.Slots, copy)
	}
	result.Summary = domain.PlanSummary{
		Cost: round2(total), BaselineCost: round2(baseline), Savings: round2(baseline - total),
		ChargeKWh: round2(chargeKWh), DischargeKWh: round2(dischargeKWh),
		Cycles: round2(dischargeKWh / in.CapacityKWh), SlotCount: len(sorted), Currency: in.Currency,
	}
	return result, validation, nil
}

func baselineCost(block Block, confidence float64) float64 {
	net := block.LoadW + block.EVW - math.Max(0, block.PVW*confidence)
	if net >= 0 {
		return net * 0.5 / 1000 * block.ImportPrice
	}
	return net * 0.5 / 1000 * block.ExportPrice
}
