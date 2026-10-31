package api

import (
	"context"
	"fmt"
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
		if start.After(now.Add(12 * time.Hour)) {
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

func TestPlanDownload(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	resp, body := a.post("/api/v1/plan/recompute", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recompute = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan = %d %s", resp.StatusCode, body)
	}
	effective, _ := decodeMap(t, body)["effective"].(map[string]any)
	if effective == nil || len(effective["slots"].([]any)) == 0 {
		t.Skip("no plan slots to download at this time")
	}
	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	resp, body = a.post("/api/v1/plan/download", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download = %d %s", resp.StatusCode, body)
	}
	result := decodeMap(t, body)
	if result["plan"].(map[string]any)["status"] != "applied" {
		t.Errorf("plan = %v", result["plan"])
	}
	resp, body = a.get("/api/v1/schedule")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("schedule = %d %s", resp.StatusCode, body)
	}
	if decodeMap(t, body)["matches"] != true {
		t.Errorf("schedule does not match: %s", body)
	}
	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan = %d %s", resp.StatusCode, body)
	}
	inverter, ok := decodeMap(t, body)["inverter_plan"].(map[string]any)
	if !ok {
		t.Fatalf("inverter plan missing: %s", body)
	}
	if len(inverter["slots"].([]any)) == 0 {
		t.Errorf("inverter plan has no slots: %s", body)
	}
}

func TestPlanDraftRevisionGuard(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	resp, body := a.post("/api/v1/plan/recompute", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recompute = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/plan")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view = %d %s", resp.StatusCode, body)
	}
	view := decodeMap(t, body)
	auto := view["auto"].(map[string]any)
	autoID := int64(auto["id"].(float64))
	autoRevision := int(auto["revision"].(float64))

	now := time.Now()
	var chosen map[string]any
	for _, raw := range auto["slots"].([]any) {
		slot := raw.(map[string]any)
		start, _ := time.Parse(time.RFC3339, slot["start"].(string))
		if start.After(now.Add(12 * time.Hour)) {
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
	slot := map[string]any{
		"start": start.Format(time.RFC3339), "end": start.Add(30 * time.Minute).Format(time.RFC3339),
		"direction": "charge", "power_w": 500,
	}
	save := func(revision int, basePlanID int64) (*http.Response, []byte) {
		return a.post("/api/v1/plan/drafts", map[string]any{
			"date": date, "revision": revision, "base_plan_id": basePlanID,
			"slots": []any{slot}, "note": "edited",
		})
	}

	resp, body = save(autoRevision, autoID)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first save = %d %s", resp.StatusCode, body)
	}
	saved := decodeMap(t, body)["plan"].(map[string]any)
	savedID := int64(saved["id"].(float64))
	savedRevision := int(saved["revision"].(float64))
	resp, body = a.post(fmt.Sprintf("/api/v1/plan/drafts/%d/apply", savedID), map[string]any{
		"confirm": true, "revision": savedRevision,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply = %d %s", resp.StatusCode, body)
	}

	resp, body = a.get("/api/v1/plan?date=" + date)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view after apply = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	applied := view["applied"].(map[string]any)
	appliedID := int64(applied["id"].(float64))
	appliedRevision := int(applied["revision"].(float64))

	// Editing an applied manual plan must not be treated as a conflict just
	// because the automatic suggestion has a different revision.
	resp, body = save(appliedRevision, appliedID)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("save after apply = %d %s", resp.StatusCode, body)
	}
	second := decodeMap(t, body)["plan"].(map[string]any)
	if got := int(second["revision"].(float64)); got != appliedRevision+1 {
		t.Fatalf("revision after apply = %d, want %d", got, appliedRevision+1)
	}

	// The new draft must not hide the still-applied plan: the applied plan
	// stays effective until the draft is resubmitted.
	resp, body = a.get("/api/v1/plan?date=" + date)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view with draft = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	if view["draft"] == nil || view["applied"] == nil {
		t.Fatalf("view with draft lost a plan: %s", body)
	}
	if got := int64(view["effective"].(map[string]any)["id"].(float64)); got != appliedID {
		t.Fatalf("effective plan = %d, want applied %d", got, appliedID)
	}

	// A stale revision from another tab is still rejected.
	resp, body = save(appliedRevision+7, appliedID)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "revision_conflict") {
		t.Fatalf("stale revision = %d %s", resp.StatusCode, body)
	}
	// So is a base plan id that no longer matches the editable plan.
	resp, body = save(appliedRevision+1, autoID)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "revision_conflict") {
		t.Fatalf("stale plan id = %d %s", resp.StatusCode, body)
	}
}

