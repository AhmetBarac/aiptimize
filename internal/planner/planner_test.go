package planner

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

var testLoc = mustLoc("Europe/Stockholm")

func baseInputs() Inputs {
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, testLoc)
	blocks := make([]Block, 8)
	prices := []float64{0.1, 0.1, 0.1, 0.1, 1.0, 1.0, 1.0, 1.0}
	for i := range blocks {
		blocks[i] = Block{
			Start:       start.Add(time.Duration(i) * 30 * time.Minute),
			End:         start.Add(time.Duration(i+1) * 30 * time.Minute),
			ImportPrice: prices[i], ExportPrice: 0.05, LoadW: 1000,
		}
	}
	return Inputs{
		Timezone: "Europe/Stockholm", HorizonStart: blocks[0].Start, HorizonEnd: blocks[len(blocks)-1].End,
		StartSOC: 50, CapacityKWh: 10, MinSOC: 15, MaxSOC: 100, ReserveSOC: 15,
		MaxChargeW: 5000, MaxDischargeW: 5000, ChargeW: 5000, DischargeW: 5000,
		ChargeEfficiency: 0.95, DischargeEfficiency: 0.95, SiteImportW: 10000,
		MaxSlotsPerDay: 4, MaxSlotBlocks: 8, PreserveSOC: true, EndSOCTarget: 50,
		MinDailySaving: 0, PVConfidence: 1, AllowDischargeToGrid: true, Currency: "SEK", Blocks: blocks,
	}
}

func TestCheapNightExpensiveEvening(t *testing.T) {
	in := baseInputs()
	in.EndSOCTarget = 15
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Slots) == 0 {
		t.Fatalf("no slots: %+v", result.Summary)
	}
	if result.Summary.Savings <= 0 {
		t.Errorf("savings = %v", result.Summary.Savings)
	}
	var charge, discharge bool
	for _, slot := range result.Slots {
		if slot.Direction == "charge" {
			charge = true
		}
		if slot.Direction == "discharge" {
			discharge = true
		}
		if slot.End.Sub(slot.Start) > 4*time.Hour {
			t.Errorf("slot too long: %v", slot)
		}
	}
	if !charge || !discharge {
		t.Errorf("expected charge and discharge, got %+v", result.Slots)
	}
	for _, point := range result.Curve {
		if point.SOC < in.ReserveSOC-0.01 {
			t.Errorf("SOC below reserve: %+v", point)
		}
	}
}

func TestReserveAndSOCLimits(t *testing.T) {
	in := baseInputs()
	in.StartSOC = 20
	in.ReserveSOC = 40
	in.EndSOCTarget = 15
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range result.Curve {
		if point.SOC < in.StartSOC-0.01 {
			t.Fatalf("SOC fell below the starting reserve: %+v", point)
		}
	}
	before := map[time.Time]float64{result.Curve[0].Start: in.StartSOC}
	previous := in.StartSOC
	for _, point := range result.Curve {
		before[point.Start] = previous
		previous = point.SOC
	}
	for _, slot := range result.Slots {
		if slot.Direction == "discharge" && before[slot.Start] < in.ReserveSOC-0.01 {
			t.Errorf("discharge started at SOC %v below reserve %v: %+v", before[slot.Start], in.ReserveSOC, slot)
		}
	}
}

func TestSavingsThreshold(t *testing.T) {
	in := baseInputs()
	in.MinDailySaving = 1000
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Slots) != 0 || result.NoPlanReason == "" {
		t.Fatalf("expected empty plan, got %+v", result)
	}
	if result.Summary.Savings != 0 {
		t.Errorf("savings = %v", result.Summary.Savings)
	}
}

func TestSiteImportClamp(t *testing.T) {
	in := baseInputs()
	in.SiteImportW = 2000
	in.Blocks[0].LoadW = 1500
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range result.Curve {
		if point.BatteryW > 500.1 {
			t.Errorf("charge above site headroom: %+v", point)
		}
	}
}

func TestSlotBudgetAndMidnight(t *testing.T) {
	in := baseInputs()
	start := time.Date(2026, 10, 29, 21, 0, 0, 0, testLoc)
	in.Blocks = make([]Block, 12)
	for i := range in.Blocks {
		price := 0.1
		if i >= 6 {
			price = 1.0
		}
		in.Blocks[i] = Block{
			Start:       start.Add(time.Duration(i) * 30 * time.Minute),
			End:         start.Add(time.Duration(i+1) * 30 * time.Minute),
			ImportPrice: price, ExportPrice: 0.05, LoadW: 1000,
		}
	}
	in.HorizonStart = in.Blocks[0].Start
	in.HorizonEnd = in.Blocks[len(in.Blocks)-1].End
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range result.Slots {
		localStart := slot.Start.In(testLoc)
		localEnd := slot.End.In(testLoc)
		if localEnd.Sub(localStart) > 0 && localStart.Day() != localEnd.Day() && localEnd.Hour() != 0 {
			t.Errorf("slot crosses midnight: %v–%v", localStart, localEnd)
		}
	}
	counts := map[string]int{}
	for _, slot := range result.Slots {
		counts[slot.DayLocal]++
	}
	for day, count := range counts {
		if count > in.MaxSlotsPerDay {
			t.Errorf("day %s has %d slots", day, count)
		}
	}
}

