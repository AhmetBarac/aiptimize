package planner

import (
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestPreviewFixedSlot(t *testing.T) {
	in := baseInputs()
	start := in.Blocks[0].Start
	slots := []domain.Slot{{
		DayLocal: start.In(testLoc).Format("2006-01-02"),
		Start:    start, End: start.Add(30 * time.Minute),
		Direction: domain.DirectionCharge, PowerW: 1000,
	}}
	result, validation, err := Preview(in, slots)
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid() {
		t.Fatalf("validation = %v", validation.Errors)
	}
	if len(result.Slots) != 1 || result.Slots[0].EnergyKWh != 0.5 {
		t.Fatalf("slots = %+v", result.Slots)
	}
	if result.Summary.ChargeKWh <= 0 {
		t.Errorf("summary = %+v", result.Summary)
	}
	if result.Curve[0].BatteryW != 1000 {
		t.Errorf("curve = %+v", result.Curve[0])
	}
}

func TestPreviewRejectsOverlapAndStarted(t *testing.T) {
	in := baseInputs()
	start := in.Blocks[0].Start
	slots := []domain.Slot{
		{Start: start, End: start.Add(time.Hour), Direction: domain.DirectionCharge, PowerW: 1000},
		{Start: start.Add(30 * time.Minute), End: start.Add(time.Hour), Direction: domain.DirectionDischarge, PowerW: 1000},
	}
	_, validation, err := Preview(in, slots)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid() {
		t.Fatal("expected overlap error")
	}
	in.Now = start.Add(time.Hour)
	_, validation, err = Preview(in, slots[:1])
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid() {
		t.Fatal("expected started-slot error")
	}
}

func TestPreviewRejectsSiteImportAndMidnight(t *testing.T) {
	in := baseInputs()
	start := in.Blocks[0].Start
	in.SiteImportW = 500
	in.Blocks[0].LoadW = 400
	slots := []domain.Slot{{Start: start, End: start.Add(30 * time.Minute), Direction: domain.DirectionCharge, PowerW: 1000}}
	_, validation, err := Preview(in, slots)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid() {
		t.Fatal("expected site import error")
	}

	in = baseInputs()
	late := time.Date(2026, 10, 29, 23, 30, 0, 0, testLoc)
	in.Blocks = []Block{{
		Start: late, End: late.Add(time.Hour), ImportPrice: 0.1, ExportPrice: 0.05,
	}}
	in.HorizonStart, in.HorizonEnd = late, late.Add(time.Hour)
	slots = []domain.Slot{{Start: late, End: late.Add(time.Hour), Direction: domain.DirectionCharge, PowerW: 1000}}
	_, validation, err = Preview(in, slots)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Valid() {
		t.Fatal("expected midnight error")
	}
}
