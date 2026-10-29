package store

import (
	"context"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestTelemetryStore(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 29, 21, 14, 6, 0, time.UTC)

	first := domain.Telemetry{
		TS: base, SOC: 94, PVW: 132, PVStrings: []float64{65.7, 46.5},
		GridW: 39, BatteryW: -2187, LoadW: 2358, InverterMode: 2,
		Extra: map[string]any{"bst": 3, "soh": 98},
	}
	if err := st.InsertTelemetry(ctx, first); err != nil {
		t.Fatalf("InsertTelemetry: %v", err)
	}
	second := first
	second.TS = base.Add(30 * time.Second)
	second.SOC = 95
	second.BatteryW = -2000
	if err := st.InsertTelemetry(ctx, second); err != nil {
		t.Fatal(err)
	}

	latest, err := st.LatestTelemetry(ctx)
	if err != nil {
		t.Fatalf("LatestTelemetry: %v", err)
	}
	if !latest.TS.Equal(second.TS) || latest.SOC != 95 || latest.BatteryW != -2000 {
		t.Fatalf("latest = %+v", latest)
	}
	if len(latest.PVStrings) != 2 || latest.PVStrings[0] != 65.7 {
		t.Fatalf("pv strings = %v", latest.PVStrings)
	}
	if latest.Extra["bst"] != float64(3) {
		// JSON numbers decode as float64.
		if latest.Extra["bst"] != 3 {
			t.Fatalf("extra = %v", latest.Extra)
		}
	}

	all, err := st.TelemetryBetween(ctx, base, base.Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || !all[0].TS.Equal(base) || !all[1].TS.Equal(second.TS) {
		t.Fatalf("between = %+v", all)
	}

	// Same timestamp replaces the row.
	updated := first
	updated.SOC = 80
	if err := st.InsertTelemetry(ctx, updated); err != nil {
		t.Fatal(err)
	}
	all, _ = st.TelemetryBetween(ctx, base, base.Add(time.Hour), 10)
	if len(all) != 2 || all[0].SOC != 80 {
		t.Fatalf("upsert = %+v", all)
	}
}

func TestTelemetryEmpty(t *testing.T) {
	st := openTest(t)
	if _, err := st.LatestTelemetry(context.Background()); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
