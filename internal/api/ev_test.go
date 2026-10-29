package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func TestEVChargerAndPlan(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	a.srv.cfg.EnableEVControl = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"ev_control_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Patch(context.Background(), settings.SectionEV, []byte(`{"grid_charge_allowed":true,"mode":"smart"}`)); err != nil {
		t.Fatal(err)
	}

	resp, body := a.get("/api/v1/chargers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chargers = %d %s", resp.StatusCode, body)
	}
	chargers := decodeMap(t, body)["chargers"].([]any)
	if len(chargers) != 1 {
		t.Fatalf("chargers = %v", chargers)
	}
	id := chargers[0].(map[string]any)["id"].(string)

	resp, body = a.get("/api/v1/chargers/" + id + "/status")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}

	resp, body = a.post("/api/v1/chargers/"+id+"/mode", map[string]any{"mode": "smart"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mode = %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/chargers/"+id+"/current", map[string]any{"amps": 10})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("current = %d %s", resp.StatusCode, body)
	}

	deadline := time.Now().Add(24 * time.Hour).Format("2006-01-02T15:04")
	resp, body = a.post("/api/v1/chargers/"+id+"/plan/static", map[string]any{
		"target_energy_kwh": 20, "deadline": deadline, "strategy": "cheapest", "active": true,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("plan = %d %s", resp.StatusCode, body)
	}
	plan := decodeMap(t, body)
	preview := plan["preview"].(map[string]any)
	if len(preview["windows"].([]any)) == 0 {
		t.Fatalf("preview = %v", preview)
	}
	if preview["state"] != "scheduled" {
		t.Errorf("state = %v warnings=%v", preview["state"], preview["warnings"])
	}

	resp, body = a.get("/api/v1/chargers/" + id + "/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan get = %d %s", resp.StatusCode, body)
	}
	if len(decodeMap(t, body)["plans"].([]any)) != 1 {
		t.Errorf("plans = %s", body)
	}
}

func TestEVControlDisarmed(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	resp, body := a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "ev_control_disarmed") {
		t.Fatalf("disarmed mode = %d %s", resp.StatusCode, body)
	}
}
