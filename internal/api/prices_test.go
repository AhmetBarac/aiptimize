package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/prices"
)

func generatedPriceDay(req prices.Request, priceMWh float64, state domain.PriceState) prices.Day {
	loc := req.Location
	if loc == nil {
		loc = time.UTC
	}
	start := time.Date(req.Date.In(loc).Year(), req.Date.In(loc).Month(), req.Date.In(loc).Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	var points []domain.PricePoint
	for ts := start; ts.Before(end); ts = ts.Add(15 * time.Minute) {
		points = append(points, domain.PricePoint{
			DeliveryStart: ts.UTC(),
			DeliveryEnd:   ts.Add(15 * time.Minute).UTC(),
			PriceMWh:      priceMWh,
			PriceKWh:      priceMWh / 1000,
			State:         state,
		})
	}
	return prices.Day{
		Area: req.Area, Currency: req.Currency,
		Date: start.Format("2006-01-02"), State: state,
		Points: points, FetchedAt: time.Now().UTC(),
	}
}

func TestPricesAPIRequiresAuth(t *testing.T) {
	a := newTestAPI(t, nil)
	resp, body := a.get("/api/v1/prices")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("prices without auth = %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/prices/refresh", map[string]any{})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("refresh without auth = %d %s", resp.StatusCode, body)
	}
}

func TestPricesAPIRefreshAndRead(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	date := time.Now().In(time.UTC).Format("2006-01-02")

	a.fetcher.set(func(_ context.Context, req prices.Request) (prices.Day, error) {
		return generatedPriceDay(req, 500, domain.PriceStateFinal), nil
	})

	resp, body := a.post("/api/v1/prices/refresh", map[string]any{"date": date})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh = %d %s", resp.StatusCode, body)
	}
	refresh := decodeMap(t, body)
	results := refresh["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v", results)
	}
	first := results[0].(map[string]any)
	if first["ok"] != true || first["points"].(float64) != 96 || first["state"] != "Final" {
		t.Fatalf("refresh result = %v", first)
	}

	resp, body = a.get("/api/v1/prices?date=" + date)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("prices = %d %s", resp.StatusCode, body)
	}
	snap := decodeMap(t, body)
	if snap["area"] != "SE4" || snap["currency"] != "SEK" || snap["timezone"] != "UTC" {
		t.Errorf("metadata = %v %v %v", snap["area"], snap["currency"], snap["timezone"])
	}
	if snap["state"] != "Final" {
		t.Errorf("state = %v", snap["state"])
	}
	points := snap["points"].([]any)
	blocks := snap["blocks"].([]any)
	if len(points) != 96 || len(blocks) != 48 {
		t.Fatalf("points/blocks = %d/%d", len(points), len(blocks))
	}
	block := blocks[0].(map[string]any)
	if block["price_mwh"].(float64) != 500 {
		t.Errorf("block price = %v", block["price_mwh"])
	}
	if block["import_price"].(float64) != 0.625 {
		t.Errorf("import price = %v", block["import_price"])
	}
	if block["points"].(float64) != 2 {
		t.Errorf("block points = %v", block["points"])
	}
	tariff := snap["tariff"].(map[string]any)
	if tariff["vat"].(float64) != 25 {
		t.Errorf("tariff = %v", tariff)
	}
	summary := snap["summary"].(map[string]any)
	if summary["points"].(float64) != 96 || summary["blocks"].(float64) != 48 {
		t.Errorf("summary = %v", summary)
	}
	if len(summary["cheapest_windows"].([]any)) != 3 {
		t.Errorf("cheapest windows = %v", summary["cheapest_windows"])
	}
	if sna := snap["attribution"].(string); !strings.Contains(sna, "Nord Pool") {
		t.Errorf("attribution = %q", sna)
	}
	if snap["source"] != "Nord Pool Data Portal" {
		t.Errorf("source = %v", snap["source"])
	}
	if snap["fetched_at"] == nil {
		t.Error("fetched_at missing")
	}

	// The status endpoint must now report available prices.
	resp, body = a.get("/api/v1/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	status := decodeMap(t, body)
	priceStatus := status["prices"].(map[string]any)
	if priceStatus["available"] != true || priceStatus["points"].(float64) != 96 || priceStatus["blocks"].(float64) != 48 {
		t.Errorf("status prices = %v", priceStatus)
	}
	if _, ok := priceStatus["now_import"]; !ok {
		t.Errorf("now_import missing: %v", priceStatus)
	}
}

func TestPricesAPIValidation(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	resp, body := a.get("/api/v1/prices?date=nope")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad date = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/prices?from=2026-10-29T00:00:00Z&to=2026-10-28T00:00:00Z")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad range = %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/prices/refresh", map[string]any{"date": "29-10-2026"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad refresh date = %d %s", resp.StatusCode, body)
	}

	a.fetcher.set(func(_ context.Context, req prices.Request) (prices.Day, error) {
		return prices.Day{}, prices.ErrNotPublished
	})
	resp, body = a.post("/api/v1/prices/refresh", map[string]any{"date": "2026-10-29"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("not published = %d %s", resp.StatusCode, body)
	}
	result := decodeMap(t, body)["results"].([]any)[0].(map[string]any)
	if result["ok"] != false || result["code"] != "price_not_published" {
		t.Fatalf("not published result = %v", result)
	}

	a.fetcher.set(func(_ context.Context, req prices.Request) (prices.Day, error) {
		return prices.Day{}, errors.New("provider down")
	})
	resp, body = a.post("/api/v1/prices/refresh", map[string]any{"date": "2026-10-29"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("provider failure = %d %s", resp.StatusCode, body)
	}
	result = decodeMap(t, body)["results"].([]any)[0].(map[string]any)
	if result["code"] != "price_refresh_failed" {
		t.Fatalf("failure result = %v", result)
	}
}
