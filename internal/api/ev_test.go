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

func TestEVZaptecCredentialsStoredAndHidden(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	resp, body := a.do(http.MethodPut, "/api/v1/ev/config", map[string]any{
		"provider": "zaptec", "charger_id": "ch-1", "installation_id": "inst-1",
		"zaptec_username": "user@example.com", "zaptec_password": "secret-password",
	}, a.csrf())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply credentials = %d %s", resp.StatusCode, body)
	}
	cfg := decodeMap(t, body)
	creds := cfg["credentials"].(map[string]any)
	if creds["zaptec"] != true {
		t.Errorf("zaptec credentials = %v", creds)
	}
	if a.settings.Get().EV.ZaptecPassword != "secret-password" {
		t.Errorf("password was not stored")
	}
	resp, body = a.get("/api/v1/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings = %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "secret-password") {
		t.Fatal("settings API exposed the Zaptec password")
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

func TestEVConfigAndRestartState(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	resp, body := a.get("/api/v1/ev/config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config = %d %s", resp.StatusCode, body)
	}
	cfg := decodeMap(t, body)
	if cfg["provider"] != "zaptec" {
		t.Errorf("provider = %v", cfg["provider"])
	}
	if cfg["active_provider"] != "simulator" {
		t.Errorf("active provider = %v", cfg["active_provider"])
	}

	resp, body = a.do(http.MethodPut, "/api/v1/ev/config", map[string]any{
		"provider": "simulator", "charger_id": "sim-ev-1",
	}, a.csrf())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply config = %d %s", resp.StatusCode, body)
	}
	cfg = decodeMap(t, body)
	if cfg["restart_required"] != true {
		t.Errorf("restart_required = %v", cfg["restart_required"])
	}
	ev := a.settings.Get().EV
	if ev.Provider != "simulator" || ev.ChargerID != "sim-ev-1" {
		t.Errorf("saved EV = %+v", ev)
	}

	resp, body = a.post("/api/v1/system/restart", map[string]any{})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("restart without callback = %d %s", resp.StatusCode, body)
	}

	called := make(chan struct{}, 1)
	a.srv.requestRestart = func() { called <- struct{}{} }
	resp, body = a.post("/api/v1/system/restart", map[string]any{})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restart = %d %s", resp.StatusCode, body)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("restart callback was not called")
	}
}
