package planner

import (
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestMinimizeCostHasNoDischargeSlots(t *testing.T) {
	in := baseInputs()
	in.Strategy = "minimize_cost"
	in.MaxPurchasePrice = 1.0
	result, err := Optimize(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range result.Slots {
		if slot.Direction == domain.DirectionDischarge {
			t.Fatalf("minimize purchase cost generated a discharge slot: %+v", slot)
		}
	}
	if len(result.Slots) == 0 {
		t.Fatal("minimize purchase cost generated no charge slots")
	}
}

func TestStrategyChargeEligibility(t *testing.T) {
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	in := Inputs{
		Timezone: "UTC", Strategy: "minimize_cost", MaxPurchasePrice: 1.0, MinPriceDifference: 0.2,
		Blocks: []Block{
			{Start: base, End: base.Add(30 * time.Minute), ImportPrice: 0.8, ExportPrice: 0.05},
			{Start: base.Add(30 * time.Minute), End: base.Add(time.Hour), ImportPrice: 1.3, ExportPrice: 0.05},
		},
	}
	o := newOptimizer(in)
	if !o.charge[0] {
		t.Fatal("cheap block with a later expensive block should be chargeable")
	}
	if o.charge[1] {
		t.Fatal("block above the maximum purchase price must not be chargeable")
	}

	in.MaxPurchasePrice = 0
	o = newOptimizer(in)
	if o.charge[0] {
		t.Fatal("zero maximum purchase price must block a positive price")
	}

	in.Strategy = "minimize_cost"
	in.MinPriceDifference = 2.0
	o = newOptimizer(in)
	if o.charge[0] {
		t.Fatal("block without the required future price difference must not be chargeable")
	}
}
