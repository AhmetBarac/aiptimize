package api

import (
	"errors"
	"net/http"
	"testing"
)

func TestUpdateApplyWritesRequestBeforeResponding(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	written := 0
	a.srv.requestUpdate = func() error {
		written++
		return nil
	}

	resp, body := a.post("/api/v1/system/update/apply", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}
	if written != 1 {
		t.Errorf("request writes = %d, want 1", written)
	}
	if decodeMap(t, body)["updating"] != true {
		t.Errorf("body = %s", body)
	}
}

func TestUpdateApplyRequiresConfirmation(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	written := 0
	a.srv.requestUpdate = func() error {
		written++
		return nil
	}

	resp, body := a.post("/api/v1/system/update/apply", map[string]any{"confirm": false})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}
	if written != 0 {
		t.Errorf("request writes = %d, want 0", written)
	}
	if code := decodeMap(t, body)["error"].(map[string]any)["code"]; code != "confirmation_required" {
		t.Errorf("code = %v", code)
	}
}

// A supervisor that cannot be reached must surface as an error so the UI does
// not sit waiting for a restart that was never requested.
func TestUpdateApplyReportsSupervisorFailure(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	a.srv.requestUpdate = func() error {
		return errors.New("write /var/lib/aiptimize/update-request: permission denied")
	}

	resp, body := a.post("/api/v1/system/update/apply", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}
	detail := decodeMap(t, body)["error"].(map[string]any)
	if detail["code"] != "update_failed" {
		t.Errorf("code = %v", detail["code"])
	}
	details, _ := detail["details"].(map[string]any)
	if details["error"] == nil || details["error"] == "" {
		t.Errorf("details = %v", detail["details"])
	}
}

func TestUpdateApplyUnavailableWhenNotSupervised(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	a.srv.requestUpdate = nil

	resp, body := a.post("/api/v1/system/update/apply", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}
	if code := decodeMap(t, body)["error"].(map[string]any)["code"]; code != "update_unavailable" {
		t.Errorf("code = %v", code)
	}
}
