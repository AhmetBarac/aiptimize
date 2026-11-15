// Package ev: the Wallbox adapter talks to the Wallbox cloud API.
//
// Wallbox issues a short-lived JWT (about a minute) from a Basic-auth token
// endpoint, so the adapter keeps a token for a little under that and refreshes
// on demand. Charging is controlled through remote actions and a current limit,
// both of which the charger applies through its own connection to the cloud,
// which means control needs internet.
package ev

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// DefaultWallboxBaseURL is the hosted cloud API.
const DefaultWallboxBaseURL = "https://api.wall-box.com"

// WallboxConfig configures the adapter.
type WallboxConfig struct {
	BaseURL   string
	Email     string
	Password  string
	ChargerID string
	UserAgent string
	HTTP      *http.Client
	Now       func() time.Time
}

// Wallbox is a charger reachable through the Wallbox cloud.
type Wallbox struct {
	cfg WallboxConfig

	mu      sync.Mutex
	token   string
	expires time.Time
	userID  int64
	paused  bool
}

// NewWallbox builds the adapter.
func NewWallbox(cfg WallboxConfig) *Wallbox {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultWallboxBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Wallbox{cfg: cfg}
}

// ID returns the configured charger id.
func (w *Wallbox) ID() string { return strings.TrimSpace(w.cfg.ChargerID) }

// Type names the provider as it appears in settings.
func (w *Wallbox) Type() string { return "wallbox" }

// Capabilities describes the charger. Wallbox exposes an explicit pause action
// and a per-charger current limit.
func (w *Wallbox) Capabilities() Capabilities {
	return Capabilities{
		MinCurrentA: 6, MaxCurrentA: 32, Phases: 3,
		CurrentScope:   "charger",
		SupportsPause:  true,
		SupportsPhases: false,
		ControlLatency: 15 * time.Second,
	}
}

// Close releases nothing: the adapter only holds a token.
func (w *Wallbox) Close() error { return nil }

// ---------------------------------------------------------------- transport --

