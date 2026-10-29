// Package pv implements the Open-Meteo client and the per-MPPT PV model.
package pv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Open-Meteo forecast endpoint.
const DefaultBaseURL = "https://api.open-meteo.com/v1/forecast"

// Request is one tilted-irradiance request.
type Request struct {
	Latitude  float64
	Longitude float64
	Tilt      float64
	Azimuth   float64 // compass degrees, 0 = north, 90 = east
	Days      int
}

// Hourly is the raw hourly series returned by Open-Meteo.
type Hourly struct {
	Time []time.Time
	GTI  []float64
	Temp []float64
}

// Fetcher retrieves hourly tilted irradiance and temperature.
type Fetcher interface {
	Fetch(ctx context.Context, req Request) (Hourly, error)
}

// Client talks to Open-Meteo.
type Client struct {
	BaseURL   string
	HTTP      *http.Client
	UserAgent string
}

// NewClient builds a client.
func NewClient(baseURL, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if userAgent == "" {
		userAgent = "aiptimize/dev (+https://github.com/AhmetBarac/aiptimize)"
	}
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 20 * time.Second}, UserAgent: userAgent}
}

type response struct {
	Hourly struct {
		Time []string   `json:"time"`
		GTI  []*float64 `json:"global_tilted_irradiance"`
		Temp []*float64 `json:"temperature_2m"`
	} `json:"hourly"`
}

// Fetch retrieves one tilt/azimuth series.
func (c *Client) Fetch(ctx context.Context, req Request) (Hourly, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return Hourly{}, fmt.Errorf("open-meteo: invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(req.Latitude, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(req.Longitude, 'f', 4, 64))
	q.Set("timezone", "UTC")
	days := req.Days
	if days <= 0 {
		days = 2
	}
	q.Set("forecast_days", strconv.Itoa(days))
	q.Set("hourly", "global_tilted_irradiance,temperature_2m")
	q.Set("tilt", strconv.FormatFloat(req.Tilt, 'f', 1, 64))
	q.Set("azimuth", strconv.FormatFloat(CompassToOpenMeteo(req.Azimuth), 'f', 1, 64))
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Hourly{}, err
	}
	httpReq.Header.Set("User-Agent", c.UserAgent)
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Hourly{}, fmt.Errorf("open-meteo: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Hourly{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Hourly{}, fmt.Errorf("open-meteo: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var payload response
	if err := json.Unmarshal(body, &payload); err != nil {
		return Hourly{}, fmt.Errorf("open-meteo: invalid response: %w", err)
	}
	if len(payload.Hourly.Time) == 0 {
		return Hourly{}, fmt.Errorf("open-meteo: empty hourly series")
	}
	out := Hourly{
		Time: make([]time.Time, 0, len(payload.Hourly.Time)),
		GTI:  make([]float64, 0, len(payload.Hourly.Time)),
		Temp: make([]float64, 0, len(payload.Hourly.Time)),
	}
	for i, raw := range payload.Hourly.Time {
		ts, err := time.ParseInLocation("2006-01-02T15:04", raw, time.UTC)
		if err != nil {
			return Hourly{}, fmt.Errorf("open-meteo: invalid time %q: %w", raw, err)
		}
		out.Time = append(out.Time, ts)
		out.GTI = append(out.GTI, valueAt(payload.Hourly.GTI, i))
		out.Temp = append(out.Temp, valueAt(payload.Hourly.Temp, i))
	}
	return out, nil
}

func valueAt(values []*float64, i int) float64 {
	if i >= len(values) || values[i] == nil {
		return 0
	}
	return *values[i]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// CompassToOpenMeteo converts a compass azimuth to Open-Meteo's convention
// (0 = south, -90 = east, 90 = west, ±180 = north).
func CompassToOpenMeteo(compass float64) float64 {
	az := math.Mod(compass-180, 360)
	if az < -180 {
		az += 360
	}
	if az > 180 {
		az -= 360
	}
	return az
}
