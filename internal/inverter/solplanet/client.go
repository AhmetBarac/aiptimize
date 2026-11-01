// Package solplanet is the read-only client for the Solplanet AI-Dongle HTTP
// API. Phase 2 reads identity, live telemetry and the weekly schedule; it never
// posts to setting.cgi.
package solplanet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
)

// DefaultTimeout is applied when Config.Timeout is zero.
const DefaultTimeout = 8 * time.Second

// Config describes how to reach one dongle.
type Config struct {
	Scheme    string
	Host      string
	Port      int
	Serial    string
	Timeout   time.Duration
	UserAgent string
	Signs     inverter.Signs
	Logger    *slog.Logger
}

// Error is a failed dongle request.
type Error struct {
	Path   string
	Status int
	Body   string
}

func (e *Error) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 160 {
		body = body[:160]
	}
	if e.Status == http.StatusNotFound {
		return fmt.Sprintf("solplanet %s: HTTP 404 (check the inverter serial; data endpoints require sn)", e.Path)
	}
	return fmt.Sprintf("solplanet %s: HTTP %d: %s", e.Path, e.Status, body)
}

// Client talks to one dongle. It is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
	log  *slog.Logger
	now  func() time.Time

	mu       sync.Mutex
	identity *inverter.Identity
	serial   string
}

