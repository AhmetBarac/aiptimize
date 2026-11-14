package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/ev"
	"github.com/AhmetBarac/aiptimize/internal/service"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func TestEVChargerAndPlan(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
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

// failingCharger is a live simulator whose status poll always fails.
type failingCharger struct{ ev.Charger }

func (failingCharger) Status(context.Context) (domain.ChargerStatus, error) {
	return domain.ChargerStatus{}, errors.New("charger unreachable")
}

// newEVService rebuilds the server's EV service with another adapter.
func (a *testAPI) newEVService(charger ev.Charger) *service.EVService {
	a.t.Helper()
	return service.NewEVServiceWithCharger(a.settings, a.store, a.prices, a.forecast,
		slog.New(slog.NewTextHandler(io.Discard, nil)), charger, config.EVControlAuto, time.Now)
}

func (a *testAPI) metaGates() map[string]any {
	a.t.Helper()
	resp, body := a.get("/api/v1/meta")
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("meta = %d %s", resp.StatusCode, body)
	}
	return decodeMap(a.t, body)["gates"].(map[string]any)
}

func TestEVControlAutoGateFollowsChargerHealth(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"ev_control_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}

	gates := a.metaGates()
	if gates["ev_control_mode"] != "auto" {
		t.Errorf("mode = %v", gates["ev_control_mode"])
	}
	if gates["ev_control_env"] != false {
		t.Errorf("the link gate must stay closed until the charger answers: %v", gates)
	}

	// One successful status poll proves the charger link works and opens the
	// environment half of the gate without any environment variable.
	resp, body := a.get("/api/v1/chargers/sim-ev-1/status")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	gates = a.metaGates()
	if gates["ev_control_env"] != true || gates["ev_control_armed"] != true {
		t.Fatalf("gates after a healthy poll = %v", gates)
	}
	if resp, body = a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mode = %d %s", resp.StatusCode, body)
	}

	// A failed poll closes the gate again; the UI arm switch cannot reopen it.
	a.srv.ev = a.newEVService(failingCharger{Charger: a.ev.Charger()})
	if resp, body = a.get("/api/v1/chargers/sim-ev-1/status"); resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("failing status = %d %s", resp.StatusCode, body)
	}
	gates = a.metaGates()
	if gates["ev_control_env"] != false || gates["ev_control_armed"] != false {
		t.Fatalf("gates after a failed poll = %v", gates)
	}
	if resp, body = a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("mode while the link is down = %d %s", resp.StatusCode, body)
	}
}

func TestEVControlAutoGateStillRespectsArmSwitch(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	if resp, body := a.get("/api/v1/chargers/sim-ev-1/status"); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	gates := a.metaGates()
	if gates["ev_control_env"] != true {
		t.Fatalf("link gate = %v", gates["ev_control_env"])
	}
	if gates["ev_control_ui"] != false || gates["ev_control_armed"] != false {
		t.Fatalf("a healthy link must not arm control by itself: %v", gates)
	}
	resp, body := a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "ev_control_disarmed") {
		t.Fatalf("mode with the arm switch off = %d %s", resp.StatusCode, body)
	}

	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"ev_control_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	if resp, body = a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("mode after arming = %d %s", resp.StatusCode, body)
	}
}

func TestEVControlForcedModeOverridesChargerHealth(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"ev_control_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	if resp, body := a.get("/api/v1/chargers/sim-ev-1/status"); resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}

	// An operator can still pin the gate closed even when the charger is fine.
	a.srv.cfg.EVControlMode = config.EVControlOff
	gates := a.metaGates()
	if gates["ev_control_env"] != false || gates["ev_control_armed"] != false {
		t.Fatalf("forced-off gates = %v", gates)
	}
	if resp, body := a.post("/api/v1/chargers/sim-ev-1/mode", map[string]any{"mode": "now"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("mode while forced off = %d %s", resp.StatusCode, body)
	}

	// ... and pin it open regardless of the link state.
	a.srv.cfg.EVControlMode = config.EVControlOn
	gates = a.metaGates()
	if gates["ev_control_env"] != true || gates["ev_control_armed"] != true {
		t.Fatalf("forced-on gates = %v", gates)
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