func TestPlanViewSavesInverterSync(t *testing.T) {
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
	autoID := int64(view["auto"].(map[string]any)["id"].(float64))
	day := view["date"].(string)
	sync, ok := view["sync"].(map[string]any)
	if !ok {
		t.Fatalf("plan view has no sync object: %s", body)
	}
	if sync["source"] != "check" {
		t.Errorf("sync source = %v", sync["source"])
	}
	if sync["match"] != view["inverter_match"] {
		t.Errorf("sync match = %v, inverter_match = %v", sync["match"], view["inverter_match"])
	}
	if id, _ := sync["plan_id"].(float64); int64(id) != autoID {
		t.Errorf("sync plan = %v, auto = %d", sync["plan_id"], autoID)
	}
	rows, err := a.store.ListPlanSyncs(context.Background(), day, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("sync rows = %d, want 1", len(rows))
	}
	// Polling again does not duplicate an unchanged, fresh comparison.
	if resp, body = a.get("/api/v1/plan"); resp.StatusCode != http.StatusOK {
		t.Fatalf("second view = %d %s", resp.StatusCode, body)
	}
	if rows, _ = a.store.ListPlanSyncs(context.Background(), day, 10); len(rows) != 1 {
		t.Fatalf("sync rows after poll = %d, want 1", len(rows))
	}

	// Applying the automatic plan writes the schedule; the next comparison is
	// saved as in sync.
	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	resp, body = a.post("/api/v1/plan/auto/apply", map[string]any{"confirm": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply auto = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/plan?date=" + day)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view after apply = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	sync = view["sync"].(map[string]any)
	if sync["match"] != true {
		t.Fatalf("in-sync snapshot = %v (%s)", sync, body)
	}
	rows, _ = a.store.ListPlanSyncs(context.Background(), day, 10)
	if len(rows) != 2 || !rows[0].Match || rows[0].Source != "apply_auto" {
		t.Fatalf("sync rows after apply = %+v", rows)
	}

	// A schedule that no longer matches is saved as out of sync.
	a.inverter.mu.Lock()
	a.inverter.schedule = domain.Schedule{Days: map[string][]domain.ScheduleSlot{
		"Mon": {{Hour: 1, Minute: 0, DurationMin: 30, Discharge: true}},
	}}
	a.inverter.mu.Unlock()
	resp, body = a.get("/api/v1/plan?date=" + day)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("third view = %d %s", resp.StatusCode, body)
	}
	view = decodeMap(t, body)
	sync = view["sync"].(map[string]any)
	if sync["match"] != false || len(sync["diff"].([]any)) == 0 {
		t.Fatalf("out-of-sync snapshot = %v", sync)
	}
	rows, _ = a.store.ListPlanSyncs(context.Background(), day, 10)
	if len(rows) != 3 || rows[0].Match {
		t.Fatalf("sync rows after drift = %+v", rows)
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

func TestManualControlDoesNotWrite(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	resp, body := a.post("/api/v1/plan/recompute", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recompute = %d %s", resp.StatusCode, body)
	}
	resp, body = a.post("/api/v1/plan/control", map[string]any{"locked": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("lock = %d %s", resp.StatusCode, body)
	}
	locked := decodeMap(t, body)
	if locked["locked"] != true {
		t.Errorf("not locked: %s", body)
	}
	applied, ok := locked["applied"].(map[string]any)
	if !ok || applied["origin"] != "manual" {
		t.Errorf("manual plan missing: %s", body)
	}
	if locked["last_write"] != nil {
		t.Errorf("manual control wrote to the inverter: %v", locked["last_write"])
	}

	resp, body = a.post("/api/v1/plan/control", map[string]any{"locked": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unlock = %d %s", resp.StatusCode, body)
	}
	if decodeMap(t, body)["locked"] != false {
		t.Errorf("still locked: %s", body)
	}
}

func TestSlotPowerUpdatesOnlySetpoint(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	seedPricesAndTelemetry(t, a)
	a.srv.cfg.EnableWrites = true
	if _, err := a.settings.Patch(context.Background(), settings.SectionSafety, []byte(`{"writes_armed_ui":true}`)); err != nil {
		t.Fatal(err)
	}
	loc := a.settings.Location()
	now := time.Now().In(loc)
	start := now.Truncate(30 * time.Minute)
	end := start.Add(30 * time.Minute)
	day := start.Format("2006-01-02")
	plan := domain.Plan{
		CreatedAt: now.UTC(), HorizonStart: start.UTC(), HorizonEnd: end.UTC(),
		Timezone: loc.String(), Status: domain.PlanStatusApplied, Origin: domain.OriginManual,
		Revision: 1, LockedDay: day,
		Summary: domain.PlanSummary{Currency: "SEK"},
		Slots: []domain.Slot{{
			DayLocal: day, Start: start.UTC(), End: end.UTC(), Direction: domain.DirectionDischarge,
			PowerW: 1234, EnergyKWh: 0.617, AvgImportPrice: 1, ExpectedSaving: 0.5,
		}},
	}
	if _, err := a.store.SavePlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := a.planner.DownloadPlan(context.Background(), day, "test"); err != nil {
		t.Fatal(err)
	}
	a.planner.SetWriteGate(true)
	if err := a.planner.SyncSlotPower(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := a.telemetry.ReadSchedule(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.PoutW != 1234 {
		t.Fatalf("Pout = %v, want 1234", got.PoutW)
	}
}
