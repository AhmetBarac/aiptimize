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

func (z *Zaptec) ID() string   { return z.cfg.ChargerID }
func (z *Zaptec) Type() string { return "zaptec_cloud" }

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
		return fmt.Errorf("zaptec %s: status %d", path, resp.StatusCode)
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
	var raw map[string]any
	if err := z.request(ctx, http.MethodGet, "/api/chargers/"+z.cfg.ChargerID+"/state", nil, &raw); err != nil {
		return domain.ChargerStatus{}, err
	}
	lookup := func(key string) float64 {
		if v, ok := raw[key]; ok {
			return number(v)
		}
		for _, value := range raw {
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
	if phases <= 0 {
		phases = 3
	}
	finalStop := lookup("718") == 1
	return domain.ChargerStatus{
		ID: z.cfg.ChargerID, Type: "zaptec_cloud", Online: true,
		Connected: power > 0 || session > 0, Charging: power > 0 && !finalStop,
		PowerW: power, CurrentA: current, Phases: phases, SessionKWh: session,
		Paused: finalStop, UpdatedAt: z.cfg.Now().UTC(),
	}, nil
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
	return z.request(ctx, http.MethodPost, "/api/chargers/"+z.cfg.ChargerID+"/sendCommand/506", nil, nil)
}

// Resume implements Charger.
func (z *Zaptec) Resume(ctx context.Context) error {
	return z.request(ctx, http.MethodPost, "/api/chargers/"+z.cfg.ChargerID+"/sendCommand/507", nil, nil)
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
