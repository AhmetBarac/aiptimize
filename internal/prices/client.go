// Package prices ingests Nord Pool day-ahead prices, stores them and derives
// 30-minute blocks with import/export tariffs.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// DefaultBaseURL is the Nord Pool Data Portal day-ahead endpoint.
const DefaultBaseURL = "https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices"

var (
	// ErrNotPublished is returned for a day that the market has not published.
	ErrNotPublished = errors.New("prices: day not published yet")
	// ErrNoData is returned when the response has no points for the area.
	ErrNoData = errors.New("prices: no data for area")
)

// HTTPError is a non-success provider response.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 200 {
		body = body[:200]
	}
	return fmt.Sprintf("prices: provider returned HTTP %d: %s", e.StatusCode, body)
}

// Request is one day-ahead price request.
type Request struct {
	Area     string
	Currency string
	// Date is the local delivery date; only the calendar date is significant.
	Date time.Time
	// Location is the site timezone used to select the local delivery day.
	Location *time.Location
}

// Day is the fetched and filtered price day.
type Day struct {
	Area      string
	Currency  string
	Date      string
	State     domain.PriceState
	Points    []domain.PricePoint
	FetchedAt time.Time
	UpdatedAt time.Time
	Version   int
}

// Fetcher retrieves one day of prices.
type Fetcher interface {
	FetchDay(ctx context.Context, req Request) (Day, error)
}

// Client talks to the Nord Pool Data Portal.
type Client struct {
	BaseURL     string
	HTTP        *http.Client
	UserAgent   string
	MinInterval time.Duration
	Now         func() time.Time

	mu   sync.Mutex
	last time.Time
}

// NewClient builds a client with a descriptive user agent and a self-imposed
// rate limit of one request per second.
func NewClient(baseURL, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if userAgent == "" {
		userAgent = "aiptimize/dev (+https://github.com/AhmetBarac/aiptimize)"
	}
	return &Client{
		BaseURL:     baseURL,
		HTTP:        &http.Client{Timeout: 20 * time.Second},
		UserAgent:   userAgent,
		MinInterval: time.Second,
		Now:         time.Now,
	}
}

type apiAreaState struct {
	State string   `json:"state"`
	Area  string   `json:"area"`
	Areas []string `json:"areas"`
}

type apiResponse struct {
	DeliveryDateCET  string    `json:"deliveryDateCET"`
	Version          int       `json:"version"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Currency         string    `json:"currency"`
	MultiAreaEntries []struct {
		DeliveryStart time.Time          `json:"deliveryStart"`
		DeliveryEnd   time.Time          `json:"deliveryEnd"`
		EntryPerArea  map[string]float64 `json:"entryPerArea"`
	} `json:"multiAreaEntries"`
	AreaStates []apiAreaState `json:"areaStates"`
}

// FetchDay retrieves prices for one local delivery day.
func (c *Client) FetchDay(ctx context.Context, req Request) (Day, error) {
	if req.Area == "" || req.Currency == "" {
		return Day{}, errors.New("prices: area and currency are required")
	}
	if req.Location == nil {
		req.Location = time.UTC
	}
	if err := c.wait(ctx); err != nil {
		return Day{}, err
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return Day{}, fmt.Errorf("prices: invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("date", req.Date.In(req.Location).Format("2006-01-02"))
	q.Set("market", "DayAhead")
	q.Set("deliveryArea", req.Area)
	q.Set("currency", req.Currency)
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Day{}, err
	}
	httpReq.Header.Set("User-Agent", c.UserAgent)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Day{}, fmt.Errorf("prices: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return Day{}, ErrNotPublished
	case http.StatusOK:
		// continue
	case http.StatusUnauthorized, http.StatusForbidden:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Day{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	case http.StatusBadRequest:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Day{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Day{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	dec := json.NewDecoder(io.LimitReader(resp.Body, 5<<20))
	var payload apiResponse
	if err := dec.Decode(&payload); err != nil {
		return Day{}, fmt.Errorf("prices: invalid response: %w", err)
	}

	state := stateFor(payload.AreaStates, req.Area)
	loc := req.Location
	dayStart := time.Date(req.Date.In(loc).Year(), req.Date.In(loc).Month(), req.Date.In(loc).Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	seen := make(map[time.Time]struct{}, len(payload.MultiAreaEntries))
	points := make([]domain.PricePoint, 0, len(payload.MultiAreaEntries))
	for _, entry := range payload.MultiAreaEntries {
		price, ok := entry.EntryPerArea[req.Area]
		if !ok {
			continue
		}
		if entry.DeliveryStart.Before(dayStart) || !entry.DeliveryStart.Before(dayEnd) {
			continue
		}
		if _, dup := seen[entry.DeliveryStart]; dup {
			continue
		}
		seen[entry.DeliveryStart] = struct{}{}
		points = append(points, domain.PricePoint{
			DeliveryStart: entry.DeliveryStart.UTC(),
			DeliveryEnd:   entry.DeliveryEnd.UTC(),
			PriceMWh:      price,
			PriceKWh:      price / 1000,
			State:         state,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].DeliveryStart.Before(points[j].DeliveryStart) })
	if len(points) == 0 {
		return Day{}, ErrNoData
	}

	currency := payload.Currency
	if currency == "" {
		currency = req.Currency
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return Day{
		Area:      req.Area,
		Currency:  currency,
		Date:      dayStart.Format("2006-01-02"),
		State:     state,
		Points:    points,
		FetchedAt: now().UTC(),
		UpdatedAt: payload.UpdatedAt,
		Version:   payload.Version,
	}, nil
}

func stateFor(states []apiAreaState, area string) domain.PriceState {
	for _, st := range states {
		if st.Area == area || containsString(st.Areas, area) {
			return normalizeState(st.State)
		}
	}
	if len(states) > 0 {
		return normalizeState(states[0].State)
	}
	return domain.PriceStateFinal
}

func normalizeState(s string) domain.PriceState {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "preliminary":
		return domain.PriceStatePreliminary
	case "missing":
		return domain.PriceStateMissing
	case "final":
		return domain.PriceStateFinal
	default:
		return domain.PriceStateFinal
	}
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// wait enforces the client's minimum request interval.
func (c *Client) wait(ctx context.Context) error {
	interval := c.MinInterval
	if interval <= 0 {
		return nil
	}
	c.mu.Lock()
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	next := c.last.Add(interval)
	if c.last.IsZero() || !next.After(now) {
		c.last = now
		c.mu.Unlock()
		return nil
	}
	delay := next.Sub(now)
	c.last = next
	c.mu.Unlock()

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