// tokenLocked fetches a JWT. Wallbox tokens live about a minute, so the cached
// one is kept for 45 seconds at most.
func (w *Wallbox) tokenLocked(ctx context.Context) (string, error) {
	if w.token != "" && w.cfg.Now().Before(w.expires) {
		return w.token, nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte(w.cfg.Email + ":" + w.cfg.Password))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.BaseURL+"/auth/token/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Accept", "application/json")
	if w.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", w.cfg.UserAgent)
	}
	resp, err := w.cfg.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("wallbox: login HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var out struct {
		JWT    string `json:"jwt"`
		UserID int64  `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", fmt.Errorf("wallbox: decode login: %w", err)
	}
	if out.JWT == "" {
		return "", errors.New("wallbox: login returned no token")
	}
	w.token = out.JWT
	w.userID = out.UserID
	w.expires = w.cfg.Now().Add(45 * time.Second)
	return w.token, nil
}

// request performs one authenticated call, refreshing the token once on 401.
func (w *Wallbox) request(ctx context.Context, method, path string, body, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := func() (string, error) {
			w.mu.Lock()
			defer w.mu.Unlock()
			if attempt > 0 {
				w.token = ""
			}
			return w.tokenLocked(ctx)
		}()
		if err != nil {
			return err
		}
		status, payload, err := w.send(ctx, method, path, body, token)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			continue
		}
		if status < 200 || status > 299 {
			return fmt.Errorf("wallbox: %s %s: HTTP %d: %s", method, path, status, payload)
		}
		if out != nil && len(payload) > 0 {
			if err := json.Unmarshal(payload, out); err != nil {
				return fmt.Errorf("wallbox: decode %s: %w", path, err)
			}
		}
		return nil
	}
	return errors.New("wallbox: unauthorized after refreshing the token")
}

func (w *Wallbox) send(ctx context.Context, method, path string, body any, token string) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.cfg.BaseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if w.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", w.cfg.UserAgent)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := w.cfg.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, payload, nil
}

// ------------------------------------------------------------------- status --

// wallboxStatusCodes groups the charger states Wallbox reports.
var (
	wallboxCharging     = map[int]bool{193: true, 194: true, 195: true, 196: true}
	wallboxPaused       = map[int]bool{178: true, 182: true}
	wallboxNotConnected = map[int]bool{0: true, 163: true}
	wallboxError        = map[int]bool{14: true, 15: true}
)

// wallboxStatus is the subset of the extended status payload we use.
type wallboxStatus struct {
	StatusID      int     `json:"status_id"`
	ChargingPower float64 `json:"charging_power"`
	AddedEnergy   float64 `json:"added_energy"`
	Finished      bool    `json:"finished"`
	Name          string  `json:"name"`
	ChargingSpeed float64 `json:"charging_speed"`
	MaxAvailable  float64 `json:"max_available_power"`
	Config        struct {
		Locked             int     `json:"locked"`
		MaxChargingCurrent float64 `json:"max_charging_current"`
		MaxAvailable       float64 `json:"max_available_current"`
	} `json:"config_data"`
}

// Status reads the charger state.
func (w *Wallbox) Status(ctx context.Context) (domain.ChargerStatus, error) {
	chargerID := w.ID()
	if chargerID == "" {
		return domain.ChargerStatus{}, errors.New("wallbox: charger id is not configured")
	}
	var raw wallboxStatus
	path := "/chargers/status/" + chargerID
	if err := w.request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return domain.ChargerStatus{}, err
	}

	status := domain.ChargerStatus{
		ID:        chargerID,
		Type:      "wallbox",
		Online:    true,
		UpdatedAt: w.cfg.Now().UTC(),
	}
	status.PowerW = raw.ChargingPower * 1000
	status.SessionKWh = raw.AddedEnergy
	if raw.Config.MaxAvailable > 0 {
		status.CurrentA = raw.Config.MaxAvailable
	} else if raw.MaxAvailable > 0 {
		status.CurrentA = raw.MaxAvailable
	}
	status.Phases = 3

	switch {
	case wallboxError[raw.StatusID]:
		status.Mode = "error"
	case wallboxCharging[raw.StatusID]:
		status.Mode = "charging"
		status.Charging = true
		status.Connected = true
	case wallboxPaused[raw.StatusID]:
		status.Mode = "paused"
		status.Connected = true
		status.Paused = true
	case wallboxNotConnected[raw.StatusID]:
		status.Mode = "idle"
	default:
		// Ready, scheduled, waiting for car demand, power sharing, locked.
		status.Mode = "connected"
		status.Connected = true
	}
	if raw.Config.Locked == 1 && status.Mode == "connected" {
		status.Mode = "locked"
	}
	w.mu.Lock()
	if !status.Paused {
		status.Paused = w.paused
	}
	w.paused = status.Paused
	w.mu.Unlock()
	return status, nil
}

// ----------------------------------------------------------------- commands --

// SetCurrent sets the charger's current limit in amps.
func (w *Wallbox) SetCurrent(ctx context.Context, amps float64) error {
	chargerID := w.ID()
	if chargerID == "" {
		return errors.New("wallbox: charger id is not configured")
	}
	if amps < 6 {
		amps = 6
	}
	if amps > 32 {
		amps = 32
	}
	body := map[string]any{"maxChargingCurrent": int(amps + 0.5)}
	path := "/v2/charger/" + chargerID
	return w.request(ctx, http.MethodPut, path, body, nil)
}

// Pause stops charging through the charger's pause action.
func (w *Wallbox) Pause(ctx context.Context) error {
	if err := w.action(ctx, 2); err != nil {
		return err
	}
	w.mu.Lock()
	w.paused = true
	w.mu.Unlock()
	return nil
}

// Resume returns the charger to its normal mode.
func (w *Wallbox) Resume(ctx context.Context) error {
	if err := w.action(ctx, 1); err != nil {
		return err
	}
	w.mu.Lock()
	w.paused = false
	w.mu.Unlock()
	return nil
}

// StopSession pauses the charger, which is how a session is ended remotely.
func (w *Wallbox) StopSession(ctx context.Context) error { return w.Pause(ctx) }

// action sends a remote action: 1 resume, 2 pause.
func (w *Wallbox) action(ctx context.Context, value int) error {
	chargerID := w.ID()
	if chargerID == "" {
		return errors.New("wallbox: charger id is not configured")
	}
	path := "/v3/chargers/" + chargerID + "/remote-action"
	return w.request(ctx, http.MethodPost, path, map[string]any{"action": value}, nil)
}

// Lock engages or releases the cable lock, which also stops charging when set.
func (w *Wallbox) Lock(ctx context.Context, locked bool) error {
	chargerID := w.ID()
	if chargerID == "" {
		return errors.New("wallbox: charger id is not configured")
	}
	value := 0
	if locked {
		value = 1
	}
	path := "/v2/charger/" + chargerID
	return w.request(ctx, http.MethodPut, path, map[string]any{"locked": value}, nil)
}

// keep strconv referenced for future numeric parsing of ids.
var _ = strconv.Itoa
