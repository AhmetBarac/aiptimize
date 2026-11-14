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

func TestZaptecAdapter(t *testing.T) {
	now := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	updates := 0
	commands := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
		case "/api/chargers":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Data": []map[string]any{{
					"Id": "ch1", "MID": "ch1", "DeviceId": "ch1",
					"InstallationId": "inst1", "IsOnline": true,
				}},
			})
		case "/api/chargers/ch1/state":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Observations": map[string]any{"553": 12.5, "513": 4140, "708": 6, "519": 3, "718": 0},
			})
		case "/api/installation/inst1/update":
			mu.Lock()
			updates++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/api/chargers/ch1/sendCommand/506", "/api/chargers/ch1/sendCommand/507":
			mu.Lock()
			commands++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	z := NewZaptec(ZaptecConfig{
		BaseURL: srv.URL, Username: "u", Password: "p",
		InstallationID: "inst1", ChargerID: "ch1",
		Now: func() time.Time { return now },
	})
	status, err := z.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.SessionKWh != 12.5 || status.PowerW != 4140 || status.CurrentA != 6 {
		t.Fatalf("status = %+v", status)
	}
	if err := z.SetCurrent(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if err := z.SetCurrent(context.Background(), 12); err == nil {
		t.Fatal("expected 15-minute rate limit")
	}
	if err := z.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := z.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if updates != 1 || commands != 2 {
		t.Fatalf("updates=%d commands=%d", updates, commands)
	}
}

func TestZaptecFailureDescribesEcoMode(t *testing.T) {
	body := []byte(`{"Code":527,"Details":"Cannot update installation when using eco mode"}`)
	got := zaptecFailure(http.StatusInternalServerError, body)

	if !strings.Contains(got, "527") || !strings.Contains(got, "eco mode") {
		t.Fatalf("eco mode failure = %q", got)
	}
	if !strings.Contains(got, "disable eco mode") {
		t.Fatalf("eco mode failure lacks the hint: %q", got)
	}
	if plain := zaptecFailure(http.StatusBadGateway, []byte("nope")); plain != "status 502" {
		t.Fatalf("opaque failure = %q", plain)
	}
}
