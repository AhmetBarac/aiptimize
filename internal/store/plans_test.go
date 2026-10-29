package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestSaveAndLoadPlan(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	start := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	plan := domain.Plan{
		CreatedAt:    start.Add(-time.Minute),
		HorizonStart: start,
		HorizonEnd:   start.Add(2 * time.Hour),
		Timezone:     "Europe/Stockholm",
		Status:       domain.PlanStatusSuggestion,
		Origin:       domain.OriginAuto,
		InputsHash:   "abc123",
		Summary:      domain.PlanSummary{Cost: 1.5, BaselineCost: 2.5, Savings: 1, SlotCount: 2, Currency: "SEK"},
		Curve: []domain.PlanCurvePoint{{
			Start: start, End: start.Add(30 * time.Minute), PriceImport: 0.1,
			PVW: 0, PVPlanW: 0, LoadW: 500, BatteryW: 0, SOC: 50, Cost: 0.05,
		}},
		Warnings:     []string{"test warning"},
		NoPlanReason: "",
		Inputs:       json.RawMessage(`{"start_soc":50}`),
		Slots: []domain.Slot{{
			DayLocal: "2026-10-29", Start: start, End: start.Add(time.Hour),
			Direction: domain.DirectionCharge, PowerW: 3000, EnergyKWh: 3,
			AvgImportPrice: 0.1, ExpectedSaving: 0.5, Reason: "test",
		}},
	}
	saved, err := st.SavePlan(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatal("plan ID not set")
	}
	loaded, err := st.LatestPlan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != saved.ID || loaded.InputsHash != "abc123" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if len(loaded.Slots) != 1 || loaded.Slots[0].Direction != domain.DirectionCharge {
		t.Fatalf("slots = %+v", loaded.Slots)
	}
	if len(loaded.Curve) != 1 || loaded.Curve[0].SOC != 50 {
		t.Fatalf("curve = %+v", loaded.Curve)
	}
	if len(loaded.Warnings) != 1 || loaded.Warnings[0] != "test warning" {
		t.Fatalf("warnings = %v", loaded.Warnings)
	}
	var inputs map[string]any
	if err := json.Unmarshal(loaded.Inputs, &inputs); err != nil {
		t.Fatal(err)
	}
	if inputs["start_soc"].(float64) != 50 {
		t.Fatalf("inputs = %s", loaded.Inputs)
	}
	hash, err := st.LatestPlanInputsHash(ctx)
	if err != nil || hash != "abc123" {
		t.Fatalf("hash = %q, %v", hash, err)
	}
	if _, err := st.PlanByID(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
}
