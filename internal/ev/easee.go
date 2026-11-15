// Package ev: the Easee adapter talks to the official Easee cloud API.
//
// Control goes through https://api.easee.com with an OAuth2 bearer token, so
// this adapter needs internet: there is no supported local API on the charger.
// Current limits are set as a volatile "dynamic charger current" that the
// charger clears on reboot or cable disconnect, which keeps a failed Aiptimize
// from leaving the car pinned at a high current.
package ev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// DefaultEaseeBaseURL is the hosted cloud API.
const DefaultEaseeBaseURL = "https://api.easee.com"

// EaseeConfig configures the adapter.
type EaseeConfig struct {
	BaseURL   string
	UserName  string
	Password  string
	ChargerID string // serial number; the first charger is used when empty
	UserAgent string
	HTTP      *http.Client
	Now       func() time.Time
}

// Easee is a charger reachable through the Easee cloud.
type Easee struct {
	cfg EaseeConfig

	mu       sync.Mutex
	token    string
	refresh  string
	expires  time.Time
	serial   string
	paused   bool
	lastMode string
}

// NewEasee builds the adapter.
func NewEasee(cfg EaseeConfig) *Easee {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultEaseeBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Easee{cfg: cfg}
}

// ID returns the charger serial.
func (e *Easee) ID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.serial
}

// Type names the provider as it appears in settings.
func (e *Easee) Type() string { return "easee" }

// Capabilities describes the charger. Easee has no pause, so pausing stops the
// session; the dynamic current is per charger rather than per installation.
func (e *Easee) Capabilities() Capabilities {
	return Capabilities{
		MinCurrentA: 6, MaxCurrentA: 32, Phases: 3,
		CurrentScope:   "charger",
		SupportsPause:  false,
		SupportsPhases: false,
		ControlLatency: 10 * time.Second,
	}
}

// Close releases nothing: the adapter is stateless besides its token.
func (e *Easee) Close() error { return nil }

// ---------------------------------------------------------------- transport --

type easeeCommand struct {
	Command     string
	StatusCode  int
	Description string
}

// tokenLocked makes sure a bearer token is available. Callers hold the mutex.
func (e *Easee) tokenLocked(ctx context.Context) (string, error) {
	if e.token != "" && e.cfg.Now().Before(e.expires) {
		return e.token, nil
	}
	if e.refresh != "" && e.token != "" {
		if err := e.refreshLocked(ctx); err == nil {
			return e.token, nil
		}
		// Fall back to a fresh login below.
	}
	if err := e.loginLocked(ctx); err != nil {
		return "", err
	}
	return e.token, nil
}

func (e *Easee) loginLocked(ctx context.Context) error {
	body := map[string]any{"userName": e.cfg.UserName, "password": e.cfg.Password}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := e.send(ctx, http.MethodPost, "/api/accounts/login", body, &out, ""); err != nil {
		return fmt.Errorf("easee: login: %w", err)
	}
	if out.AccessToken == "" {
		return fmt.Errorf("easee: login returned no access token")
	}
	e.token = out.AccessToken
	e.refresh = out.RefreshToken
	e.expires = e.cfg.Now().Add(time.Duration(out.ExpiresIn) * time.Second).Add(-2 * time.Minute)
	return nil
}

func (e *Easee) refreshLocked(ctx context.Context) error {
	body := map[string]any{"accessToken": e.token, "refreshToken": e.refresh}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := e.send(ctx, http.MethodPost, "/api/accounts/refresh_token", body, &out, ""); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return fmt.Errorf("easee: refresh returned no access token")
	}
	e.token = out.AccessToken
	if out.RefreshToken != "" {
		e.refresh = out.RefreshToken
	}
	e.expires = e.cfg.Now().Add(time.Duration(out.ExpiresIn) * time.Second).Add(-2 * time.Minute)
	return nil
}

// send performs one request, adding the bearer token when given.
func (e *Easee) send(ctx context.Context, method, path string, body, out any, token string) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.cfg.BaseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if e.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", e.cfg.UserAgent)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &easeeError{Status: resp.StatusCode, Body: strings.TrimSpace(string(payload))}
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("easee: decode %s: %w", path, err)
	}
	return nil
}

type easeeError struct {
	Status int
	Body   string
}

