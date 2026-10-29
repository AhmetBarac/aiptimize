package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func TestForecastAPIRefreshAndRead(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	body := `{"strings":[
		{"name":"East","kwp":4.2,"tilt":35,"azimuth":120,"performance_ratio":0.85,"temp_coefficient":-0.004},
		{"name":"West","kwp":4.2,"tilt":35,"azimuth":240,"performance_ratio":0.85,"temp_coefficient":-0.004}
	],"inverter_ac_limit_w":8000}`
	if _, err := a.settings.Patch(context.Background(), settings.SectionPV, []byte(body)); err != nil {
		t.Fatal(err)
	}

	resp, responseBody := a.post("/api/v1/forecast/refresh", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh forecast = %d %s", resp.StatusCode, responseBody)
	}
	snap := decodeMap(t, responseBody)
	points := snap["points"].([]any)
	if len(points) != 96 {
		t.Fatalf("points = %d, want 96", len(points))
	}
	if snap["pv_total_kwh"].(float64) <= 0 || snap["load_total_kwh"].(float64) <= 0 {
		t.Fatalf("totals = %v / %v", snap["pv_total_kwh"], snap["load_total_kwh"])
	}
	if len(snap["strings"].([]any)) != 2 {
		t.Fatalf("strings = %v", snap["strings"])
	}
	first := points[0].(map[string]any)
	if first["pv_w"].(float64) <= 0 || first["load_w"].(float64) <= 0 {
		t.Fatalf("first point = %v", first)
	}
	if len(first["pv_strings"].([]any)) != 2 {
		t.Fatalf("pv strings = %v", first["pv_strings"])
	}

	resp, responseBody = a.get("/api/v1/forecast")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forecast = %d %s", resp.StatusCode, responseBody)
	}
	if decodeMap(t, responseBody)["source"] != "Open-Meteo" {
		t.Errorf("source = %v", decodeMap(t, responseBody)["source"])
	}

	resp, responseBody = a.get("/api/v1/status")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	forecast := decodeMap(t, responseBody)["forecast"].(map[string]any)
	if forecast["available"] != true {
		t.Fatalf("forecast status = %v", forecast)
	}
}

func TestForecastAPIWithoutStrings(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	resp, body := a.post("/api/v1/forecast/refresh", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d %s", resp.StatusCode, body)
	}
	snap := decodeMap(t, body)
	if snap["pv_total_kwh"].(float64) != 0 {
		t.Errorf("pv total = %v", snap["pv_total_kwh"])
	}
	warnings, _ := snap["warnings"].([]any)
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning.(string), "no PV strings") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestForecastAPIRequiresAuth(t *testing.T) {
	a := newTestAPI(t, nil)
	resp, body := a.get("/api/v1/forecast")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forecast without auth = %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/forecast/refresh", map[string]any{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh without auth = %d %s", resp.StatusCode, body)
	}
}
