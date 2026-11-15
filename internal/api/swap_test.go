package api

import (
	"net/http"
	"testing"
)

// TestInverterSwapNeedsConfirmation covers the guard around replacing hardware:
// an identity change is refused until the caller acknowledges what will happen,
// and acknowledging disarms writes for the new device.
func TestInverterSwapNeedsConfirmation(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	// Changing where the inverter lives is a swap, not an ordinary edit.
	csrf := a.ensureCSRF()
	resp, body := a.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"section": "inverter",
		"values":  map[string]any{"host": "10.0.0.5"},
	}, csrf)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("unconfirmed swap: %d %s", resp.StatusCode, body)
	}
	payload := decodeMap(t, body)
	errBody, _ := payload["error"].(map[string]any)
	if errBody == nil || errBody["code"] != "inverter_swap_required" {
		t.Fatalf("error = %v", payload["error"])
	}

	// Confirming applies it.
	resp, body = a.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"section":      "inverter",
		"values":       map[string]any{"host": "10.0.0.5"},
		"confirm_swap": true,
	}, csrf)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirmed swap: %d %s", resp.StatusCode, body)
	}

	// Writes are disarmed so the new device cannot be driven until re-armed.
	resp, body = a.get("/api/v1/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", resp.StatusCode, body)
	}
	safety := decodeMap(t, body)["safety"].(map[string]any)
	if safety["writes_armed_ui"] != false {
		t.Fatalf("writes_armed_ui = %v, want false after a swap", safety["writes_armed_ui"])
	}
}

// TestInverterEditIsNotASwap checks that tuning a limit does not trigger the
// replacement ceremony.
func TestInverterEditIsNotASwap(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	resp, body := a.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"section": "inverter",
		"values":  map[string]any{"max_charge_w": 5000},
	}, a.ensureCSRF())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("limit edit: %d %s", resp.StatusCode, body)
	}
}
