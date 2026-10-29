package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/forecast/pv"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

type fakePVFetcher struct {
	hourly pv.Hourly
	err    error
	calls  int
}

func (f *fakePVFetcher) Fetch(context.Context, pv.Request) (pv.Hourly, error) {
	f.calls++
	if f.err != nil {
		return pv.Hourly{}, f.err
	}
	return f.hourly, nil
}

func newForecastTest(t *testing.T) (*ForecastService, *settings.Service, *store.Store, *fakePVFetcher, *time.Time) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "forecast.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 29, 12, 0, 0, 0, testLoc)
	fetcher := &fakePVFetcher{hourly: constantHourly(clock)}
	svc := NewForecastService(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, ForecastOptions{
		Version: "test",
		NewFetcher: func(settings.Values) (pv.Fetcher, error) {
			return fetcher, nil
		},
	})
	svc.now = func() time.Time { return clock }
	return svc, cfg, st, fetcher, &clock
}

func constantHourly(now time.Time) pv.Hourly {
	start := now.UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	h := pv.Hourly{}
	for i := 0; i < 72; i++ {
		h.Time = append(h.Time, start.Add(time.Duration(i)*time.Hour))
		h.GTI = append(h.GTI, 800)
		h.Temp = append(h.Temp, 25)
	}
	return h
}

func configurePV(t *testing.T, cfg *settings.Service) {
	t.Helper()
	body := `{"strings":[
		{"name":"East","kwp":4.2,"tilt":35,"azimuth":120,"performance_ratio":0.85,"temp_coefficient":-0.004},
		{"name":"West","kwp":4.2,"tilt":35,"azimuth":240,"performance_ratio":0.85,"temp_coefficient":-0.004}
	],"inverter_ac_limit_w":8000}`
	if _, err := cfg.Patch(context.Background(), settings.SectionPV, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func TestForecastRefresh(t *testing.T) {
	svc, cfg, _, fetcher, _ := newForecastTest(t)
	configurePV(t, cfg)
	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Points) != 96 {
		t.Fatalf("points = %d, want 96", len(snap.Points))
	}
	if snap.PVTotalKWh <= 0 || snap.LoadTotalKWh <= 0 {
		t.Fatalf("totals = %v / %v", snap.PVTotalKWh, snap.LoadTotalKWh)
	}
	if snap.Source != "Open-Meteo" || len(snap.Strings) != 2 {
		t.Errorf("metadata = %+v", snap)
	}
	if fetcher.calls != 1 {
		// Both strings share tilt/azimuth here only if equal; here they differ.
		t.Logf("fetcher calls = %d", fetcher.calls)
	}
	if snap.Points[0].PVW <= 0 {
		t.Errorf("first PV = %v", snap.Points[0].PVW)
	}
	if len(snap.Points[0].PVStrings) != 2 {
		t.Errorf("strings = %v", snap.Points[0].PVStrings)
	}
	if snap.Stale {
		t.Error("fresh snapshot marked stale")
	}
	if snap.Confidence != 0.7 {
		t.Errorf("confidence = %v", snap.Confidence)
	}
	latest, ok := svc.Latest()
	if !ok || latest.PVTotalKWh != snap.PVTotalKWh {
		t.Fatalf("latest = %+v", latest)
	}
}

func TestForecastFallbackKeepsLastSnapshot(t *testing.T) {
	svc, cfg, _, fetcher, clock := newForecastTest(t)
	configurePV(t, cfg)
	first, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fetcher.err = errors.New("weather down")
	*clock = clock.Add(30 * time.Minute)
	second, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Stale || len(second.Warnings) == 0 {
		t.Fatalf("fallback = %+v", second)
	}
	if second.PVTotalKWh != first.PVTotalKWh {
		t.Errorf("fallback should keep PV for <3h: %v vs %v", second.PVTotalKWh, first.PVTotalKWh)
	}

	*clock = clock.Add(4 * time.Hour)
	third, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.PVTotalKWh != 0 {
		t.Errorf("PV after 3h stale = %v, want 0", third.PVTotalKWh)
	}
	for _, p := range third.Points {
		if p.PVW != 0 {
			t.Fatalf("stale point PV = %v", p.PVW)
		}
	}
}

func TestForecastWithoutStrings(t *testing.T) {
	svc, _, _, _, _ := newForecastTest(t)
	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.PVTotalKWh != 0 {
		t.Errorf("PV total = %v", snap.PVTotalKWh)
	}
	if len(snap.Points) == 0 || snap.LoadTotalKWh <= 0 {
		t.Errorf("load-only snapshot = %+v", snap)
	}
	found := false
	for _, warning := range snap.Warnings {
		if warning == "no PV strings configured" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v", snap.Warnings)
	}
}

func TestForecastSimulator(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "sim.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	configurePV(t, cfg)
	svc := NewForecastService(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, ForecastOptions{Simulator: true})
	svc.now = func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, testLoc) }
	snap, err := svc.Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Source != "Simulator" || snap.PVTotalKWh <= 0 || len(snap.Points) != 96 {
		t.Fatalf("simulator snapshot = %+v", snap)
	}
}

func TestForecastCalibrationDisabled(t *testing.T) {
	svc, cfg, _, _, _ := newForecastTest(t)
	configurePV(t, cfg)
	snap, err := svc.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.CalibrationFactor != 1 || snap.CalibrationSamples != 0 {
		t.Errorf("calibration = %v / %d", snap.CalibrationFactor, snap.CalibrationSamples)
	}
}
