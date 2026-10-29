package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/config"
	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/forecast/pv"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/service"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

type testAPI struct {
	t         *testing.T
	server    *httptest.Server
	client    *http.Client
	jar       http.CookieJar
	store     *store.Store
	settings  *settings.Service
	prices    *prices.Service
	fetcher   *fakePriceFetcher
	telemetry *service.TelemetryPoller
	inverter  *fakeInverter
	forecast  *service.ForecastService
	planner   *service.PlannerService
	ev        *service.EVService
	srv       *Server
}

// apiPVFetcher returns a constant 800 W/m² series for every request.
type apiPVFetcher struct{}

func (f *apiPVFetcher) Fetch(context.Context, pv.Request) (pv.Hourly, error) {
	start := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	var h pv.Hourly
	for i := 0; i < 72; i++ {
		h.Time = append(h.Time, start.Add(time.Duration(i)*time.Hour))
		h.GTI = append(h.GTI, 800)
		h.Temp = append(h.Temp, 25)
	}
	return h, nil
}

type fakeInverter struct {
	mu       sync.Mutex
	reading  inverter.Reading
	err      error
	schedule domain.Schedule
}

func (f *fakeInverter) Identity(context.Context) (inverter.Identity, error) {
	return inverter.Identity{
		Serial: "SIM000000000001", Model: "ASW08kH-T2", Firmware: "SIM-1.0",
		RatedPowerW: 8000, Phases: 3, CapacityKWh: 12.812, WorkMode: 2,
	}, nil
}

func (f *fakeInverter) Read(context.Context) (inverter.Reading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return inverter.Reading{}, f.err
	}
	return f.reading, nil
}

func (f *fakeInverter) Schedule(context.Context) (domain.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.schedule.Days == nil {
		return domain.Schedule{Days: map[string][]domain.ScheduleSlot{}}, nil
	}
	return f.schedule, nil
}

func (f *fakeInverter) WriteSchedule(_ context.Context, schedule domain.Schedule) (domain.Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.schedule = schedule
	return schedule, nil
}

func (f *fakeInverter) Close() error { return nil }

type fakePriceFetcher struct {
	mu    sync.Mutex
	fn    func(ctx context.Context, req prices.Request) (prices.Day, error)
	calls int
}

func (f *fakePriceFetcher) FetchDay(ctx context.Context, req prices.Request) (prices.Day, error) {
	f.mu.Lock()
	f.calls++
	fn := f.fn
	f.mu.Unlock()
	if fn == nil {
		return prices.Day{}, prices.ErrNotPublished
	}
	return fn(ctx, req)
}

func (f *fakePriceFetcher) set(fn func(ctx context.Context, req prices.Request) (prices.Day, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fn = fn
}

func newTestAPI(t *testing.T, web fs.FS) *testAPI {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Listen:       "127.0.0.1:8080",
		DataDir:      dir,
		DatabasePath: filepath.Join(dir, "api.db"),
		Timezone:     "UTC",
		LogLevel:     slog.LevelError,
		LogFormat:    "text",
		CookieSecure: config.CookieSecureNever,
		Version:      "test",
	}
	st, err := store.Open(context.Background(), cfg.DatabasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	svc, err := settings.New(context.Background(), st)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fetcher := &fakePriceFetcher{}
	priceSvc := prices.New(st, svc, fetcher, logger)
	fakeInv := &fakeInverter{reading: inverter.Reading{
		At: time.Now().UTC(), SOC: 94, PVW: 132, PVStrings: []float64{65.7, 46.5},
		GridW: 39, BatteryW: -2187, LoadW: 2358, SOH: 98, Mode: 2,
		Extra: map[string]any{"bst": 3},
	}}
	telemetrySvc := service.NewTelemetryPoller(svc, st, logger, nil, service.TelemetryOptions{
		NewReader: func(settings.Values) (inverter.Reader, error) { return fakeInv, nil },
	})
	forecastSvc := service.NewForecastService(svc, st, logger, nil, service.ForecastOptions{
		NewFetcher: func(settings.Values) (pv.Fetcher, error) { return &apiPVFetcher{}, nil },
	})
	solarSvc := service.NewSolarService(svc, st, forecastSvc)
	plannerSvc := service.NewPlannerService(st, svc, priceSvc, forecastSvc, telemetrySvc, logger, nil)
	evSvc := service.NewEVService(svc, st, priceSvc, forecastSvc, logger, true, true, time.Now)
	plannerSvc.SetEVProvider(evSvc)
	historySvc := service.NewHistoryService(st, svc, priceSvc, plannerSvc)
	srv := New(Deps{
		Config: cfg, Store: st, Settings: svc, Prices: priceSvc,
		Telemetry: telemetrySvc, Forecast: forecastSvc, Solar: solarSvc, Planner: plannerSvc,
		EV: evSvc, History: historySvc, Logger: logger, WebFS: web,
	})
	ts := httptest.NewServer(srv.Handler())

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	t.Cleanup(func() {
		ts.Close()
		_ = st.Close()
	})
	return &testAPI{
		t: t, server: ts, client: client, jar: jar, store: st,
		settings: svc, prices: priceSvc, fetcher: fetcher,
		telemetry: telemetrySvc, inverter: fakeInv, forecast: forecastSvc, planner: plannerSvc,
		ev: evSvc, srv: srv,
	}
}

func (a *testAPI) csrf() string {
	a.t.Helper()
	u, _ := url.Parse(a.server.URL)
	for _, c := range a.jar.Cookies(u) {
		if c.Name == csrfCookieName {
			return c.Value
		}
	}
	return ""
}

func (a *testAPI) ensureCSRF() string {
	a.t.Helper()
	if tok := a.csrf(); tok != "" {
		return tok
	}
	a.get("/api/v1/meta")
	return a.csrf()
}

func (a *testAPI) do(method, path string, body any, csrf string) (*http.Response, []byte) {
	a.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.server.URL+path, rdr)
	if err != nil {
		a.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set(csrfHeaderName, csrf)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		a.t.Fatalf("read body: %v", err)
	}
	return resp, data
}

