package ev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// ZaptecConfig configures the Zaptec cloud adapter.
type ZaptecConfig struct {
	BaseURL        string
	Username       string
	Password       string
	InstallationID string
	ChargerID      string
	UserAgent      string
	HTTP           *http.Client
	Now            func() time.Time
}

// Zaptec is a minimal Zaptec Go cloud adapter. Vehicle SOC is not available.
type Zaptec struct {
	cfg ZaptecConfig

	mu          sync.Mutex
	token       string
	expires     time.Time
	lastCurrent time.Time
	resolvedID  string
}

// NewZaptec builds the adapter.
func NewZaptec(cfg ZaptecConfig) *Zaptec {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.zaptec.com"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "aiptimize/dev (+https://github.com/AhmetBarac/aiptimize)"
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Zaptec{cfg: cfg}
}

func (z *Zaptec) ID() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.resolvedID != "" {
		return z.resolvedID
	}
	return z.cfg.ChargerID
}
func (z *Zaptec) Type() string { return "zaptec_cloud" }

// UpdateConfig applies charger credentials and IDs without rebuilding the
// adapter. The cached OAuth token is invalidated because credentials changed.
func (z *Zaptec) UpdateConfig(chargerID, installationID, username, password string) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.cfg.ChargerID = chargerID
	z.cfg.InstallationID = installationID
	if username != "" {
		z.cfg.Username = username
	}
	if password != "" {
		z.cfg.Password = password
	}
	z.token = ""
	z.expires = time.Time{}
	z.lastCurrent = time.Time{}
	z.resolvedID = ""
}

type zaptecCharger struct {
	ID             string `json:"Id"`
	MID            string `json:"MID"`
	DeviceID       string `json:"DeviceId"`
	SerialNo       string `json:"SerialNo"`
	Name           string `json:"Name"`
	InstallationID string `json:"InstallationId"`
	IsOnline       bool   `json:"IsOnline"`
}

type zaptecChargerList struct {
	Data []zaptecCharger `json:"Data"`
}

// resolveChargerID maps a serial/MID/name to the GUID used by the Zaptec API.
// If the configured value does not match and the installation has exactly one
// charger, that charger is used.
func (z *Zaptec) resolveChargerID(ctx context.Context, force bool) (string, error) {
	z.mu.Lock()
	if z.resolvedID != "" && !force {
		id := z.resolvedID
		z.mu.Unlock()
		return id, nil
	}
	configured := strings.TrimSpace(z.cfg.ChargerID)
	z.mu.Unlock()

	var list zaptecChargerList
	if err := z.request(ctx, http.MethodGet, "/api/chargers", nil, &list); err != nil {
		return "", err
	}
	needle := strings.ToLower(configured)
	var exact *zaptecCharger
	var installationMatches []zaptecCharger
	for i := range list.Data {
		c := &list.Data[i]
		if needle != "" && (strings.ToLower(c.ID) == needle ||
			strings.ToLower(c.MID) == needle ||
			strings.ToLower(c.DeviceID) == needle ||
			strings.ToLower(c.SerialNo) == needle ||
			strings.ToLower(c.Name) == needle) {
			exact = c
			break
		}
		if z.cfg.InstallationID != "" && c.InstallationID == z.cfg.InstallationID {
			installationMatches = append(installationMatches, *c)
		}
	}
	if exact == nil && len(installationMatches) == 1 {
		exact = &installationMatches[0]
	}
	if exact == nil {
		if configured == "" {
			return "", fmt.Errorf("zaptec charger id is missing")
		}
		return "", fmt.Errorf("zaptec charger %q was not found", configured)
	}
	z.mu.Lock()
	z.resolvedID = exact.ID
	z.mu.Unlock()
	return exact.ID, nil
}

func (z *Zaptec) chargerID(ctx context.Context) (string, error) {
	return z.resolveChargerID(ctx, false)
}

// Capabilities implements Charger.
func (z *Zaptec) Capabilities() Capabilities {
	return Capabilities{
		MinCurrentA: 6, MaxCurrentA: 16, Phases: 3, CurrentScope: "installation",
		SupportsPause: true, ControlLatency: 15 * time.Minute,
	}
}

