package pv

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// StringSpec is one configured MPPT string.
type StringSpec struct {
	Name             string
	KWP              float64
	Tilt             float64
	Azimuth          float64
	PerformanceRatio float64
	TempCoefficient  float64
	MaxW             float64
}

// Key identifies a distinct Open-Meteo request (tilt/azimuth pair).
func (s StringSpec) Key() string {
	return fmt.Sprintf("%.1f|%.1f", s.Tilt, s.Azimuth)
}

// Block is one 30-minute PV forecast.
type Block struct {
	Start     time.Time
	End       time.Time
	PVW       float64
	PVStrings []float64
}

// Build computes 30-minute PV blocks from hourly Open-Meteo series.
// Irradiance and temperature are linearly interpolated at each block midpoint.
func Build(series map[string]Hourly, specs []StringSpec, from, to time.Time, acLimitW float64) []Block {
	if len(specs) == 0 {
		return nil
	}
	start := from.UTC().Truncate(30 * time.Minute)
	end := to.UTC()
	if !end.After(start) {
		return nil
	}
	blocks := make([]Block, 0, int(end.Sub(start)/(30*time.Minute)))
	for ts := start; ts.Before(end); ts = ts.Add(30 * time.Minute) {
		mid := ts.Add(15 * time.Minute)
		stringsW := make([]float64, len(specs))
		total := 0.0
		for i, spec := range specs {
			h, ok := series[spec.Key()]
			if !ok {
				continue
			}
			gti, temp := interpolate(h, mid)
			w := StringPower(spec, gti, temp)
			stringsW[i] = round1(w)
			total += w
		}
		if acLimitW > 0 && total > acLimitW {
			total = acLimitW
			if len(stringsW) > 0 {
				scale := total / sum(stringsW)
				for i := range stringsW {
					stringsW[i] = round1(stringsW[i] * scale)
				}
			}
		}
		blocks = append(blocks, Block{
			Start:     ts,
			End:       ts.Add(30 * time.Minute),
			PVW:       round1(total),
			PVStrings: stringsW,
		})
	}
	return blocks
}

// StringPower applies the documented model:
//
//	Tcell     = Tair + (GTI / 800) * 25
//	tempFactor = 1 + tempCoefficient * (Tcell - 25)
//	P          = clamp(kWp * 1000 * (GTI / 1000) * PR * tempFactor, 0, MaxW)
func StringPower(spec StringSpec, gti, airTemp float64) float64 {
	if gti <= 0 || spec.KWP <= 0 {
		return 0
	}
	pr := spec.PerformanceRatio
	if pr <= 0 {
		pr = 0.85
	}
	tempFactor := 1 + spec.TempCoefficient*((airTemp+gti/800*25)-25)
	if tempFactor < 0 {
		tempFactor = 0
	}
	w := spec.KWP * 1000 * (gti / 1000) * pr * tempFactor
	if spec.MaxW > 0 && w > spec.MaxW {
		w = spec.MaxW
	}
	if w < 0 {
		w = 0
	}
	return w
}

func interpolate(h Hourly, at time.Time) (gti, temp float64) {
	if len(h.Time) == 0 {
		return 0, 0
	}
	if !at.After(h.Time[0]) {
		return h.GTI[0], h.Temp[0]
	}
	last := len(h.Time) - 1
	if !at.Before(h.Time[last]) {
		return h.GTI[last], h.Temp[last]
	}
	idx := sort.Search(len(h.Time), func(i int) bool { return h.Time[i].After(at) }) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= last {
		return h.GTI[last], h.Temp[last]
	}
	span := h.Time[idx+1].Sub(h.Time[idx])
	if span <= 0 {
		return h.GTI[idx], h.Temp[idx]
	}
	ratio := float64(at.Sub(h.Time[idx])) / float64(span)
	return h.GTI[idx] + (h.GTI[idx+1]-h.GTI[idx])*ratio,
		h.Temp[idx] + (h.Temp[idx+1]-h.Temp[idx])*ratio
}

// Simulate builds a deterministic PV forecast for demos and tests.
func Simulate(specs []StringSpec, from, to time.Time, loc *time.Location) []Block {
	if loc == nil {
		loc = time.UTC
	}
	if len(specs) == 0 {
		return nil
	}
	start := from.In(loc).Truncate(time.Hour).Add(-time.Hour)
	end := to.In(loc).Add(2 * time.Hour)
	series := make(map[string]Hourly, len(specs))
	for _, spec := range specs {
		key := spec.Key()
		if _, ok := series[key]; ok {
			continue
		}
		h := Hourly{}
		for ts := start; !ts.After(end); ts = ts.Add(time.Hour) {
			local := ts.In(loc)
			hour := float64(local.Hour()) + float64(local.Minute())/60
			solar := math.Sin(math.Pi * (hour - 6) / 12)
			if solar < 0 {
				solar = 0
			}
			doy := float64(local.YearDay())
			season := 0.65 + 0.35*math.Sin(2*math.Pi*(doy-80)/365)
			cloud := 0.88 + 0.08*math.Sin(float64(local.YearDay())*1.7+hour/3)
			gti := 870 * solar * season * cloud
			temp := 6 + 16*math.Sin(2*math.Pi*(doy-120)/365) + 6*solar
			h.Time = append(h.Time, ts.UTC())
			h.GTI = append(h.GTI, gti)
			h.Temp = append(h.Temp, temp)
		}
		series[key] = h
	}
	return Build(series, specs, from, to, 0)
}

func sum(values []float64) float64 {
	var out float64
	for _, v := range values {
		out += v
	}
	return out
}

func round1(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*10) / 10
}
