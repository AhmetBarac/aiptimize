package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestTelemetryStatusAndInverterTest(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	if _, err := a.telemetry.PollOnce(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	resp, body := a.get("/api/v1/status")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	status := decodeMap(t, body)
	tel := status["telemetry"].(map[string]any)
	if tel["available"] != true || tel["stale"] != false {
		t.Fatalf("telemetry status = %v", tel)
	}
	sample := tel["sample"].(map[string]any)
	if sample["soc"].(float64) != 94 || sample["grid_w"].(float64) != 39 || sample["battery_w"].(float64) != -2187 {
		t.Fatalf("sample = %v", sample)
	}
	if checks := status["checks"].(map[string]any); checks["inverter"] != "ok" {
		t.Fatalf("checks = %v", checks)
	}

	resp, body = a.post("/api/v1/settings/test/inverter", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("test inverter = %d %s", resp.StatusCode, body)
	}
	result := decodeMap(t, body)
	identity := result["identity"].(map[string]any)
	if identity["model"] != "ASW08kH-T2" || identity["serial"] != "SIM000000000001" {
		t.Fatalf("identity = %v", identity)
	}
	telemetry := result["telemetry"].(map[string]any)
	if telemetry["soc"].(float64) != 94 {
		t.Fatalf("telemetry = %v", telemetry)
	}

	a.inverter.mu.Lock()
	a.inverter.err = errors.New("dongle offline")
	a.inverter.mu.Unlock()
	resp, body = a.post("/api/v1/settings/test/inverter", map[string]any{})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("test inverter failure = %d %s", resp.StatusCode, body)
	}
	errObj := decodeMap(t, body)["error"].(map[string]any)
	if errObj["code"] != "inverter_unreachable" {
		t.Fatalf("error = %v", errObj)
	}
}

func TestTestInverterRequiresAuth(t *testing.T) {
	a := newTestAPI(t, nil)
	resp, body := a.post("/api/v1/settings/test/inverter", map[string]any{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("test inverter without auth = %d %s", resp.StatusCode, body)
	}
}
