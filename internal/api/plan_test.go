package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func TestPlanAPIRecomputeAndRead(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	points := make([]domain.PricePoint, 0, 192)
	for i := 0; i < 192; i++ {
		start := dayStart.Add(time.Duration(i) * 15 * time.Minute)
		localHour := start.Hour()
		price := 0.6
		if localHour < 6 {
			price = 0.05
		}
		if localHour >= 17 && localHour < 21 {
			price = 2.5
		}
		points = append(points, domain.PricePoint{
			DeliveryStart: start.UTC(), DeliveryEnd: start.Add(15 * time.Minute).UTC(),
			PriceMWh: price * 1000, PriceKWh: price, State: domain.PriceStateFinal,
		})
	}
	if err := a.store.ReplacePrices(context.Background(), "SE4", "SEK", dayStart, dayStart.AddDate(0, 0, 2), points, time.Now()); err != nil {
		t.Fatal(err)
	}
	a.inverter.mu.Lock()
	a.inverter.reading = inverter.Reading{At: time.Now(), SOC: 50}
	a.inverter.mu.Unlock()
	if _, err := a.telemetry.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.settings.Patch(context.Background(), settings.SectionPlan, []byte(`{"min_daily_saving":0}`)); err != nil {
		t.Fatal(err)
	}

	resp, body := a.post("/api/v1/plan/recompute", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recompute = %d %s", resp.StatusCode, body)
	}
	plan := decodeMap(t, body)
	if plan["horizon_start"] == nil || plan["summary"] == nil {
		t.Fatalf("plan = %v", plan)
	}
	curve := plan["curve"].([]any)
	if len(curve) == 0 {
		t.Fatal("empty curve")
	}
	if len(plan["slots"].([]any)) == 0 {
		t.Logf("plan has no slots (possible at this time): %v", plan["summary"])
	}

	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get plan = %d %s", resp.StatusCode, body)
	}
	if decodeMap(t, body)["id"].(float64) != plan["id"].(float64) {
		t.Errorf("latest plan differs")
	}

	resp, body = a.get("/api/v1/status")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	statusPlan := decodeMap(t, body)["plan"].(map[string]any)
	if statusPlan["available"] != true {
		t.Errorf("status plan = %v", statusPlan)
	}
}
