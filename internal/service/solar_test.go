package service

import (
	"context"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestSolarSnapshotCombinesActualAndForecast(t *testing.T) {
	forecastSvc, cfg, st, _, clock := newForecastTest(t)
	configurePV(t, cfg)
	if _, err := forecastSvc.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	loc := testLoc
	samples := []struct {
		hour, minute int
		pv           float64
	}{
		{9, 0, 1000},
		{9, 30, 1200},
		{10, 0, 800},
	}
	for _, sample := range samples {
		ts := time.Date(2026, 10, 29, sample.hour, sample.minute, 0, 0, loc)
		if err := st.InsertTelemetry(context.Background(), domain.Telemetry{
			TS: ts, SOC: 50, PVW: sample.pv, LoadW: 500,
		}); err != nil {
			t.Fatal(err)
		}
	}
	solarSvc := NewSolarService(cfg, st, forecastSvc)
	solarSvc.now = func() time.Time { return *clock }
	snap, err := solarSvc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Points) != 48 {
		t.Fatalf("points = %d, want 48", len(snap.Points))
	}
	if snap.CurrentW != 800 {
		t.Errorf("current = %v, want 800", snap.CurrentW)
	}
	if snap.ActualKWh != 1.5 {
		t.Errorf("actual kWh = %v, want 1.5", snap.ActualKWh)
	}
	if snap.ExpectedKWh <= 0 || snap.LoadKWh <= 0 {
		t.Fatalf("expected/load = %v/%v", snap.ExpectedKWh, snap.LoadKWh)
	}
	if snap.MinKWh >= snap.ExpectedKWh || snap.MaxKWh <= snap.ExpectedKWh {
		t.Errorf("band = %v/%v/%v", snap.MinKWh, snap.ExpectedKWh, snap.MaxKWh)
	}
	// 09:00-09:30 block has measured data.
	start := time.Date(2026, 10, 29, 9, 0, 0, 0, loc)
	var found bool
	for _, point := range snap.Points {
		if point.Start.Equal(start.UTC()) {
			found = true
			if point.ActualW == nil || *point.ActualW != 1000 {
				t.Errorf("actual at 09:00 = %v", point.ActualW)
			}
			if point.ExpectedW <= 0 || point.LoadW <= 0 {
				t.Errorf("forecast at 09:00 = %+v", point)
			}
			if point.MinW >= point.ExpectedW || point.MaxW <= point.ExpectedW {
				t.Errorf("band at 09:00 = %+v", point)
			}
		}
	}
	if !found {
		t.Fatal("09:00 block missing")
	}
}

func TestSolarSnapshotEmpty(t *testing.T) {
	forecastSvc, cfg, st, _, _ := newForecastTest(t)
	_ = forecastSvc
	solarSvc := NewSolarService(cfg, st, forecastSvc)
	snap, err := solarSvc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.HasForecast {
		t.Errorf("unexpected forecast: %+v", snap)
	}
	if len(snap.Warnings) == 0 {
		t.Error("expected warnings")
	}
	if len(snap.Points) != 48 {
		t.Errorf("points = %d", len(snap.Points))
	}
}
