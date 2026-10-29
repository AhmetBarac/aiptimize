// Package load builds the house-load forecast from the configured profile,
// blended with measured telemetry when enough history exists.
package load

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Point is one 30-minute load forecast.
type Point struct {
	Start time.Time
	End   time.Time
	LoadW float64
}

// Provider builds load forecasts.
type Provider struct {
	cfg *settings.Service
	st  *store.Store
	now func() time.Time
}

// New builds a Provider.
func New(cfg *settings.Service, st *store.Store) *Provider {
	return &Provider{cfg: cfg, st: st, now: time.Now}
}

// Build returns 30-minute load points for [from, to). It also returns warnings
// when the measured profile is enabled but not ready.
func (p *Provider) Build(ctx context.Context, from, to time.Time) ([]Point, []string, error) {
	values := p.cfg.Get()
	loc := p.cfg.Location()
	var warnings []string

	profile := values.Load.Profile
	if len(profile) != 24 {
		profile = settings.Default().Load.Profile
	}
	dailyKWh := values.Load.DailyKWh

	var measured [7][24]float64
	var counts [7][24]int
	if values.Load.UseMeasured {
		ready := p.loadMeasured(ctx, loc, &measured, &counts, &warnings)
		if !ready {
			warnings = append(warnings, "measured load profile not ready; using configured profile")
		}
	}

	start := from.UTC().Truncate(30 * time.Minute)
	end := to.UTC()
	if !end.After(start) {
		return nil, warnings, nil
	}
	points := make([]Point, 0, int(end.Sub(start)/(30*time.Minute)))
	for ts := start; ts.Before(end); ts = ts.Add(30 * time.Minute) {
		local := ts.In(loc)
		hour := local.Hour()
		weekday := (int(local.Weekday()) + 6) % 7 // Monday = 0
		configuredW := dailyKWh * 1000 * profile[hour]
		loadW := configuredW
		if values.Load.UseMeasured {
			count := counts[weekday][hour]
			if count >= 4 {
				// More samples mean more trust in the measured median, capped
				// at 70% so the configured profile still anchors the forecast.
				weight := math.Min(0.7, float64(count)/20*0.7)
				loadW = measured[weekday][hour]*weight + configuredW*(1-weight)
			}
		}
		points = append(points, Point{Start: ts, End: ts.Add(30 * time.Minute), LoadW: round1(loadW)})
	}
	return points, warnings, nil
}

func (p *Provider) loadMeasured(
	ctx context.Context,
	loc *time.Location,
	measured *[7][24]float64,
	counts *[7][24]int,
	warnings *[]string,
) bool {
	now := p.now().In(loc)
	from := now.AddDate(0, 0, -14)
	samples, err := p.st.LoadSamplesBetween(ctx, from, now)
	if err != nil {
		*warnings = append(*warnings, "measured load profile unavailable: "+err.Error())
		return false
	}
	var buckets [7][24][]float64
	days := make(map[string]struct{})
	for _, sample := range samples {
		local := sample.TS.In(loc)
		value := sample.LoadW - sample.EVW
		if value < 0 {
			value = 0
		}
		buckets[(int(local.Weekday())+6)%7][local.Hour()] = append(
			buckets[(int(local.Weekday())+6)%7][local.Hour()], value)
		days[local.Format("2006-01-02")] = struct{}{}
	}
	if len(samples) < 500 || len(days) < 3 {
		return false
	}
	readyBuckets := 0
	for wd := 0; wd < 7; wd++ {
		for hour := 0; hour < 24; hour++ {
			values := buckets[wd][hour]
			counts[wd][hour] = len(values)
			if len(values) == 0 {
				continue
			}
			sort.Float64s(values)
			measured[wd][hour] = median(values)
			if len(values) >= 4 {
				readyBuckets++
			}
		}
	}
	return readyBuckets >= 24*7/2
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func round1(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*10) / 10
}