func (e *easeeError) Error() string {
	return fmt.Sprintf("easee: HTTP %d: %s", e.Status, e.Body)
}

// call performs an authenticated request, refreshing once on 401.
func (e *Easee) call(ctx context.Context, method, path string, body, out any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	token, err := e.tokenLocked(ctx)
	if err != nil {
		return err
	}
	err = e.send(ctx, method, path, body, out, token)
	var apiErr *easeeError
	if err != nil && errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		if refreshErr := e.loginLocked(ctx); refreshErr != nil {
			return err
		}
		return e.send(ctx, method, path, body, out, e.token)
	}
	return err
}

// resolveSerial finds the charger to talk to, preferring the configured serial.
func (e *Easee) resolveSerial(ctx context.Context) (string, error) {
	e.mu.Lock()
	cached := e.serial
	e.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	configured := strings.TrimSpace(e.cfg.ChargerID)
	if configured != "" {
		e.mu.Lock()
		e.serial = configured
		e.mu.Unlock()
		return configured, nil
	}
	var chargers []struct {
		SerialNumber string `json:"serialNumber"`
		ID           string `json:"id"`
	}
	if err := e.call(ctx, http.MethodGet, "/api/chargers", nil, &chargers); err != nil {
		return "", err
	}
	if len(chargers) == 0 {
		return "", fmt.Errorf("easee: the account has no chargers")
	}
	serial := chargers[0].SerialNumber
	if serial == "" {
		serial = chargers[0].ID
	}
	if serial == "" {
		return "", fmt.Errorf("easee: charger has no serial number")
	}
	e.mu.Lock()
	e.serial = serial
	e.mu.Unlock()
	return serial, nil
}

// ------------------------------------------------------------------- status --

// observation is one entry from the observations endpoint.
type observation struct {
	ID    int             `json:"id"`
	Value json.RawMessage `json:"value"`
}

// observations fetches the latest values by observation id.
func (e *Easee) observations(ctx context.Context) (map[int]json.RawMessage, error) {
	serial, err := e.resolveSerial(ctx)
	if err != nil {
		return nil, err
	}
	// The state endpoint carries the newest observations; the observations
	// endpoint is the fallback for firmware that only exposes that one.
	endpoints := []string{
		"/api/chargers/" + url.PathEscape(serial) + "/state",
		"/api/chargers/" + url.PathEscape(serial) + "/observations",
	}
	var lastErr error
	for _, path := range endpoints {
		var raw json.RawMessage
		if err := e.call(ctx, http.MethodGet, path, nil, &raw); err != nil {
			lastErr = err
			continue
		}
		if values := parseObservations(raw); len(values) > 0 {
			return values, nil
		}
		lastErr = fmt.Errorf("easee: %s returned no observations", path)
	}
	return nil, lastErr
}

