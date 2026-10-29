package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestHistoryAPI(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	base := time.Now().Add(-6 * time.Hour).Truncate(30 * time.Minute)
	for i := 0; i < 12; i++ {
		err := a.store.InsertTelemetry(context.Background(), domain.Telemetry{
			TS:  base.Add(time.Duration(i) * 30 * time.Minute),
			SOC: 50, PVW: 3000, LoadW: 1000, GridW: -1500, BatteryW: -500, EVW: 0,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	resp, body := a.get("/api/v1/history?resolution=day")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history = %d %s", resp.StatusCode, body)
	}
	snapshot := decodeMap(t, body)
	buckets := snapshot["buckets"].([]any)
	if len(buckets) == 0 {
		t.Fatalf("no buckets: %s", body)
	}
	totals := snapshot["totals"].(map[string]any)
	if totals["pv_kwh"].(float64) <= 0 || totals["load_kwh"].(float64) <= 0 {
		t.Errorf("totals = %v", totals)
	}
}

func TestMetricsAndSettingsTransfer(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	resp, body := a.get("/api/v1/metrics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics = %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "aiptimize_up 1") {
		t.Fatalf("metrics body = %s", body)
	}

	resp, body = a.get("/api/v1/settings/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export = %d %s", resp.StatusCode, body)
	}
	exported := decodeMap(t, body)
	exported["site"] = map[string]any{"name": "Imported Home"}
	resp, body = a.post("/api/v1/settings/import", exported)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import = %d %s", resp.StatusCode, body)
	}
	if got := a.settings.Get().Site.Name; got != "Imported Home" {
		t.Errorf("site name = %q", got)
	}
}
