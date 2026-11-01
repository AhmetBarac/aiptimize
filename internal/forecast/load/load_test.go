package load

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

func testProvider(t *testing.T) (*Provider, *settings.Service, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "load.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, st), cfg, st
}

func TestTemperatureAdjustment(t *testing.T) {
	provider, cfg, _ := testProvider(t)
	ctx := context.Background()
	if _, err := cfg.Patch(ctx, settings.SectionLoad, []byte(`{
		"use_temperature_adjustment":true,
		"temperature_reference_c":18,
		"temperature_sensitivity_pct_c":2
	}`)); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	temps := map[int64]float64{
		from.Unix():                       8,
		from.Add(30 * time.Minute).Unix(): 28,
	}
	points, warnings, err := provider.BuildWithTemperature(ctx, from, from.Add(time.Hour), temps)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d", len(points))
	}
	values := cfg.Get()
	base := values.Load.DailyKWh * 1000 * values.Load.Profile[from.In(cfg.Location()).Hour()]
	if math.Abs(points[0].LoadW-base*1.2) > 0.2 {
		t.Errorf("cold load = %v, want %v", points[0].LoadW, base*1.2)
	}
	if math.Abs(points[1].LoadW-base*0.8) > 0.2 {
		t.Errorf("warm load = %v, want %v", points[1].LoadW, base*0.8)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestTemperatureAdjustmentMissingForecast(t *testing.T) {
	provider, cfg, _ := testProvider(t)
	ctx := context.Background()
	if _, err := cfg.Patch(ctx, settings.SectionLoad, []byte(`{"use_temperature_adjustment":true}`)); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	_, warnings, err := provider.BuildWithTemperature(ctx, from, from.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a missing-temperature warning")
	}
}

func TestConfiguredProfile(t *testing.T) {
	provider, cfg, _ := testProvider(t)
	values := cfg.Get()
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	points, warnings, err := provider.Build(context.Background(), from, from.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if len(points) != 4 {
		t.Fatalf("points = %d", len(points))
	}
	want := values.Load.DailyKWh * 1000 * values.Load.Profile[from.In(cfg.Location()).Hour()]
	if math.Abs(points[0].LoadW-want) > 0.2 {
		t.Errorf("load = %v, want %v", points[0].LoadW, want)
	}
}

func TestMeasuredProfileBlend(t *testing.T) {
	provider, cfg, st := testProvider(t)
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	provider.now = func() time.Time { return now }
	ctx := context.Background()

	// 14 days of 30-minute samples at 800 W house load; EV load is separate.
	for day := 0; day < 14; day++ {
		for slot := 0; slot < 48; slot++ {
			ts := now.AddDate(0, 0, -day).Truncate(24 * time.Hour).Add(time.Duration(slot) * 30 * time.Minute)
			if ts.After(now) {
				continue
			}
			if err := st.InsertTelemetry(ctx, domain.Telemetry{
				TS: ts, SOC: 50, LoadW: 800, EVW: 400,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := cfg.Patch(ctx, settings.SectionLoad, []byte(`{"use_measured_profile":true}`)); err != nil {
		t.Fatal(err)
	}
	points, warnings, err := provider.Build(ctx, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d", len(points))
	}
	if points[0].LoadW < 400 || points[0].LoadW > 900 {
		t.Errorf("blended load = %v, expected between configured and 800 W", points[0].LoadW)
	}
}

func TestMeasuredProfileNotReady(t *testing.T) {
	provider, cfg, _ := testProvider(t)
	ctx := context.Background()
	if _, err := cfg.Patch(ctx, settings.SectionLoad, []byte(`{"use_measured_profile":true}`)); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	points, warnings, err := provider.Build(ctx, from, from.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("points = %d", len(points))
	}
	if len(warnings) == 0 {
		t.Fatal("expected a not-ready warning")
	}
}
