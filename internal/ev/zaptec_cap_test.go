package ev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeZaptecCap records what the adapter writes and serves a charger whose own
// maxChargeCurrent is below the requested value.
type fakeZaptecCap struct {
	mu           sync.Mutex
	chargerCap   string
	installation []map[string]any
	chargerBody  []map[string]any
}

func (f *fakeZaptecCap) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case "/api/chargers":
			w.Write([]byte(`{"Data":[{"Id":"charger-1","SerialNo":"ZAP1"}]}`))
		case "/api/chargers/charger-1/settings":
			f.mu.Lock()
			cap := f.chargerCap
			f.mu.Unlock()
			w.Write([]byte(`{"510":{"SettingId":510,"Value":"` + cap + `"}}`))
		case "/api/chargers/charger-1/update":
			var body map[string]any
			_ = decodeBody(r, &body)
			f.mu.Lock()
			f.chargerBody = append(f.chargerBody, body)
			if v, ok := body["maxChargeCurrent"].(float64); ok {
				f.chargerCap = formatAmps(v)
			}
			f.mu.Unlock()
			w.Write([]byte(`{}`))
		case "/api/installation/inst-1/update":
			var body map[string]any
			_ = decodeBody(r, &body)
			f.mu.Lock()
			f.installation = append(f.installation, body)
			f.mu.Unlock()
			w.Write([]byte(`{}`))
		default:
			w.Write([]byte(`{}`))
		}
	})
}

func TestZaptecRaisesChargerCapWhenItCapsBelowTarget(t *testing.T) {
	fake := &fakeZaptecCap{chargerCap: "6"}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	adapter := NewZaptec(ZaptecConfig{
		BaseURL:        server.URL,
		Username:       "u",
		Password:       "p",
		InstallationID: "inst-1",
		ChargerID:      "charger-1",
		HTTP:           server.Client(),
		Now:            time.Now,
	})

	if err := adapter.SetCurrent(context.Background(), 14); err != nil {
		t.Fatalf("set current: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.chargerBody) != 1 {
		t.Fatalf("charger updates = %v, want one raising the cap", fake.chargerBody)
	}
	if got := fake.chargerBody[0]["maxChargeCurrent"]; got != float64(14) {
		t.Fatalf("maxChargeCurrent = %v, want 14", got)
	}
	if len(fake.installation) != 1 || fake.installation[0]["AvailableCurrent"] != float64(14) {
		t.Fatalf("installation updates = %v", fake.installation)
	}
}

func TestZaptecLeavesChargerCapWhenItIsHigher(t *testing.T) {
	fake := &fakeZaptecCap{chargerCap: "32"}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()

	adapter := NewZaptec(ZaptecConfig{
		BaseURL:        server.URL,
		Username:       "u",
		Password:       "p",
		InstallationID: "inst-1",
		ChargerID:      "charger-1",
		HTTP:           server.Client(),
		Now:            time.Now,
	})

	if err := adapter.SetCurrent(context.Background(), 10); err != nil {
		t.Fatalf("set current: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.chargerBody) != 0 {
		t.Fatalf("charger was updated although its cap is higher: %v", fake.chargerBody)
	}
	if len(fake.installation) != 1 {
		t.Fatalf("installation updates = %v", fake.installation)
	}
}
