package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/settings"
)

func TestPlanDraftAndApply(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	resp, body := a.post("/api/v1/plan/recompute", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recompute = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view = %d %s", resp.StatusCode, body)
	}
	view := decodeMap(t, body)
	auto, ok := view["auto"].(map[string]any)
	if !ok {
		t.Fatal("no auto plan")
	}
	now := time.Now()
	var chosen map[string]any
	for _, raw := range auto["slots"].([]any) {
		slot := raw.(map[string]any)
		start, _ := time.Parse(time.RFC3339, slot["start"].(string))
		if start.After(now.Add(2 * time.Minute)) {
			chosen = slot
			break
		}
	}
	if chosen == nil {
		t.Skip("automatic plan has no future slot to edit")
	}
	date := chosen["day_local"].(string)
	start, err := time.Parse(time.RFC3339, chosen["start"].(string))
	if err != nil {
		t.Fatal(err)
	}
	draft := map[string]any{
		"date": date, "revision": auto["revision"],
		"slots": []any{map[string]any{
			"start": start.Format(time.RFC3339), "end": start.Add(30 * time.Minute).Format(time.RFC3339),
			"direction": "charge", "power_w": 500,
		}},
		"note": "e2e draft",
	}
	resp, body = a.post("/api/v1/plan/drafts", draft)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("save draft = %d %s", resp.StatusCode, body)
	}
	saved := decodeMap(t, body)["plan"].(map[string]any)
	if saved["status"] != "draft" || saved["origin"] != "manual" {
		t.Fatalf("saved draft = %v", saved)
	}
	id := int64(saved["id"].(float64))
	revision := int(saved["revision"].(float64))

	resp, body = a.post("/api/v1/plan/drafts/"+strconv.FormatInt(id, 10)+"/apply", map[string]any{
		"confirm": true, "revision": revision,
	})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "writes_disarmed") {
		t.Fatalf("apply without arming = %d %s", resp.StatusCode, body)
	}

	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	resp, body = a.post("/api/v1/plan/drafts/"+strconv.FormatInt(id, 10)+"/apply", map[string]any{
		"confirm": true, "revision": revision,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}
	result := decodeMap(t, body)
	if result["plan"].(map[string]any)["status"] != "applied" {
		t.Errorf("applied = %v", result["plan"])
	}

	resp, body = a.get("/api/v1/plan?date=" + date)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view after apply = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	if view["applied"] == nil || view["locked"] != true {
		t.Fatalf("view after apply = %v", view)
	}
	resp, body = a.get("/api/v1/plan/revisions?date=" + date)
	if resp.StatusCode != http.StatusOK || len(decodeMap(t, body)["revisions"].([]any)) < 2 {
		t.Fatalf("revisions = %d %s", resp.StatusCode, body)
	}
}

func seedPricesAndTelemetry(t *testing.T, a *testAPI) {
	t.Helper()
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
}

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
	view := decodeMap(t, body)
	auto := view["auto"].(map[string]any)
	if auto["id"].(float64) != plan["id"].(float64) {
		t.Errorf("latest auto plan differs")
	}

	// Apply the automatic plan through the armed simulator path.
	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	resp, body = a.post("/api/v1/plan/auto/apply", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply auto = %d %s", resp.StatusCode, body)
	}
	applied := decodeMap(t, body)["plan"].(map[string]any)
	if applied["status"] != "applied" {
		t.Errorf("applied status = %v", applied["status"])
	}
	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get applied plan = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	if view["applied"] == nil || view["effective"] == nil {
		t.Errorf("applied view = %v", view)
	}

	resp, body = a.post("/api/v1/schedule/clear", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/schedule")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("schedule = %d %s", resp.StatusCode, body)
	}
	schedule := decodeMap(t, body)["schedule"].(map[string]any)
	days := schedule["days"].(map[string]any)
	for day, raw := range days {
		if len(raw.([]any)) != 0 {
			t.Errorf("%s still has slots after clear", day)
		}
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
