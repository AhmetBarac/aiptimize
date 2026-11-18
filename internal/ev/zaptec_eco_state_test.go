package ev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The answer is cached briefly so a double click on Retry does not call the
// vendor twice, while a page opened a few seconds later sees the new state.
func TestZaptecEcoModeCache(t *testing.T) {
	features := 8
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case "/api/installation/inst-1":
			w.Write([]byte(`{"EnabledFeatures":` + itoa(features) + `}`))
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
	if err != nil || !on {
		t.Fatalf("first read = %v, %v; want true", on, err)
	}

	// Eco mode switched off, but the cache still holds the old answer.
	features = 4
	cached, _ := adapter.EcoMode(context.Background())
	if !cached {
		t.Fatal("cache should hold the previous answer within its window")
	}

	// Once the window passes, the new state is read.
	adapter.mu.Lock()
	adapter.ecoAt = time.Now().Add(-time.Minute)
	adapter.mu.Unlock()
	off, err := adapter.EcoMode(context.Background())
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if off {
		t.Fatal("eco mode still reported on after the feature bit cleared")
	}
}
