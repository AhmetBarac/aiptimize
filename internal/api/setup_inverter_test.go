package api

import (
	"net/http"
	"testing"
)

// TestSetupAcceptsInverter checks that the inverter, including its Modbus port,
// can be configured during first run instead of later in Settings.
func TestSetupAcceptsInverter(t *testing.T) {
	a := newTestAPI(t, nil)

	resp, body := a.post("/api/v1/setup", map[string]any{
		"username": "admin",
		"password": "very-secret-1",
		"site":     map[string]any{"name": "Test Home", "timezone": "UTC"},
		"inverter": map[string]any{
			"type":    "saj",
			"host":    "10.0.0.9",
			"port":    502,
			"unit_id": 1,
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}

	resp, body = a.get("/api/v1/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", resp.StatusCode, body)
	}
	inverter := decodeMap(t, body)["inverter"].(map[string]any)
	if inverter["type"] != "saj" {
		t.Errorf("type = %v, want saj", inverter["type"])
	}
	if inverter["host"] != "10.0.0.9" {
		t.Errorf("host = %v", inverter["host"])
	}
	if inverter["port"] != float64(502) {
		t.Errorf("port = %v, want 502", inverter["port"])
	}
	if inverter["unit_id"] != float64(1) {
		t.Errorf("unit_id = %v, want 1", inverter["unit_id"])
	}
}

// A Huawei plant on the alternative port must be accepted too.
func TestSetupAcceptsAlternativeModbusPort(t *testing.T) {
	a := newTestAPI(t, nil)

	resp, body := a.post("/api/v1/setup", map[string]any{
		"username": "admin",
		"password": "very-secret-1",
		"site":     map[string]any{"name": "Test Home", "timezone": "UTC"},
		"inverter": map[string]any{
			"type": "huawei", "host": "10.0.0.10", "port": 6607, "unit_id": 100,
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}

	resp, body = a.get("/api/v1/settings")
	inverter := decodeMap(t, body)["inverter"].(map[string]any)
	if inverter["port"] != float64(6607) {
		t.Errorf("port = %v, want 6607", inverter["port"])
	}
	if inverter["unit_id"] != float64(100) {
		t.Errorf("unit_id = %v, want 100", inverter["unit_id"])
	}
}

// An impossible unit id is rejected before the account exists, so a typo cannot
// leave a half-configured installation.
func TestSetupRejectsInvalidUnitID(t *testing.T) {
	a := newTestAPI(t, nil)

	resp, body := a.post("/api/v1/setup", map[string]any{
		"username": "admin",
		"password": "very-secret-1",
		"site":     map[string]any{"name": "Test Home", "timezone": "UTC"},
		"inverter": map[string]any{
			"type": "huawei", "host": "10.0.0.10", "port": 502, "unit_id": 300,
		},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("setup: %d %s, want 422", resp.StatusCode, body)
	}
}