// parseObservations accepts the shapes the API has used: an object with an
// "observations" array, a bare array, or a flat map of id to value.
func parseObservations(raw json.RawMessage) map[int]json.RawMessage {
	values := map[int]json.RawMessage{}
	var wrapper struct {
		Observations []observation `json:"observations"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Observations) > 0 {
		for _, item := range wrapper.Observations {
			values[item.ID] = item.Value
		}
		return values
	}
	var list []observation
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		for _, item := range list {
			values[item.ID] = item.Value
		}
		return values
	}
	var flat map[string]json.RawMessage
	if err := json.Unmarshal(raw, &flat); err == nil {
		for key, value := range flat {
			id, convErr := strconv.Atoi(key)
			if convErr != nil {
				continue
			}
			values[id] = value
		}
	}
	return values
}

// Observation ids used by the adapter (Easee "Charger Observation Ids").
const (
	obsIsEnabled        = 31
	obsMaxCurrent       = 47
	obsDynamicCurrent   = 48
	obsPilotMode        = 100
	obsOpMode           = 109
	obsOutputCurrent    = 114
	obsErrorCode        = 119
	obsTotalPowerKW     = 120
	obsSessionEnergy    = 121
	obsConnectedToCloud = 250
)

func obsFloat(values map[int]json.RawMessage, id int) (float64, bool) {
	raw, ok := values[id]
	if !ok {
		return 0, false
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func obsString(values map[int]json.RawMessage, id int) (string, bool) {
	raw, ok := values[id]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, true
	}
	return "", false
}

func obsBool(values map[int]json.RawMessage, id int) (bool, bool) {
	raw, ok := values[id]
	if !ok {
		return false, false
	}
	var flag bool
	if err := json.Unmarshal(raw, &flag); err == nil {
		return flag, true
	}
	return false, false
}

// Status reads the charger state and maps it onto the shared shape.
func (e *Easee) Status(ctx context.Context) (domain.ChargerStatus, error) {
	values, err := e.observations(ctx)
	if err != nil {
		return domain.ChargerStatus{}, err
	}
	serial, _ := e.resolveSerial(ctx)

	status := domain.ChargerStatus{
		ID:   serial,
		Type: "easee",
		// Assume online unless the charger says otherwise; an observation set
		// that parsed at all means the cloud sees the device.
		Online:    true,
		UpdatedAt: e.cfg.Now().UTC(),
	}
	if online, ok := obsBool(values, obsConnectedToCloud); ok {
		status.Online = online
	}
	pilot, _ := obsString(values, obsPilotMode)
	pilot = strings.ToUpper(strings.TrimSpace(pilot))
	if power, ok := obsFloat(values, obsTotalPowerKW); ok {
		status.PowerW = power * 1000
	}
	if current, ok := obsFloat(values, obsOutputCurrent); ok {
		status.CurrentA = current
	}
	if energy, ok := obsFloat(values, obsSessionEnergy); ok {
		status.SessionKWh = energy
	}
	enabled, hasEnabled := obsBool(values, obsIsEnabled)

	connected := pilot != "" && pilot != "A"
	charging := status.PowerW > 50 || pilot == "D" || pilot == "E"

	switch {
	case !status.Online:
		status.Mode = "offline"
	case hasEnabled && !enabled:
		status.Mode = "disabled"
	case pilot == "E" || pilot == "F":
		status.Mode = "error"
		charging = false
	case charging:
		status.Mode = "charging"
	case connected:
		status.Mode = "connected"
	default:
		status.Mode = "idle"
	}
	status.Connected = connected && status.Mode != "error"
	status.Charging = charging && status.Mode == "charging"
	if phaseMode, ok := obsFloat(values, 38); ok && phaseMode > 0 {
		status.Phases = int(phaseMode)
	}
	if status.Phases == 0 {
		status.Phases = 3
	}
	e.mu.Lock()
	status.Paused = e.paused
	e.lastMode = status.Mode
	e.mu.Unlock()
	return status, nil
}

// ------------------------------------------------------------------ commands --

// SetCurrent applies a dynamic current limit. A zero TTL keeps the limit until
// the next command, and the charger clears it on reboot or cable disconnect.
func (e *Easee) SetCurrent(ctx context.Context, amps float64) error {
	serial, err := e.resolveSerial(ctx)
	if err != nil {
		return err
	}
	if amps < 6 {
		amps = 6
	}
	if amps > 32 {
		amps = 32
	}
	body := map[string]any{"amps": int(roundToInt(amps)), "minutes": 0}
	path := "/api/chargers/" + url.PathEscape(serial) + "/commands/set_dynamic_charger_current"
	return e.call(ctx, http.MethodPost, path, body, nil)
}

func roundToInt(value float64) int {
	if value < 0 {
		return -int(-value + 0.5)
	}
	return int(value + 0.5)
}

// Pause stops charging: Easee has no pause state, so the session ends.
func (e *Easee) Pause(ctx context.Context) error {
	if err := e.StopSession(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	e.paused = true
	e.mu.Unlock()
	return nil
}

// Resume asks the charger to start charging again.
func (e *Easee) Resume(ctx context.Context) error {
	serial, err := e.resolveSerial(ctx)
	if err != nil {
		return err
	}
	path := "/api/chargers/" + url.PathEscape(serial) + "/commands/start_charging"
	if err := e.call(ctx, http.MethodPost, path, map[string]any{}, nil); err != nil {
		return err
	}
	e.mu.Lock()
	e.paused = false
	e.mu.Unlock()
	return nil
}

// StopSession ends an active charging session.
func (e *Easee) StopSession(ctx context.Context) error {
	serial, err := e.resolveSerial(ctx)
	if err != nil {
		return err
	}
	path := "/api/chargers/" + url.PathEscape(serial) + "/commands/stop_charging"
	return e.call(ctx, http.MethodPost, path, map[string]any{}, nil)
}
