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
	dayPV   []forecast.Point
	dayDate string
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
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	// Start at the beginning of the local day, not at now: the dashboard overlays
	// measured values, and those only exist for the hours already elapsed.
	from := dayStart.UTC()
	to := from.Add(48 * time.Hour)
	dayEnd := dayStart.AddDate(0, 0, 1)

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
	var pvBlocks, dayBlocks []pv.Block
	pvFailed := false
	if len(specs) == 0 {
		warnings = append(warnings, "no PV strings configured")
	} else if s.opts.Simulator {
		pvBlocks = pv.Simulate(specs, from, to, loc)
		dayBlocks = pv.Simulate(specs, dayStart, dayEnd, loc)
	} else {
		series, err := s.fetchSeries(ctx, values, specs)
		if err != nil {
			pvFailed = true
			warnings = append(warnings, "PV forecast unavailable: "+err.Error())
		} else {
			pvBlocks = pv.Build(series, specs, from, to, values.PV.InverterACLimitW)
			dayBlocks = pv.Build(series, specs, dayStart, dayEnd, values.PV.InverterACLimitW)
		}
	}

	loadTemps := make(map[int64]float64, len(pvBlocks))
	for _, block := range pvBlocks {
		if block.HasTemp {
			loadTemps[block.Start.Unix()] = block.TempC
		}
	}
	dayLoadTemps := make(map[int64]float64, len(dayBlocks))
	for _, block := range dayBlocks {
		if block.HasTemp {
			dayLoadTemps[block.Start.Unix()] = block.TempC
		}
	}

	loadPoints, loadWarnings, err := s.loadProvider.BuildWithTemperature(ctx, from, to, loadTemps)
	warnings = append(warnings, loadWarnings...)
	if err != nil {
		warnings = append(warnings, "load forecast failed: "+err.Error())
	}
	dayLoadPoints, _, dayLoadErr := s.loadProvider.BuildWithTemperature(ctx, dayStart, dayEnd, dayLoadTemps)
	if dayLoadErr != nil {
		warnings = append(warnings, "day load forecast failed: "+dayLoadErr.Error())
	}

	if pvFailed {
		if previous, ok := s.fallback(ctx, now, warnings); ok {
			return previous, nil
		}
	}

	factor, samples := s.calibration(ctx, now)
	if values.PV.CalibrationEnabled && factor != 1 {
		scaleBlocks(pvBlocks, factor)
		scaleBlocks(dayBlocks, factor)
	}

	loadByStart := make(map[int64]float64, len(loadPoints))
	for _, point := range loadPoints {
		loadByStart[point.Start.Unix()] = point.LoadW
	}
	confidence := values.PV.Confidence
	acLimit := values.PV.InverterACLimitW
	points := make([]forecast.Point, 0, len(pvBlocks))
	pvTotal := 0.0
	loadTotal := 0.0
	for _, block := range pvBlocks {
		loadW := loadByStart[block.Start.Unix()]
		points = append(points, makePoint(block, loadW, confidence, acLimit))
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

	dayPoints := make([]forecast.Point, 0, len(dayBlocks))
	dayLoadByStart := make(map[int64]float64, len(dayLoadPoints))
	for _, point := range dayLoadPoints {
		dayLoadByStart[point.Start.Unix()] = point.LoadW
	}
	for _, block := range dayBlocks {
		dayPoints = append(dayPoints, makePoint(block, dayLoadByStart[block.Start.Unix()], confidence, acLimit))
	}

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
	s.dayPV = dayPoints
	s.dayDate = dayStart.Format("2006-01-02")
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

func (s *ForecastService) fetchSeries(ctx context.Context, values settings.Values, specs []pv.StringSpec) (map[string]pv.Hourly, error) {
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
			Tilt: spec.Tilt, Azimuth: spec.Azimuth, Days: 3,
		})
		if err != nil {
			return nil, err
		}
		series[key] = hourly
	}
	return series, nil
}

// TodayPV returns today's full-day PV forecast (including past blocks).
func (s *ForecastService) TodayPV() ([]forecast.Point, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.dayPV) == 0 {
		return nil, s.dayDate, false
	}
	out := make([]forecast.Point, len(s.dayPV))
	for i, p := range s.dayPV {
		out[i] = p
		out[i].PVStrings = append([]float64{}, p.PVStrings...)
	}
	return out, s.dayDate, true
}

func makePoint(block pv.Block, loadW, confidence, acLimitW float64) forecast.Point {
	uncertainty := 1 - confidence
	if uncertainty < 0 {
		uncertainty = 0
	}
	if uncertainty > 1 {
		uncertainty = 1
	}
	minW := block.PVW * (1 - uncertainty)
	maxW := block.PVW * (1 + uncertainty)
	if minW < 0 {
		minW = 0
	}
	if acLimitW > 0 && maxW > acLimitW {
		maxW = acLimitW
	}
	return forecast.Point{
		Start: block.Start, End: block.End,
		PVW: block.PVW, PVMinW: round1(minW), PVMaxW: round1(maxW),
		PVStrings: block.PVStrings, LoadW: loadW,
	}
}

func scaleBlocks(blocks []pv.Block, factor float64) {
	for i := range blocks {
		blocks[i].PVW = round1(blocks[i].PVW * factor)
		for j := range blocks[i].PVStrings {
			blocks[i].PVStrings[j] = round1(blocks[i].PVStrings[j] * factor)
		}
	}
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
			snap.Points[i].PVMinW = 0
			snap.Points[i].PVMaxW = 0
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
