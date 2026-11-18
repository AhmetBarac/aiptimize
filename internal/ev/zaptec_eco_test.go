package ev

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Eco mode makes Zaptec refuse every installation update with HTTP 500 and
// Code 527. The adapter must expose that as ErrEcoMode so callers stop retrying
// and the UI can explain what to change.
func TestZaptecEcoModeIsTyped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
		case r.URL.Path == "/api/installation/inst-1/update":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"Code":527,"Details":"Cannot update installation when using eco mode"}`))
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

	err := adapter.SetCurrent(context.Background(), 10)
	if !errors.Is(err, ErrEcoMode) {
		t.Fatalf("err = %v, want ErrEcoMode", err)
	}
}
