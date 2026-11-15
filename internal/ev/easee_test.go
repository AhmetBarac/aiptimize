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

// fakeEasee stands in for api.easee.com.
type fakeEasee struct {
	mu       sync.Mutex
	logins   int
	commands []string
	bodies   []map[string]any
	state    string
	expired  bool
}

func (f *fakeEasee) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/login", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.logins++
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"accessToken": "token-1", "refreshToken": "refresh-1", "expiresIn": 3600,
		})
	})
	mux.HandleFunc("/api/accounts/refresh_token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"accessToken": "token-2", "refreshToken": "refresh-2", "expiresIn": 3600,
		})
	})
	mux.HandleFunc("/api/chargers/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.expired = false
		f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer token-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/state") || strings.HasSuffix(r.URL.Path, "/observations") {
			w.Write([]byte(f.state))
			return
		}
		if strings.Contains(r.URL.Path, "/commands/") {
			command := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.commands = append(f.commands, command)
			f.bodies = append(f.bodies, body)
			f.mu.Unlock()
			w.Write([]byte(`{"id":1,"status":0}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	return mux
}

func newTestEasee(t *testing.T, fake *fakeEasee) *Easee {
	t.Helper()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)
	return NewEasee(EaseeConfig{
		BaseURL:   server.URL,
		UserName:  "driver@example.com",
		Password:  "secret",
		ChargerID: "EH123456",
		HTTP:      server.Client(),
		Now:       time.Now,
	})
}

const easeeChargingState = `{"observations":[
	{"id":100,"value":"C"},
	{"id":120,"value":7.2},
	{"id":121,"value":4.55},
	{"id":114,"value":16.0},
	{"id":31,"value":true},
	{"id":250,"value":true}
]}`

func TestEaseeStatusParsesObservations(t *testing.T) {
	fake := &fakeEasee{state: easeeChargingState}
	adapter := newTestEasee(t, fake)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.ID != "EH123456" {
		t.Errorf("ID = %q", status.ID)
	}
	if !status.Online || !status.Connected {
		t.Errorf("online/connected = %v/%v, want true/true", status.Online, status.Connected)
	}
	if status.PowerW != 7200 {
		t.Errorf("PowerW = %v, want 7200", status.PowerW)
	}
	if status.SessionKWh != 4.55 {
		t.Errorf("SessionKWh = %v, want 4.55", status.SessionKWh)
	}
	if status.CurrentA != 16 {
		t.Errorf("CurrentA = %v, want 16", status.CurrentA)
	}
	if status.Mode != "charging" || !status.Charging {
		t.Errorf("mode = %q charging = %v", status.Mode, status.Charging)
	}
}

func TestEaseeStatusIdleWhenNoCar(t *testing.T) {
	fake := &fakeEasee{state: `{"observations":[{"id":100,"value":"A"},{"id":31,"value":true},{"id":250,"value":true}]}`}
	adapter := newTestEasee(t, fake)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Connected || status.Charging {
		t.Errorf("connected/charging = %v/%v, want false/false", status.Connected, status.Charging)
	}
	if status.Mode != "idle" {
		t.Errorf("mode = %q, want idle", status.Mode)
	}
}

func TestEaseeSetCurrentSendsAmps(t *testing.T) {
	fake := &fakeEasee{state: easeeChargingState}
	adapter := newTestEasee(t, fake)

	if err := adapter.SetCurrent(context.Background(), 10.4); err != nil {
		t.Fatalf("set current: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.commands) != 1 || fake.commands[0] != "set_dynamic_charger_current" {
		t.Fatalf("commands = %v", fake.commands)
	}
	amps, _ := fake.bodies[0]["amps"].(float64)
	if amps != 10 {
		t.Fatalf("amps = %v, want 10", fake.bodies[0]["amps"])
	}
	if _, ok := fake.bodies[0]["minutes"]; !ok {
		t.Fatalf("minutes missing from %v", fake.bodies[0])
	}
}

func TestEaseePauseAndResumeMapToStopAndStart(t *testing.T) {
	fake := &fakeEasee{state: easeeChargingState}
	adapter := newTestEasee(t, fake)

	if err := adapter.Pause(context.Background()); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := adapter.Resume(context.Background()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	got := strings.Join(fake.commands, ",")
	if got != "stop_charging,start_charging" {
		t.Fatalf("commands = %q", got)
	}
}

func TestEaseeReauthenticatesOnUnauthorized(t *testing.T) {
	fake := &fakeEasee{state: easeeChargingState}
	adapter := newTestEasee(t, fake)

	// First read logs in.
	if _, err := adapter.Status(context.Background()); err != nil {
		t.Fatalf("first status: %v", err)
	}
	// Expire the token: the adapter must log in again rather than fail.
	adapter.mu.Lock()
	adapter.expires = time.Now().Add(-time.Hour)
	adapter.token = ""
	adapter.mu.Unlock()

	if _, err := adapter.Status(context.Background()); err != nil {
		t.Fatalf("second status: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.logins < 2 {
		t.Fatalf("logins = %d, want at least 2", fake.logins)
	}
}
