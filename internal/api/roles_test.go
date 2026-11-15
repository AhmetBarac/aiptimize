package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestRoleGates walks the two roles end to end: an administrator may manage
// users and settings, a normal user may do everything else but Settings.
func TestRoleGates(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	// The seeded account is an administrator.
	resp, body := a.get("/api/v1/auth/me")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
	me := decodeMap(t, body)["user"].(map[string]any)
	if me["role"] != "admin" {
		t.Fatalf("seeded role = %v", me["role"])
	}

	// Creating a user requires admin: it works now, and fails once we are the user.
	resp, body = a.post("/api/v1/users", map[string]any{
		"username": "anna", "password": "another-secret-1", "role": "user",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user: %d %s", resp.StatusCode, body)
	}

	resp, body = a.get("/api/v1/users")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "anna") {
		t.Fatalf("list users: %d %s", resp.StatusCode, body)
	}

	// Sign in as the normal user.
	a.post("/api/v1/auth/logout", map[string]any{})
	resp, body = a.post("/api/v1/auth/login", map[string]any{
		"username": "anna", "password": "another-secret-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login as user: %d %s", resp.StatusCode, body)
	}

	// Settings are closed.
	resp, body = a.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"section": "site", "values": map[string]any{"name": "Nope"},
	}, a.ensureCSRF())
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user patched settings: %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/users")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user listed users: %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/system/restart", map[string]any{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user restarted the service: %d %s", resp.StatusCode, body)
	}

	// Everything outside Settings stays open.
	for _, path := range []string{"/api/v1/status", "/api/v1/chargers", "/api/v1/prices", "/api/v1/plan"} {
		resp, body = a.get(path)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			t.Fatalf("user blocked from %s: %d %s", path, resp.StatusCode, body)
		}
	}
}