func (a *testAPI) get(path string) (*http.Response, []byte) {
	return a.do(http.MethodGet, path, nil, "")
}

func (a *testAPI) post(path string, body any) (*http.Response, []byte) {
	return a.do(http.MethodPost, path, body, a.ensureCSRF())
}

func (a *testAPI) setup() {
	a.t.Helper()
	resp, body := a.post("/api/v1/setup", map[string]any{
		"username": "admin",
		"password": "very-secret-1",
		"site":     map[string]any{"name": "Test Home", "timezone": "UTC"},
	})
	if resp.StatusCode != http.StatusCreated {
		a.t.Fatalf("setup status = %d, body = %s", resp.StatusCode, body)
	}
}

func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return m
}

func TestPublicEndpoints(t *testing.T) {
	a := newTestAPI(t, nil)

	resp, body := a.get("/healthz")
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("healthz: %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/readyz")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ready") {
		t.Fatalf("readyz: %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/api/v1/meta")
	if resp.StatusCode != 200 {
		t.Fatalf("meta: %d %s", resp.StatusCode, body)
	}
	meta := decodeMap(t, body)
	if meta["setup_required"] != true {
		t.Errorf("setup_required = %v", meta["setup_required"])
	}
	if meta["version"] != "test" {
		t.Errorf("version = %v", meta["version"])
	}
	if a.csrf() == "" {
		t.Error("CSRF cookie was not issued")
	}
	features := meta["features"].(map[string]any)
	if features["prices"] != true || features["telemetry"] != true || features["forecast"] != true ||
		features["battery_plan"] != true || features["actuation"] != true || features["plan_editing"] != true ||
		features["ev_charging"] != true || features["history"] != true || features["phase"].(float64) != 7 {
		t.Errorf("features = %v", features)
	}

	resp, body = a.get("/api/v1/status")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d %s", resp.StatusCode, body)
	}
	if code := decodeMap(t, body)["error"].(map[string]any)["code"]; code != "unauthorized" {
		t.Errorf("error code = %v", code)
	}

	resp, body = a.get("/api/v1/nope")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown endpoint = %d %s", resp.StatusCode, body)
	}
	resp, body = a.get("/healthz")
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("security headers missing: %v", resp.Header)
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Errorf("CSP missing")
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}
}

func TestSetupLoginLogout(t *testing.T) {
	a := newTestAPI(t, nil)

	// Login before setup is refused.
	resp, body := a.do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "very-secret-1",
	}, a.ensureCSRF())
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "setup_required") {
		t.Fatalf("login before setup = %d %s", resp.StatusCode, body)
	}

	// State-changing requests require CSRF.
	resp, body = a.do(http.MethodPost, "/api/v1/setup", map[string]any{
		"username": "admin", "password": "very-secret-1",
	}, "")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "csrf_failed") {
		t.Fatalf("setup without csrf = %d %s", resp.StatusCode, body)
	}

	// Setup succeeds and logs in.
	a.setup()

	resp, body = a.get("/api/v1/auth/me")
	if resp.StatusCode != 200 {
		t.Fatalf("me = %d %s", resp.StatusCode, body)
	}
	if user := decodeMap(t, body)["user"].(map[string]any); user["username"] != "admin" {
		t.Errorf("me user = %v", user)
	}

	// Second setup is rejected.
	resp, body = a.post("/api/v1/setup", map[string]any{"username": "other", "password": "very-secret-2"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second setup = %d %s", resp.StatusCode, body)
	}

	// Status works while authenticated.
	resp, body = a.get("/api/v1/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d %s", resp.StatusCode, body)
	}
	status := decodeMap(t, body)
	if status["site"].(map[string]any)["name"] != "Test Home" {
		t.Errorf("status site = %v", status["site"])
	}

	// Logout revokes the session.
	resp, _ = a.post("/api/v1/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d", resp.StatusCode)
	}
	resp, _ = a.get("/api/v1/status")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d", resp.StatusCode)
	}

	// Wrong password then correct login.
	resp, _ = a.post("/api/v1/auth/login", map[string]any{"username": "admin", "password": "wrong-password"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong login = %d", resp.StatusCode)
	}
	resp, body = a.post("/api/v1/auth/login", map[string]any{"username": "admin", "password": "very-secret-1"})
	if resp.StatusCode != 200 {
		t.Fatalf("login = %d %s", resp.StatusCode, body)
	}
	resp, _ = a.get("/api/v1/status")
	if resp.StatusCode != 200 {
		t.Fatalf("status after login = %d", resp.StatusCode)
	}
}

