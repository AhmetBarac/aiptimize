package ev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Eco mode is installation feature bit 8 (PowerManagement_EcoMode). SurplusMode
// is a different feature and must not be mistaken for it — that mistake let the
// banner flicker while Zaptec kept refusing current updates.
func TestZaptecEcoModeReadsFeatureBit(t *testing.T) {
	cases := []struct {
		name     string
		features int
		want     bool
	}{
		{"eco enabled", 8, true},
		{"eco enabled with other features", 8 | 4 | 16, true},
		{"eco off", 4 | 16, false},
		{"only surplus mode", 131072, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth/token":
					w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
				case "/api/installation/inst-1":
					w.Write([]byte(`{"EnabledFeatures":` + itoa(tc.features) + `,"SurplusMode":{"Active":false}}`))
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

			got, err := adapter.EcoMode(context.Background())
			if err != nil {
				t.Fatalf("eco mode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("eco mode = %v, want %v for features %d", got, tc.want, tc.features)
			}
		})
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
