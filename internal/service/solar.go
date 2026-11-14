package service

import (
	"context"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/forecast"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// SolarPoint is one 30-minute production/forecast block for the solar page.
type SolarPoint struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	ActualW     *float64  `json:"actual_w"`
	ActualLoadW *float64  `json:"actual_load_w,omitempty"`
	ExpectedW   float64   `json:"expected_w"`
	MinW        float64   `json:"min_w"`
	MaxW        float64   `json:"max_w"`
	LoadW       float64   `json:"load_w"`
}

// SolarSnapshot combines measured production and the day's PV forecast.
type SolarSnapshot struct {
	Date        string       `json:"date"`
	Timezone    string       `json:"timezone"`
	Now         time.Time    `json:"now"`
	CurrentW    float64      `json:"current_w"`
	ActualKWh   float64      `json:"actual_kwh"`
	ExpectedKWh float64      `json:"expected_kwh"`
	MinKWh      float64      `json:"min_kwh"`
	MaxKWh      float64      `json:"max_kwh"`
	LoadKWh     float64      `json:"load_kwh"`
	HasForecast bool         `json:"has_forecast"`
	Stale       bool         `json:"stale"`
	GeneratedAt *time.Time   `json:"generated_at,omitempty"`
	Points      []SolarPoint `json:"points"`
	Warnings    []string     `json:"warnings"`
}

// SolarService builds the solar detail page snapshot.
type SolarService struct {
	cfg      *settings.Service
	st       *store.Store
	forecast *ForecastService
	now      func() time.Time
}

// NewSolarService builds the service.
func NewSolarService(cfg *settings.Service, st *store.Store, forecastSvc *ForecastService) *SolarService {
	return &SolarService{cfg: cfg, st: st, forecast: forecastSvc, now: time.Now}
}

// Snapshot returns today's measured PV, the full-day forecast and its band.
func (s *SolarService) Snapshot(ctx context.Context) (SolarSnapshot, error) {
	loc := s.cfg.Location()
	now := s.now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	out := SolarSnapshot{
		Date:     dayStart.Format("2006-01-02"),
		Timezone: s.cfg.Get().Site.Timezone,
		Now:      now.UTC(),
		Warnings: []string{},
		Points:   []SolarPoint{},
	}

	type bucket struct {
		sum     float64
		count   int
		loadSum float64
		loadN   int
	}
	actual := make(map[int64]*bucket)
	samples, err := s.st.PVSamplesBetween(ctx, dayStart, dayEnd)
	if err != nil {
		return SolarSnapshot{}, err
	}
	for _, sample := range samples {
		if sample.TS.After(now) {
			continue
		}
		start := sample.TS.UTC().Truncate(30 * time.Minute).Unix()
		b := actual[start]
		if b == nil {
			b = &bucket{}
			actual[start] = b
		}
		if sample.PVW > 0 {
			b.sum += sample.PVW
		}
		b.count++
		if sample.LoadW >= 0 {
			b.loadSum += sample.LoadW
			b.loadN++
		}
	}
	if latest, err := s.st.LatestTelemetry(ctx); err == nil && !latest.TS.Before(dayStart) && latest.TS.Before(dayEnd) {
		out.CurrentW = latest.PVW
	}
	if len(actual) == 0 {
		out.Warnings = append(out.Warnings, "no measured PV today yet")
	}

	expected := make(map[int64]forecast.Point)
	if s.forecast != nil {
		if points, date, ok := s.forecast.TodayPV(); ok && date == out.Date {
			for _, point := range points {
				expected[point.Start.Unix()] = point
			}
		}
		if snap, ok := s.forecast.Latest(); ok {
			out.Stale = snap.Stale
			generated := snap.GeneratedAt
			out.GeneratedAt = &generated
			if len(expected) == 0 {
				for _, point := range snap.Points {
					if !point.Start.Before(dayStart) && point.Start.Before(dayEnd) {
						expected[point.Start.Unix()] = point
					}
				}
			}
		}
	}
	out.HasForecast = len(expected) > 0
	if !out.HasForecast {
		out.Warnings = append(out.Warnings, "no PV forecast for today")
	}

	for ts := dayStart; ts.Before(dayEnd); ts = ts.Add(30 * time.Minute) {
		key := ts.UTC().Unix()
		point := SolarPoint{Start: ts.UTC(), End: ts.Add(30 * time.Minute).UTC()}
		if b, ok := actual[key]; ok && b.count > 0 {
			avg := round1(b.sum / float64(b.count))
			point.ActualW = &avg
			out.ActualKWh += avg * 0.5 / 1000
			if b.loadN > 0 {
				load := round1(b.loadSum / float64(b.loadN))
				point.ActualLoadW = &load
			}
		}
		if f, ok := expected[key]; ok {
			point.ExpectedW = f.PVW
			point.MinW = f.PVMinW
			point.MaxW = f.PVMaxW
			point.LoadW = f.LoadW
			out.ExpectedKWh += f.PVW * 0.5 / 1000
			out.MinKWh += f.PVMinW * 0.5 / 1000
			out.MaxKWh += f.PVMaxW * 0.5 / 1000
			out.LoadKWh += f.LoadW * 0.5 / 1000
		}
		out.Points = append(out.Points, point)
	}
	out.ActualKWh = round2(out.ActualKWh)
	out.ExpectedKWh = round2(out.ExpectedKWh)
	out.MinKWh = round2(out.MinKWh)
	out.MaxKWh = round2(out.MaxKWh)
	out.LoadKWh = round2(out.LoadKWh)
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].Start.Before(out.Points[j].Start) })
	return out, nil
}