func TestSettingsPatches(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()

	resp, body := a.get("/api/v1/settings")
	if resp.StatusCode != 200 {
		t.Fatalf("get settings = %d %s", resp.StatusCode, body)
	}
	all := decodeMap(t, body)
	if all["site"] == nil || all["inverter"] == nil || all["safety"] == nil {
		t.Fatalf("missing sections: %v", all)
	}

	patch := map[string]any{"section": "site", "values": map[string]any{"name": "Cabin"}}
	resp, body = a.do(http.MethodPatch, "/api/v1/settings", patch, a.ensureCSRF())
	if resp.StatusCode != 200 {
		t.Fatalf("patch settings = %d %s", resp.StatusCode, body)
	}
	if a.settings.Get().Site.Name != "Cabin" {
		t.Errorf("site not updated: %+v", a.settings.Get().Site)
	}

	// Validation failure.
	patch = map[string]any{"section": "site", "values": map[string]any{"timezone": "Mars/Olympus"}}
	resp, body = a.do(http.MethodPatch, "/api/v1/settings", patch, a.ensureCSRF())
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "validation_failed") {
		t.Fatalf("invalid patch = %d %s", resp.StatusCode, body)
	}

	// Unknown field.
	patch = map[string]any{"section": "site", "values": map[string]any{"nope": 1}}
	resp, body = a.do(http.MethodPatch, "/api/v1/settings", patch, a.ensureCSRF())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field = %d %s", resp.StatusCode, body)
	}

	// Missing CSRF.
	patch = map[string]any{"section": "site", "values": map[string]any{"name": "X"}}
	resp, _ = a.do(http.MethodPatch, "/api/v1/settings", patch, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("patch without csrf = %d", resp.StatusCode)
	}

	// Events feed is available and contains the setup event.
	resp, body = a.get("/api/v1/events")
	if resp.StatusCode != 200 {
		t.Fatalf("events = %d %s", resp.StatusCode, body)
	}
	events := decodeMap(t, body)["events"].([]any)
	if len(events) == 0 {
		t.Error("expected at least the setup event")
	}
}

func TestLoginRateLimit(t *testing.T) {
	a := newTestAPI(t, nil)
	a.setup()
	a.post("/api/v1/auth/logout", nil)

	for i := 0; i < 5; i++ {
		resp, _ := a.post("/api/v1/auth/login", map[string]any{"username": "admin", "password": "bad"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, resp.StatusCode)
		}
	}
	resp, body := a.post("/api/v1/auth/login", map[string]any{"username": "admin", "password": "very-secret-1"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate limit = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
}

func TestOriginMismatch(t *testing.T) {
	a := newTestAPI(t, nil)
	token := a.ensureCSRF()
	req, _ := http.NewRequest(http.MethodPost, a.server.URL+"/api/v1/setup",
		strings.NewReader(`{"username":"admin","password":"very-secret-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(csrfHeaderName, token)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("origin mismatch = %d", resp.StatusCode)
	}
}

func TestStaticSPA(t *testing.T) {
	web := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>app</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('app')")},
	}
	a := newTestAPI(t, web)

	resp, body := a.get("/")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "title>app") {
		t.Fatalf("index = %d %s", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index cache = %q", cc)
	}

	resp, body = a.get("/plan")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "title>app") {
		t.Fatalf("spa fallback = %d %s", resp.StatusCode, body)
	}

	resp, _ = a.get("/assets/app.js")
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("asset = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset cache = %q", cc)
	}

	resp, _ = a.get("/missing.js")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset = %d", resp.StatusCode)
	}
}

func TestStaticPlaceholder(t *testing.T) {
	a := newTestAPI(t, nil)
	resp, body := a.get("/")
	if resp.StatusCode != 200 || !strings.Contains(string(body), "web UI has not been built") {
		t.Fatalf("placeholder = %d %s", resp.StatusCode, body)
	}
}

func TestBroker(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe()
	defer cancel()
	b.Publish("plan", map[string]any{"id": 1})
	select {
	case frame := <-ch:
		if !strings.Contains(string(frame), "event: plan") || !strings.Contains(string(frame), `"id":1`) {
			t.Fatalf("frame = %s", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("no frame received")
	}
	cancel()
	// Publishing after cancel must not panic.
	b.Publish("plan", map[string]any{"id": 2})
}