func TestDeterministic(t *testing.T) {
	in := baseInputs()
	in.EndSOCTarget = 15
	first, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatalf("non-deterministic plan:\n%s\n%s", a, b)
	}
}

func TestBruteForceTinyHorizon(t *testing.T) {
	in := baseInputs()
	in.Blocks = in.Blocks[:6]
	in.HorizonStart = in.Blocks[0].Start
	in.HorizonEnd = in.Blocks[5].End
	in.MaxSlotBlocks = 3
	in.EndSOCTarget = 15
	in.Blocks[0].ImportPrice = 0.05
	in.Blocks[1].ImportPrice = 0.05
	in.Blocks[2].ImportPrice = 1.5
	in.Blocks[3].ImportPrice = 1.5
	in.Blocks[4].ImportPrice = 0.1
	in.Blocks[5].ImportPrice = 0.1
	o := newOptimizer(in)
	startIdx := o.socIndex(in.StartSOC)
	brute := bruteForce(o, in, 0, startIdx, 0)
	if math.IsInf(brute, 1) {
		t.Fatal("brute force infeasible")
	}
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	// The DP adds a 1e-6 slot penalty that the summary excludes.
	want := brute - float64(result.Summary.SlotCount)*1e-6
	if math.Abs(result.Summary.Cost-want) > 1e-5 {
		t.Fatalf("DP cost %v, brute force %v (raw %v)", result.Summary.Cost, want, brute)
	}
}

func bruteForce(o *optimizer, in Inputs, i, soc, used int) float64 {
	if i >= o.n {
		if in.PreserveSOC {
			target := math.Min(in.EndSOCTarget, in.StartSOC)
			if o.socValue(soc)+1e-9 < target {
				return math.Inf(1)
			}
		}
		return 0
	}
	if i > 0 && o.day[i] != o.day[i-1] {
		used = 0
	}
	best := o.blockCost(i, 0) + bruteForce(o, in, i+1, soc, used)
	if used >= in.MaxSlotsPerDay {
		return best
	}
	maxD := o.maxDuration(i)
	power := math.Min(in.ChargeW, in.MaxChargeW)
	for d := 1; d <= maxD; d++ {
		if clamp := o.chargePower(i, d, power); clamp < power {
			power = clamp
		}
		if power < 1 {
			break
		}
		cost, next, ok := o.simulate(i, d, soc, power)
		if !ok {
			continue
		}
		value := cost + 1e-6 + bruteForce(o, in, i+d, next, used+1)
		if value < best {
			best = value
		}
	}
	power = math.Min(in.DischargeW, in.MaxDischargeW)
	for d := 1; d <= maxD; d++ {
		cost, next, ok := o.simulate(i, d, soc, -power)
		if !ok {
			continue
		}
		value := cost + 1e-6 + bruteForce(o, in, i+d, next, used+1)
		if value < best {
			best = value
		}
	}
	return best
}

func TestNoDischargeToGrid(t *testing.T) {
	in := baseInputs()
	in.AllowDischargeToGrid = false
	in.StartSOC = 100
	in.EndSOCTarget = 15
	for i := range in.Blocks {
		in.Blocks[i].LoadW = 1000
		in.Blocks[i].ImportPrice = 2.0
	}
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range result.Curve {
		if point.BatteryW < 0 && point.BatteryW < -1000.1 {
			t.Errorf("discharged %v W into load 1000 W: %+v", -point.BatteryW, point)
		}
	}
}

func TestNoDischargeDuringEVWindow(t *testing.T) {
	in := baseInputs()
	in.StartSOC = 90
	in.EndSOCTarget = 15
	in.Blocks[2].ImportPrice = 5
	in.Blocks[3].ImportPrice = 5
	in.EVWindows = []bool{false, false, true, true, false, false, false, false}
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range result.Curve {
		if in.EVWindows[indexOfStart(in.Blocks, point.Start)] && point.BatteryW < 0 {
			t.Errorf("battery discharged during EV grid window: %+v", point)
		}
	}
}

func indexOfStart(blocks []Block, start time.Time) int {
	for i, block := range blocks {
		if block.Start.Equal(start) {
			return i
		}
	}
	return 0
}

func TestPVCharging(t *testing.T) {
	in := baseInputs()
	for i := range in.Blocks {
		in.Blocks[i].ImportPrice = 0.2
		in.Blocks[i].ExportPrice = 0.01
		in.Blocks[i].LoadW = 200
	}
	// Strong midday PV with a later expensive load.
	for i := 2; i < 4; i++ {
		in.Blocks[i].PVW = 4000
	}
	for i := 4; i < 6; i++ {
		in.Blocks[i].ImportPrice = 2.0
		in.Blocks[i].LoadW = 3000
	}
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Savings <= 0 {
		t.Errorf("savings = %v", result.Summary.Savings)
	}
	if math.IsNaN(result.Summary.Cost) {
		t.Error("NaN cost")
	}
}
