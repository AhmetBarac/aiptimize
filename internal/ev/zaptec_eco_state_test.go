package ev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The installation payload reports the surplus (eco) mode as SurplusMode.Active;
// reading it lets the UI warn before a command is refused.
func TestZaptecEcoModeReadsSurplusMode(t *testing.T) {
	active := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case "/api/installation/inst-1":
			if active {
				w.Write([]byte(`{"SurplusMode":{"Active":true}}`))
				return
			}
			w.Write([]byte(`{"SurplusMode":{"Active":false}}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	adapter := NewZaptec(ZaptecConfig{
		BaseURL:        server.URL,
		Username:       "user",
		Password:       "pass",
		InstallationID: "inst-1",
		ChargerID:      "charger-1",
		HTTP:           server.Client(),
		Now:            time.Now,
	})

	on, err := adapter.EcoMode(context.Background())
	if err != nil {
		t.Fatalf("eco mode: %v", err)
	}
	if !on {
		t.Fatal("eco mode reported off while SurplusMode.Active is true")
	}

	// Cached for a minute, so flipping the remote state is not visible at once.
	active = false
	stillOn, _ := adapter.EcoMode(context.Background())
	if !stillOn {
		t.Fatal("cache should hold the previous answer within a minute")
	}

	// After the cache expires the new state is read.
	adapter.mu.Lock()
	adapter.ecoAt = time.Now().Add(-2 * time.Minute)
	adapter.mu.Unlock()
	off, err := adapter.EcoMode(context.Background())
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if off {
		t.Fatal("eco mode still reported on after SurplusMode.Active became false")
	}
}
