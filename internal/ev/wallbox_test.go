package ev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWallbox stands in for api.wall-box.com.
type fakeWallbox struct {
	mu             sync.Mutex
	logins         int
	calls          []string
	bodies         []map[string]any
	status         int
	power          float64
	energy         float64
	rejectBadToken bool
}

func (f *fakeWallbox) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token/user", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.logins++
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"jwt": "jwt-token", "user_id": 42})
	})
	mux.HandleFunc("/chargers/status/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "status")
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer jwt-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status_id":      f.status,
			"charging_power": f.power,
			"added_energy":   f.energy,
			"config_data":    map[string]any{"max_available_current": 16},
		})
	})
	mux.HandleFunc("/v2/charger/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, "charger-put")
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/v3/chargers/", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.calls = append(f.calls, "remote-action")
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	})
	return mux
}

func newTestWallbox(t *testing.T, fake *fakeWallbox) *Wallbox {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	return NewWallbox(WallboxConfig{
		BaseURL:   server.URL,
		Email:     "owner@example.com",
		Password:  "secret",
		ChargerID: "9988552",
		HTTP:      server.Client(),
		Now:       time.Now,
	})
}

func TestWallboxStatusCharging(t *testing.T) {
	fake := &fakeWallbox{status: 194, power: 7.4, energy: 12.5}
	adapter := newTestWallbox(t, fake)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Charging || !status.Connected {
		t.Errorf("charging/connected = %v/%v", status.Charging, status.Connected)
	}
	if status.PowerW != 7400 {
		t.Errorf("PowerW = %v, want 7400", status.PowerW)
	}
	if status.SessionKWh != 12.5 {
		t.Errorf("SessionKWh = %v", status.SessionKWh)
	}
	if status.CurrentA != 16 {
		t.Errorf("CurrentA = %v, want 16", status.CurrentA)
	}
	if status.Mode != "charging" {
		t.Errorf("mode = %q", status.Mode)
	}
}

func TestWallboxStatusIdleWhenUnplugged(t *testing.T) {
	fake := &fakeWallbox{status: 0}
	adapter := newTestWallbox(t, fake)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Connected || status.Charging {
		t.Errorf("connected/charging = %v/%v, want false", status.Connected, status.Charging)
	}
	if status.Mode != "idle" {
		t.Errorf("mode = %q, want idle", status.Mode)
	}
}

func TestWallboxPausedState(t *testing.T) {
	fake := &fakeWallbox{status: 178}
	adapter := newTestWallbox(t, fake)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Paused || status.Mode != "paused" {
		t.Errorf("paused/mode = %v/%q", status.Paused, status.Mode)
	}
}

func TestWallboxSetCurrentClampsToRange(t *testing.T) {
	fake := &fakeWallbox{status: 193}
	adapter := newTestWallbox(t, fake)

	if err := adapter.SetCurrent(context.Background(), 40); err != nil {
		t.Fatalf("set current: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 1 {
		t.Fatalf("bodies = %v", fake.bodies)
	}
	if got := fake.bodies[0]["maxChargingCurrent"]; got != float64(32) {
		t.Fatalf("maxChargingCurrent = %v, want 32", got)
	}
}

func TestWallboxPauseAndResumeUseRemoteActions(t *testing.T) {
	fake := &fakeWallbox{status: 193}
	adapter := newTestWallbox(t, fake)

	if err := adapter.Pause(context.Background()); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := adapter.Resume(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.bodies) != 2 {
		t.Fatalf("bodies = %v", fake.bodies)
	}
	if fake.bodies[0]["action"] != float64(2) || fake.bodies[1]["action"] != float64(1) {
		t.Fatalf("actions = %v", fake.bodies)
	}
}
