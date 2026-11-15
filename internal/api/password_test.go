package api

import (
	"net/http"
	"testing"
)

// TestChangePassword covers the self-service flow: a wrong current password is
// rejected, a correct one rotates the credentials, and the old password stops
// working.
func TestChangePassword(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	resp, body := a.post("/api/v1/auth/password", map[string]any{
		"current_password": "wrong-password", "new_password": "brand-new-secret-1",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong current password: %d %s", resp.StatusCode, body)
	}

	resp, body = a.post("/api/v1/auth/password", map[string]any{
		"current_password": "very-secret-1", "new_password": "short",
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("short password accepted: %d %s", resp.StatusCode, body)
	}

	resp, body = a.post("/api/v1/auth/password", map[string]any{
		"current_password": "very-secret-1", "new_password": "brand-new-secret-1",
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password: %d %s", resp.StatusCode, body)
	}

	// The caller stays signed in.
	if resp, body = a.get("/api/v1/auth/me"); resp.StatusCode != http.StatusOK {
		t.Fatalf("session lost after password change: %d %s", resp.StatusCode, body)
	}

	a.post("/api/v1/auth/logout", map[string]any{})
	resp, _ = a.post("/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "very-secret-1",
	})
	if resp.StatusCode == http.StatusOK {
		t.Fatal("the old password still works")
	}
	resp, body = a.post("/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "brand-new-secret-1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("new password rejected: %d %s", resp.StatusCode, body)
	}
}