func (z *Zaptec) accessToken(ctx context.Context) (string, error) {
	z.mu.Lock()
	if z.token != "" && z.cfg.Now().Before(z.expires.Add(-60*time.Second)) {
		token := z.token
		z.mu.Unlock()
		return token, nil
	}
	z.mu.Unlock()

	form := url.Values{
		"grant_type": {"password"},
		"username":   {z.cfg.Username},
		"password":   {z.cfg.Password},
		"scope":      {"openid"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.cfg.BaseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", z.cfg.UserAgent)
	resp, err := z.cfg.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zaptec token: status %d", resp.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("zaptec token: empty token")
	}
	z.mu.Lock()
	z.token = token.AccessToken
	z.expires = z.cfg.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	z.mu.Unlock()
	return token.AccessToken, nil
}

// zaptecFailure turns an error response into something actionable. Zaptec
// reports business-rule rejections as HTTP 500 with a Code/Details body, e.g.
// {Code:527, Details:"Cannot update installation when using eco mode"}.
func zaptecFailure(status int, raw []byte) string {
	var problem struct {
		Code    int    `json:"Code"`
		Details string `json:"Details"`
	}
	if err := json.Unmarshal(raw, &problem); err == nil {
		switch {
		case problem.Code == 527:
			return fmt.Sprintf("status %d (code 527): %s — eco mode blocks remote current control; "+
				"disable eco mode for this charger in the Zaptec app", status, problem.Details)
		case problem.Details != "":
			return fmt.Sprintf("status %d (code %d): %s", status, problem.Code, problem.Details)
		}
	}
	return fmt.Sprintf("status %d", status)
}

func (z *Zaptec) request(ctx context.Context, method, path string, payload any, out any) error {
	token, err := z.accessToken(ctx)
	if err != nil {
		return err
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, z.cfg.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", z.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := z.cfg.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		z.mu.Lock()
		z.token = ""
		z.mu.Unlock()
		return fmt.Errorf("zaptec %s: unauthorized", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("zaptec %s: %s", path, zaptecFailure(resp.StatusCode, raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return err
		}
	}
	return nil
}

// Status implements Charger.
func (z *Zaptec) Status(ctx context.Context) (domain.ChargerStatus, error) {
	id, err := z.chargerID(ctx)
	if err != nil {
		return domain.ChargerStatus{}, err
	}
	var raw json.RawMessage
	err = z.request(ctx, http.MethodGet, "/api/chargers/"+id+"/state", nil, &raw)
	if err != nil && strings.Contains(err.Error(), "status 404") {
		if id, resolveErr := z.resolveChargerID(ctx, true); resolveErr == nil {
			err = z.request(ctx, http.MethodGet, "/api/chargers/"+id+"/state", nil, &raw)
		}
	}
	if err != nil {
		return domain.ChargerStatus{}, err
	}
	state := map[string]any{}
	var observations []struct {
		StateID int             `json:"StateId"`
		Value   json.RawMessage `json:"ValueAsString"`
	}
	if err := json.Unmarshal(raw, &observations); err == nil && len(observations) > 0 {
		for _, obs := range observations {
			state[fmt.Sprintf("%d", obs.StateID)] = decodeStateValue(obs.Value)
		}
	} else {
		_ = json.Unmarshal(raw, &state)
		if nested, ok := state["Observations"].(map[string]any); ok {
			state = nested
		}
	}
	lookup := func(key string) float64 {
		if v, ok := state[key]; ok {
			return number(v)
		}
		for _, value := range state {
			if obj, ok := value.(map[string]any); ok {
				if v, ok := obj[key]; ok {
					return number(v)
				}
			}
		}
		return 0
	}
	session := lookup("553")
	power := lookup("513")
	current := lookup("708")
	phases := int(lookup("519"))
	if phases < 1 || phases > 3 {
		phases = 3
	}
	finalStop := lookup("718") == 1
	return domain.ChargerStatus{
		ID: z.ID(), Type: "zaptec_cloud", Online: true,
		Connected: power > 0 || session > 0, Charging: power > 0 && !finalStop,
		PowerW: power, CurrentA: current, Phases: phases, SessionKWh: session,
		Paused: finalStop, UpdatedAt: z.cfg.Now().UTC(),
	}, nil
}

func decodeStateValue(raw json.RawMessage) any {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		var numberValue float64
		if _, err := fmt.Sscanf(text, "%f", &numberValue); err == nil {
			return numberValue
		}
		return text
	}
	var value any
	_ = json.Unmarshal(raw, &value)
	return value
}

// SetCurrent implements Charger. The installation-wide update is rate-limited.
func (z *Zaptec) SetCurrent(ctx context.Context, amps float64) error {
	z.mu.Lock()
	if !z.lastCurrent.IsZero() && z.cfg.Now().Sub(z.lastCurrent) < 15*time.Minute {
		z.mu.Unlock()
		return fmt.Errorf("zaptec current update is limited to once per 15 minutes")
	}
	z.mu.Unlock()
	if z.cfg.InstallationID == "" {
		return fmt.Errorf("zaptec installation id is missing")
	}
	if err := z.request(ctx, http.MethodPost, "/api/installation/"+z.cfg.InstallationID+"/update",
		map[string]any{"AvailableCurrent": amps}, nil); err != nil {
		return err
	}
	z.mu.Lock()
	z.lastCurrent = z.cfg.Now()
	z.mu.Unlock()
	return nil
}

// Pause implements Charger.
func (z *Zaptec) Pause(ctx context.Context) error {
	id, err := z.chargerID(ctx)
	if err != nil {
		return err
	}
	return z.request(ctx, http.MethodPost, "/api/chargers/"+id+"/sendCommand/506", nil, nil)
}

// Resume implements Charger.
func (z *Zaptec) Resume(ctx context.Context) error {
	id, err := z.chargerID(ctx)
	if err != nil {
		return err
	}
	return z.request(ctx, http.MethodPost, "/api/chargers/"+id+"/sendCommand/507", nil, nil)
}

// StopSession implements SessionStopper. Zaptec's stop command ends the active
// charging session; the current limit is left for the next provider to manage.
func (z *Zaptec) StopSession(ctx context.Context) error {
	id, err := z.chargerID(ctx)
	if err != nil {
		return err
	}
	return z.request(ctx, http.MethodPost, "/api/chargers/"+id+"/sendCommand/506", nil, nil)
}

// Close implements Charger.
func (z *Zaptec) Close() error { return nil }

func number(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case json.Number:
		f, _ := value.Float64()
		return f
	case string:
		var f float64
		_, _ = fmt.Sscanf(value, "%f", &f)
		return f
	default:
		return 0
	}
}
