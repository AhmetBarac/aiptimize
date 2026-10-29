package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/forecast"
	"github.com/AhmetBarac/aiptimize/internal/forecast/load"
	"github.com/AhmetBarac/aiptimize/internal/forecast/pv"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// ForecastOptions controls how the forecast service obtains weather data.
type ForecastOptions struct {
	Simulator        bool
	Version          string
	OpenMeteoBaseURL string
	// NewFetcher injects a fake PV fetcher in tests.
	NewFetcher func(values settings.Values) (pv.Fetcher, error)
}

// ForecastService fetches, caches and publishes the PV/load forecast.
type ForecastService struct {
	cfg  *settings.Service
	st   *store.Store
	log  *slog.Logger
	pub  Publisher
	opts ForecastOptions
	now  func() time.Time

	loadProvider *load.Provider

	mu      sync.Mutex
	latest  *forecast.Snapshot
	history map[time.Time]float64
}

// NewForecastService builds the service.
func NewForecastService(cfg *settings.Service, st *store.Store, log *slog.Logger, pub Publisher, opts ForecastOptions) *ForecastService {
	if log == nil {
		log = slog.Default()
	}
	return &ForecastService{
		cfg:          cfg,
		st:           st,
		log:          log,
		pub:          pub,
		opts:         opts,
		now:          time.Now,
		loadProvider: load.New(cfg, st),
		history:      make(map[time.Time]float64),
	}
}

// Run refreshes the forecast immediately and then every pv.refresh_minutes.
func (s *ForecastService) Run(ctx context.Context) {
	_, _ = s.Refresh(ctx)
	for {
		minutes := s.cfg.Get().PV.RefreshMinutes
		if minutes < 5 {
			minutes = 30
		}
		timer := time.NewTimer(time.Duration(minutes) * time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_, _ = s.Refresh(ctx)
		}
	}
}

// Latest returns the cached snapshot with staleness applied.
func (s *ForecastService) Latest() (forecast.Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return forecast.Snapshot{}, false
	}
	snap := s.latest.Copy()
	age := s.now().Sub(snap.GeneratedAt)
	if age > 3*time.Hour {
		snap.Stale = true
		snap.PVTotalKWh = 0
		for i := range snap.Points {
			snap.Points[i].PVW = 0
			for j := range snap.Points[i].PVStrings {
				snap.Points[i].PVStrings[j] = 0
			}
		}
		snap.Warnings = append(snap.Warnings, "forecast older than 3 hours; PV set to 0")
	}
	return snap, true
}

// Refresh fetches a new forecast and stores it in memory.
func (s *ForecastService) Refresh(ctx context.Context) (forecast.Snapshot, error) {
	values := s.cfg.Get()
	loc := s.cfg.Location()
	now := s.now().In(loc)
	from := now.UTC().Truncate(30 * time.Minute)
	to := from.Add(48 * time.Hour)

	specs := make([]pv.StringSpec, 0, len(values.PV.Strings))
	infos := make([]forecast.StringInfo, 0, len(values.PV.Strings))
	for _, str := range values.PV.Strings {
		specs = append(specs, pv.StringSpec{
			Name: str.Name, KWP: str.KWP, Tilt: str.Tilt, Azimuth: str.Azimuth,
			PerformanceRatio: str.PerformanceRatio, TempCoefficient: str.TempCoefficient, MaxW: str.MaxW,
		})
		infos = append(infos, forecast.StringInfo{
			Name: str.Name, KWP: str.KWP, Tilt: str.Tilt, Azimuth: str.Azimuth,
		})
	}

	warnings := []string{}
	var pvBlocks []pv.Block
	pvFailed := false
	if len(specs) == 0 {
		warnings = append(warnings, "no PV strings configured")
	} else if s.opts.Simulator {
		pvBlocks = pv.Simulate(specs, from, to, loc)
	} else {
		blocks, err := s.fetchPV(ctx, values, specs, from, to)
		if err != nil {
			pvFailed = true
			warnings = append(warnings, "PV forecast unavailable: "+err.Error())
		} else {
			pvBlocks = blocks
		}
	}

	loadPoints, loadWarnings, err := s.loadProvider.Build(ctx, from, to)
	warnings = append(warnings, loadWarnings...)
	if err != nil {
		warnings = append(warnings, "load forecast failed: "+err.Error())
	}

	if pvFailed {
		if previous, ok := s.fallback(ctx, now, warnings); ok {
			return previous, nil
		}
	}

	factor, samples := s.calibration(ctx, now)
	if values.PV.CalibrationEnabled && factor != 1 {
		for i := range pvBlocks {
			pvBlocks[i].PVW = round1(pvBlocks[i].PVW * factor)
			for j := range pvBlocks[i].PVStrings {
				pvBlocks[i].PVStrings[j] = round1(pvBlocks[i].PVStrings[j] * factor)
			}
		}
	}

	loadByStart := make(map[int64]float64, len(loadPoints))
	for _, point := range loadPoints {
		loadByStart[point.Start.Unix()] = point.LoadW
	}
	points := make([]forecast.Point, 0, len(pvBlocks))
	pvTotal := 0.0
	loadTotal := 0.0
	for _, block := range pvBlocks {
		loadW := loadByStart[block.Start.Unix()]
		points = append(points, forecast.Point{
			Start: block.Start, End: block.End, PVW: block.PVW,
			PVStrings: block.PVStrings, LoadW: loadW,
		})
		pvTotal += block.PVW * 0.5 / 1000
		loadTotal += loadW * 0.5 / 1000
	}
	if len(pvBlocks) == 0 {
		for _, point := range loadPoints {
			points = append(points, forecast.Point{
				Start: point.Start, End: point.End, PVStrings: []float64{}, LoadW: point.LoadW,
			})
			loadTotal += point.LoadW * 0.5 / 1000
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Start.Before(points[j].Start) })

	source := "Open-Meteo"
	if s.opts.Simulator {
		source = "Simulator"
	}
	snap := forecast.Snapshot{
		GeneratedAt:        now.UTC(),
		HorizonStart:       from,
		HorizonEnd:         to,
		Timezone:           values.Site.Timezone,
		Confidence:         values.PV.Confidence,
		CalibrationFactor:  factor,
		CalibrationSamples: samples,
		Stale:              pvFailed,
		PVTotalKWh:         round2(pvTotal),
		LoadTotalKWh:       round2(loadTotal),
		Strings:            infos,
		Points:             points,
		Warnings:           warnings,
		Source:             source,
	}

	s.mu.Lock()
	s.latest = &snap
	for _, point := range points {
		if point.PVW > 0 {
			s.history[point.Start] = point.PVW
		}
	}
	cutoff := now.AddDate(0, 0, -8)
	for ts := range s.history {
		if ts.Before(cutoff) {
			delete(s.history, ts)
		}
	}
	s.mu.Unlock()

	if s.pub != nil {
		s.pub.Publish("forecast", map[string]any{
			"generated_at": snap.GeneratedAt, "pv_total_kwh": snap.PVTotalKWh,
			"load_total_kwh": snap.LoadTotalKWh, "stale": snap.Stale,
		})
	}
	return snap, nil
}