// New builds a client. Host must be set.
func New(cfg Config) *Client {
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	if cfg.Port == 0 {
		if cfg.Scheme == "https" {
			cfg.Port = 443
		} else {
			cfg.Port = 8484
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "aiptimize/dev (+https://github.com/AhmetBarac/aiptimize)"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
		log:  logger,
		now:  time.Now,
	}
}

func (c *Client) baseURL() string {
	host := c.cfg.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s", c.cfg.Scheme, net.JoinHostPort(host, fmt.Sprintf("%d", c.cfg.Port)))
}

func (c *Client) getRaw(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := c.baseURL() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solplanet %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("solplanet %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Path: path, Status: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

func (c *Client) getMap(ctx context.Context, path string, query url.Values) (map[string]json.RawMessage, error) {
	body, err := c.getRaw(ctx, path, query)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("solplanet %s: invalid JSON: %w", path, err)
	}
	return m, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("solplanet %s: %w", path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("solplanet %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Path: path, Status: resp.StatusCode, Body: string(out)}
	}
	return out, nil
}

// Identity reads and caches the inverter and battery description.
func (c *Client) Identity(ctx context.Context) (inverter.Identity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != nil {
		return *c.identity, nil
	}

	payload, err := c.getMap(ctx, "/getdev.cgi", url.Values{"device": {"2"}})
	if err != nil {
		return inverter.Identity{}, err
	}
	var invList []map[string]json.RawMessage
	if raw, ok := payload["inv"]; ok {
		if err := json.Unmarshal(raw, &invList); err != nil {
			return inverter.Identity{}, fmt.Errorf("solplanet getdev device=2: invalid inv list: %w", err)
		}
	}
	if len(invList) == 0 {
		return inverter.Identity{}, errors.New("solplanet getdev device=2: no inverter entries")
	}
	inv := invList[0]
	serial := stringValue(inv, "isn")
	if serial == "" {
		return inverter.Identity{}, errors.New("solplanet getdev device=2: missing serial (isn)")
	}
	if configured := strings.TrimSpace(c.cfg.Serial); configured != "" && !strings.EqualFold(configured, serial) {
		return inverter.Identity{}, fmt.Errorf("inverter serial mismatch: configured %q, dongle reports %q", configured, serial)
	}
	rated, _ := floatValue(inv, "rate")
	phases, _ := intValue(inv, "add")
	safety, _ := intValue(inv, "safety")

	id := inverter.Identity{
		Serial:      serial,
		Model:       stringValue(inv, "model"),
		Firmware:    stringValue(inv, "msw"),
		RatedPowerW: rated,
		Phases:      phases,
		SafetyCode:  safety,
	}

	// Battery configuration is optional for telemetry but provides capacity and
	// the selected work mode.
	if bc, bcErr := c.getMap(ctx, "/getdev.cgi", url.Values{"device": {"4"}, "sn": {serial}}); bcErr == nil {
		if capWh, ok := floatValue(objectValue(bc, "battery"), "capacity"); ok {
			id.CapacityKWh = capWh / 1000
		}
		if mode, ok := intValue(bc, "mod_r"); ok {
			id.WorkMode = mode
		}
		if minSOC, ok := floatValue(bc, "discharge_max"); ok {
			id.MinSOC = minSOC
		}
		if maxSOC, ok := floatValue(bc, "charge_max"); ok {
			id.MaxSOC = maxSOC
		}
	} else {
		c.log.Debug("battery configuration unavailable", "err", bcErr)
	}

	c.identity = &id
	c.serial = serial
	return id, nil
}

// Read fetches one live sample from the inverter, grid meter and battery.
func (c *Client) Read(ctx context.Context) (inverter.Reading, error) {
	id, err := c.Identity(ctx)
	if err != nil {
		return inverter.Reading{}, err
	}
	serial := id.Serial
	if serial == "" {
		serial = c.cfg.Serial
	}
	if serial == "" {
		return inverter.Reading{}, errors.New("solplanet: no inverter serial")
	}

	invData, err := c.getMap(ctx, "/getdevdata.cgi", url.Values{"device": {"2"}, "sn": {serial}})
	if err != nil {
		return inverter.Reading{}, err
	}
	meter, err := c.getMap(ctx, "/getdevdata.cgi", url.Values{"device": {"3"}})
	if err != nil {
		return inverter.Reading{}, err
	}
	battery, err := c.getMap(ctx, "/getdevdata.cgi", url.Values{"device": {"4"}, "sn": {serial}})
	if err != nil {
		return inverter.Reading{}, err
	}

	gridW, ok := floatValue(meter, "pac")
	if !ok {
		return inverter.Reading{}, errors.New("solplanet meter: missing pac")
	}
	soc, ok := floatValue(battery, "soc")
	if !ok {
		return inverter.Reading{}, errors.New("solplanet battery: missing soc")
	}

	stringsW := make([]float64, 0, 3)
	vpv := floatSlice(invData, "vpv")
	ipv := floatSlice(invData, "ipv")
	for i := 0; i < len(vpv) && i < len(ipv); i++ {
		watts := (vpv[i] / 10) * (ipv[i] / 100)
		if watts < 0 {
			watts = 0
		}
		stringsW = append(stringsW, round1(watts))
	}
	pvW, hasPV := floatValue(battery, "ppv")
	if !hasPV || pvW <= 0 {
		pvW = 0
		for _, w := range stringsW {
			pvW += w
		}
	}
	pb, hasPB := floatValue(battery, "pb")
	if !hasPB {
		pb = 0
	}

	signPV := signOr(c.cfg.Signs.PV)
	signGrid := signOr(c.cfg.Signs.Grid)
	signBattery := signOr(c.cfg.Signs.Battery)

	reading := inverter.Reading{
		At:        c.now().UTC(),
		PVW:       round1(pvW * signPV),
		PVStrings: scaleSlice(stringsW, signPV),
		GridW:     round1(gridW * signGrid),
		BatteryW:  round1(-pb * signBattery), // wire positive = discharging
		SOC:       soc,
		Mode:      id.WorkMode,
		Extra:     map[string]any{},
	}
	reading.LoadW = round1(reading.PVW + reading.GridW - reading.BatteryW)
	if reading.LoadW < 0 {
		reading.LoadW = 0
	}
	if soh, ok := floatValue(battery, "soh"); ok {
		reading.SOH = soh
		reading.Extra["soh"] = soh
	}
	if v, ok := floatValue(battery, "bst"); ok {
		reading.Extra["bst"] = int(v)
	}
	if v, ok := floatValue(battery, "cst"); ok {
		reading.Extra["cst"] = int(v)
	}
	if v, ok := floatValue(battery, "vb"); ok {
		reading.Extra["battery_voltage_v"] = round2(v / 100)
	}
	if v, ok := floatValue(battery, "tb"); ok {
		reading.Extra["battery_temp_c"] = round1(v / 10)
	}
	if v, ok := floatValue(battery, "etdpv"); ok {
		reading.Extra["pv_energy_today_kwh"] = round2(v / 10)
	}
	if v, ok := floatValue(invData, "pac"); ok {
		reading.Extra["pv_ac_w"] = round1(v)
	}
	if v, ok := floatValue(invData, "tmp"); ok {
		reading.Extra["inverter_temp_c"] = round1(v / 10)
	}
	if v, ok := floatValue(meter, "itd"); ok {
		reading.Extra["grid_import_today_kwh"] = round2(v / 100)
	}
	if v, ok := floatValue(meter, "otd"); ok {
		reading.Extra["grid_export_today_kwh"] = round2(v / 100)
	}
	if tim := stringValue(invData, "tim"); tim != "" {
		reading.Extra["device_time"] = tim
	}
	if vac := floatSlice(invData, "vac"); len(vac) > 0 {
		reading.Extra["phases"] = len(vac)
	}
	return reading, nil
}

// Schedule reads and decodes the weekly schedule (read-only).
func (c *Client) Schedule(ctx context.Context) (domain.Schedule, error) {
	body, err := c.getRaw(ctx, "/getdefine.cgi", nil)
	if err != nil {
		return domain.Schedule{}, err
	}
	schedule, warnings, err := DecodeSchedule(body)
	if err != nil {
		return domain.Schedule{}, err
	}
	for _, w := range warnings {
		c.log.Warn("schedule decode warning", "warning", w)
	}
	return schedule, nil
}

// SetWorkMode selects the inverter battery work mode via setbattery. The
// caller must enforce the write gate. Supported modes are 1 (off-grid),
// 2 (self-consumption), 3 (reserve power) and 4 (custom schedule).
func (c *Client) SetWorkMode(ctx context.Context, mode int) error {
	batteryType := 1
	switch mode {
	case 1:
		batteryType = 2
	case 2, 3, 4:
	default:
		return fmt.Errorf("solplanet: unsupported battery work mode %d", mode)
	}
	if c.serial == "" {
		if _, err := c.Identity(ctx); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"device": 4,
		"action": "setbattery",
		"value":  map[string]any{"sn": c.serial, "mod_r": mode, "type": batteryType},
	}
	_, err := c.postJSON(ctx, "/setting.cgi", payload)
	if err == nil {
		c.mu.Lock()
		if c.identity != nil {
			c.identity.WorkMode = mode
		}
		c.mu.Unlock()
	}
	return err
}

// WriteSchedule selects Custom mode, writes the full seven-day setdefine value
// and returns the readback. The caller must enforce the arm gate first.
func (c *Client) WriteSchedule(ctx context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	if c.serial == "" {
		if _, err := c.Identity(ctx); err != nil {
			return domain.Schedule{}, err
		}
	}
	encoded, err := EncodeSchedule(schedule)
	if err != nil {
		return domain.Schedule{}, err
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return domain.Schedule{}, err
	}
	selectPayload := map[string]any{
		"device": 4,
		"action": "setbattery",
		"value":  map[string]any{"sn": c.serial, "mod_r": 4, "type": 1},
	}
	if _, err := c.postJSON(ctx, "/setting.cgi", selectPayload); err != nil {
		return domain.Schedule{}, err
	}
	writePayload := map[string]any{"device": 4, "action": "setdefine", "value": value}
	if _, err := c.postJSON(ctx, "/setting.cgi", writePayload); err != nil {
		return domain.Schedule{}, err
	}
	return c.Schedule(ctx)
}

// Close releases idle connections.
func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

func signOr(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}

func scaleSlice(values []float64, factor float64) []float64 {
	if len(values) == 0 {
		return nil
	}
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = round1(v * factor)
	}
	return out
}

func round1(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*100) / 100
}
