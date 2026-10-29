package pv

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompassToOpenMeteo(t *testing.T) {
	cases := map[float64]float64{
		0:   -180,
		90:  -90,
		120: -60,
		180: 0,
		270: 90,
		359: 179,
	}
	for compass, want := range cases {
		if got := CompassToOpenMeteo(compass); math.Abs(got-want) > 0.001 {
			t.Errorf("CompassToOpenMeteo(%v) = %v, want %v", compass, got, want)
		}
	}
}

func TestClientFetch(t *testing.T) {
	var query map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = map[string]string{
			"latitude":  r.URL.Query().Get("latitude"),
			"longitude": r.URL.Query().Get("longitude"),
			"tilt":      r.URL.Query().Get("tilt"),
			"azimuth":   r.URL.Query().Get("azimuth"),
			"days":      r.URL.Query().Get("forecast_days"),
			"timezone":  r.URL.Query().Get("timezone"),
			"hourly":    r.URL.Query().Get("hourly"),
			"ua":        r.UserAgent(),
		}
		times := make([]string, 0, 48)
		gti := make([]any, 0, 48)
		temp := make([]any, 0, 48)
		start := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
		for i := 0; i < 48; i++ {
			times = append(times, start.Add(time.Duration(i)*time.Hour).Format("2006-01-02T15:04"))
			if i == 5 {
				gti = append(gti, nil)
			} else {
				gti = append(gti, 100+i)
			}
			temp = append(temp, 10.5)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hourly": map[string]any{"time": times, "global_tilted_irradiance": gti, "temperature_2m": temp},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "aiptimize/test")
	h, err := c.Fetch(context.Background(), Request{
		Latitude: 59.3293, Longitude: 18.0686, Tilt: 35, Azimuth: 120, Days: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Time) != 48 || len(h.GTI) != 48 || len(h.Temp) != 48 {
		t.Fatalf("series lengths = %d/%d/%d", len(h.Time), len(h.GTI), len(h.Temp))
	}
	if h.GTI[5] != 0 {
		t.Errorf("null GTI not zeroed: %v", h.GTI[5])
	}
	if h.GTI[4] != 104 || h.Temp[0] != 10.5 {
		t.Errorf("values = %v / %v", h.GTI[4], h.Temp[0])
	}
	want := map[string]string{
		"latitude": "59.3293", "longitude": "18.0686", "tilt": "35.0",
		"azimuth": "-60.0", "days": "2", "timezone": "UTC",
		"hourly": "global_tilted_irradiance,temperature_2m", "ua": "aiptimize/test",
	}
	for key, value := range want {
		if query[key] != value {
			t.Errorf("query %s = %q, want %q", key, query[key], value)
		}
	}
}

func TestClientErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"provider error", http.StatusInternalServerError, "boom"},
		{"invalid json", http.StatusOK, "not json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := NewClient(srv.URL, "test")
			if _, err := c.Fetch(context.Background(), Request{Latitude: 1, Longitude: 1, Tilt: 30, Azimuth: 180}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStringPowerFormula(t *testing.T) {
	spec := StringSpec{KWP: 4, Tilt: 35, Azimuth: 180, PerformanceRatio: 0.85, TempCoefficient: -0.004}
	// GTI 800 -> Tcell 50, tempFactor 0.9, power 4*1000*0.8*0.85*0.9 = 2448.
	if got := StringPower(spec, 800, 25); math.Abs(got-2448) > 0.01 {
		t.Fatalf("power = %v", got)
	}
	if got := StringPower(spec, -10, 25); got != 0 {
		t.Errorf("negative GTI = %v", got)
	}
	spec.MaxW = 2000
	if got := StringPower(spec, 800, 25); got != 2000 {
		t.Errorf("MaxW clip = %v", got)
	}
	spec.MaxW = 0
	spec.KWP = 0
	if got := StringPower(spec, 800, 25); got != 0 {
		t.Errorf("zero kWp = %v", got)
	}
}

func TestBuildInterpolationAndClipping(t *testing.T) {
	start := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	spec := StringSpec{KWP: 4, Tilt: 35, Azimuth: 180, PerformanceRatio: 0.85, TempCoefficient: -0.004}
	hourly := Hourly{
		Time: []time.Time{start, start.Add(time.Hour), start.Add(2 * time.Hour)},
		GTI:  []float64{0, 1000, 0},
		Temp: []float64{20, 25, 20},
	}
	series := map[string]Hourly{spec.Key(): hourly}
	blocks := Build(series, []StringSpec{spec}, start, start.Add(90*time.Minute), 0)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	// 00:15 -> GTI 250; 00:45 -> 750; 01:15 -> 750.
	if blocks[0].PVW <= 0 || blocks[1].PVW <= blocks[0].PVW || blocks[2].PVW <= 0 {
		t.Errorf("interpolated blocks = %+v", blocks)
	}
	// Two strings each 4 kWp, clipped to the inverter AC limit.
	spec2 := spec
	spec2.Name = "west"
	series[spec2.Key()] = hourly
	clipped := Build(series, []StringSpec{spec, spec2}, start.Add(time.Hour), start.Add(90*time.Minute), 3000)
	if len(clipped) != 1 || clipped[0].PVW != 3000 {
		t.Fatalf("clipped = %+v", clipped)
	}
	if len(clipped[0].PVStrings) != 2 || clipped[0].PVStrings[0]+clipped[0].PVStrings[1] < 2999 {
		t.Errorf("string split = %v", clipped[0].PVStrings)
	}
}

func TestSimulate(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	spec := StringSpec{KWP: 5, Tilt: 35, Azimuth: 180, PerformanceRatio: 0.85, TempCoefficient: -0.004}
	noon := time.Date(2026, 6, 21, 12, 0, 0, 0, loc)
	blocks := Simulate([]StringSpec{spec}, noon, noon.Add(2*time.Hour), loc)
	if len(blocks) != 4 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if blocks[0].PVW <= 0 {
		t.Errorf("noon PV = %v", blocks[0].PVW)
	}
	night := time.Date(2026, 6, 21, 1, 0, 0, 0, loc)
	nblocks := Simulate([]StringSpec{spec}, night, night.Add(time.Hour), loc)
	for _, b := range nblocks {
		if b.PVW != 0 {
			t.Errorf("night PV = %v", b.PVW)
		}
	}
}