func (s *ForecastService) fetchPV(ctx context.Context, values settings.Values, specs []pv.StringSpec, from, to time.Time) ([]pv.Block, error) {
	fetcher := pv.Fetcher(nil)
	if s.opts.NewFetcher != nil {
		var err error
		fetcher, err = s.opts.NewFetcher(values)
		if err != nil {
			return nil, err
		}
	} else {
		fetcher = pv.NewClient(s.opts.OpenMeteoBaseURL,
			fmt.Sprintf("aiptimize/%s (+https://github.com/AhmetBarac/aiptimize)", s.opts.Version))
	}
	series := make(map[string]pv.Hourly, len(specs))
	for _, spec := range specs {
		key := spec.Key()
		if _, ok := series[key]; ok {
			continue
		}
		hourly, err := fetcher.Fetch(ctx, pv.Request{
			Latitude: values.Site.Latitude, Longitude: values.Site.Longitude,
			Tilt: spec.Tilt, Azimuth: spec.Azimuth, Days: 2,
		})
		if err != nil {
			return nil, err
		}
		series[key] = hourly
	}
	return pv.Build(series, specs, from, to, values.PV.InverterACLimitW), nil
}

func (s *ForecastService) fallback(ctx context.Context, now time.Time, warnings []string) (forecast.Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return forecast.Snapshot{}, false
	}
	snap := s.latest.Copy()
	snap.Stale = true
	snap.Warnings = append(snap.Warnings, warnings...)
	if now.Sub(snap.GeneratedAt) > 3*time.Hour {
		snap.PVTotalKWh = 0
		for i := range snap.Points {
			snap.Points[i].PVW = 0
			for j := range snap.Points[i].PVStrings {
				snap.Points[i].PVStrings[j] = 0
			}
		}
	}
	// Keep the published cache consistent with what the API will return.
	s.latest = &snap
	return snap, true
}

// calibration compares past forecast values with measured PV over seven days.
func (s *ForecastService) calibration(ctx context.Context, now time.Time) (float64, int) {
	values := s.cfg.Get()
	if !values.PV.CalibrationEnabled {
		return 1, 0
	}
	samples, err := s.st.PVSamplesBetween(ctx, now.AddDate(0, 0, -7), now)
	if err != nil {
		return 1, 0
	}
	s.mu.Lock()
	history := make(map[time.Time]float64, len(s.history))
	for ts, w := range s.history {
		history[ts] = w
	}
	s.mu.Unlock()

	var sumForecast, sumMeasured float64
	count := 0
	for _, sample := range samples {
		start := sample.TS.UTC().Truncate(30 * time.Minute)
		predicted, ok := history[start]
		if !ok || predicted <= 5 {
			continue
		}
		measured := sample.PVW
		if measured < 0 {
			measured = 0
		}
		sumForecast += predicted
		sumMeasured += measured
		count++
	}
	if count < 20 || sumForecast <= 0 {
		return 1, count
	}
	factor := sumMeasured / sumForecast
	if factor < 0.5 {
		factor = 0.5
	}
	if factor > 1.5 {
		factor = 1.5
	}
	return math.Round(factor*100) / 100, count
}

func round1(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*100) / 100
}
