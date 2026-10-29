package solplanet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
)

func TestDecodeSlotGoldenVectors(t *testing.T) {
	cases := []struct {
		code      uint32
		hour      int
		minute    int
		duration  int
		discharge bool
	}{
		{0x1E02, 0, 0, 30, false},
		{0x011E1E02, 1, 30, 30, false},
		{0x02003C02, 2, 0, 60, false},
		{0x04007802, 4, 0, 120, false},
		{0x0200B402, 2, 0, 180, false},
		{0x0E1E3C02, 14, 30, 60, false},
		{0x0B1EB402, 11, 30, 180, false},
		{0x13003C03, 19, 0, 60, true},
		{0x1EF002, 0, 30, 240, false},
	}
	for _, tc := range cases {
		slot, err := DecodeSlot(tc.code)
		if err != nil {
			t.Fatalf("%#08x: %v", tc.code, err)
		}
		if slot.Hour != tc.hour || slot.Minute != tc.minute || slot.DurationMin != tc.duration || slot.Discharge != tc.discharge {
			t.Errorf("%#08x: got %+v", tc.code, slot)
		}
		if slot.Code != tc.code {
			t.Errorf("%#08x: code not preserved", tc.code)
		}
	}
	empty, err := DecodeSlot(0)
	if err != nil || empty.Code != 0 || empty.DurationMin != 0 {
		t.Fatalf("empty slot = %+v, %v", empty, err)
	}
}

func TestDecodeSlotRejectsInvalid(t *testing.T) {
	bad := []uint32{
		0x18000002, // hour 24
		0x000F0002, // minute 15
		0x00001402, // 20 minutes
		0x0000FA02, // 250 minutes
		0x001E0000, // marker bit missing
		0x001E0202, // unexpected marker bits
	}
	for _, code := range bad {
		if _, err := DecodeSlot(code); err == nil {
			t.Errorf("%#08x: expected error", code)
		}
	}
}

func TestEncodeSlotGolden(t *testing.T) {
	cases := []struct {
		slot domain.ScheduleSlot
		code uint32
	}{
		{domain.ScheduleSlot{Hour: 0, Minute: 0, DurationMin: 30}, 0x1E02},
		{domain.ScheduleSlot{Hour: 0, Minute: 0, DurationMin: 30, Discharge: true}, 0x1E03},
		{domain.ScheduleSlot{Hour: 2, Minute: 0, DurationMin: 60}, 0x02003C02},
	}
	for _, tc := range cases {
		code, err := EncodeSlot(tc.slot)
		if err != nil {
			t.Fatal(err)
		}
		if code != tc.code {
			t.Errorf("%+v = %#08x, want %#08x", tc.slot, code, tc.code)
		}
	}
	if _, err := EncodeSlot(domain.ScheduleSlot{Hour: 0, Minute: 15, DurationMin: 30}); err == nil {
		t.Error("expected bad-minute error")
	}
}

func TestEncodeScheduleRoundTrip(t *testing.T) {
	schedule := domain.Schedule{
		Days: map[string][]domain.ScheduleSlot{
			"Mon": {{Hour: 2, Minute: 30, DurationMin: 120}, {Hour: 16, Minute: 0, DurationMin: 60, Discharge: true}},
		},
		PinW: 6000, PoutW: 3000,
	}
	payload, err := EncodeSchedule(schedule)
	if err != nil {
		t.Fatal(err)
	}
	decoded, warnings, err := DecodeSchedule(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if len(decoded.Days["Mon"]) != 2 || decoded.Days["Mon"][0].DurationMin != 120 || decoded.PinW != 6000 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestDecodeScheduleFixture(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "solplanet", "getdefine.json"))
	if err != nil {
		t.Fatal(err)
	}
	schedule, warnings, err := DecodeSchedule(payload)
	if err != nil {
		t.Fatalf("DecodeSchedule: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	if schedule.PinW != 4000 || schedule.PoutW != 8000 {
		t.Errorf("Pin/Pout = %v/%v", schedule.PinW, schedule.PoutW)
	}
	for _, day := range DayKeys {
		if _, ok := schedule.Days[day]; !ok {
			t.Errorf("missing day %s", day)
		}
	}
	if len(schedule.Days["Mon"]) != 2 {
		t.Errorf("Mon slots = %+v", schedule.Days["Mon"])
	}
	if len(schedule.Days["Thu"]) != 3 {
		t.Errorf("Thu slots = %+v", schedule.Days["Thu"])
	}
	first := schedule.Days["Mon"][0]
	if first.Hour != 2 || first.Minute != 30 || first.DurationMin != 120 {
		t.Errorf("first Mon slot = %+v", first)
	}
}

func TestDecodeScheduleRejectsBadJSON(t *testing.T) {
	if _, _, err := DecodeSchedule([]byte("not json")); err == nil {
		t.Fatal("expected error")
	}
}

type fixtureSet struct {
	device2  []byte
	device3  []byte
	device4  []byte
	identity []byte
	battery  []byte
	define   []byte
}

func loadFixtures(t *testing.T) fixtureSet {
	t.Helper()
	read := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "solplanet", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return data
	}
	return fixtureSet{
		device2:  read("getdevdata_device2.json"),
		device3:  read("getdevdata_device3.json"),
		device4:  read("getdevdata_device4.json"),
		identity: read("getdev_device2.json"),
		battery:  read("getdev_device4.json"),
		define:   read("getdefine.json"),
	}
}

func newFixtureServer(t *testing.T, set fixtureSet) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/getdev.cgi":
			switch r.URL.Query().Get("device") {
			case "2":
				_, _ = w.Write(set.identity)
			case "4":
				_, _ = w.Write(set.battery)
			default:
				http.NotFound(w, r)
			}
		case "/getdevdata.cgi":
			switch r.URL.Query().Get("device") {
			case "2":
				_, _ = w.Write(set.device2)
			case "3":
				_, _ = w.Write(set.device3)
			case "4":
				_, _ = w.Write(set.device4)
			default:
				http.NotFound(w, r)
			}
		case "/getdefine.cgi":
			_, _ = w.Write(set.define)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(t *testing.T, srv *httptest.Server, signs inverter.Signs, serial string) *Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	if p := u.Port(); p != "" {
		parsed, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("port: %v", err)
		}
		port = parsed
	}
	c := New(Config{
		Scheme: u.Scheme,
		Host:   u.Hostname(),
		Port:   port,
		Serial: serial,
		Signs:  signs,
	})
	c.now = func() time.Time { return time.Date(2026, 10, 29, 21, 14, 6, 0, time.UTC) }
	return c
}

func TestClientIdentityAndRead(t *testing.T) {
	srv := newFixtureServer(t, loadFixtures(t))
	c := clientFor(t, srv, inverter.Signs{}, "")

	id, err := c.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if id.Serial != "SIM000000000001" || id.Model != "ASW08kH-T2" {
		t.Errorf("identity = %+v", id)
	}
	if id.CapacityKWh != 12.812 || id.WorkMode != 2 || id.MinSOC != 10 || id.MaxSOC != 100 {
		t.Errorf("battery identity = %+v", id)
	}
	if id.RatedPowerW != 8000 || id.Phases != 3 || id.SafetyCode != 87 {
		t.Errorf("inverter identity = %+v", id)
	}

	r, err := c.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if r.SOC != 94 || r.SOH != 98 {
		t.Errorf("SOC/SOH = %v/%v", r.SOC, r.SOH)
	}
	if r.GridW != 39 {
		t.Errorf("grid = %v", r.GridW)
	}
	if r.BatteryW != -2187 {
		t.Errorf("battery = %v", r.BatteryW)
	}
	if r.PVW != 132 {
		t.Errorf("pv = %v", r.PVW)
	}
	if r.LoadW != 2358 {
		t.Errorf("load = %v", r.LoadW)
	}
	if r.Mode != 2 {
		t.Errorf("mode = %v", r.Mode)
	}
	if len(r.PVStrings) != 2 || r.PVStrings[0] != 65.7 || r.PVStrings[1] != 46.5 {
		t.Errorf("pv strings = %v", r.PVStrings)
	}
	if r.Extra["bst"] != 3 || r.Extra["battery_voltage_v"] != 317.0 || r.Extra["battery_temp_c"] != 26.5 {
		t.Errorf("extra = %v", r.Extra)
	}
	if r.Extra["pv_ac_w"] != 2561.0 || r.Extra["grid_import_today_kwh"] != 9.66 {
		t.Errorf("extra = %v", r.Extra)
	}
	if !r.At.Equal(time.Date(2026, 10, 29, 21, 14, 6, 0, time.UTC)) {
		t.Errorf("timestamp = %s", r.At)
	}
}

func TestClientSignOverrides(t *testing.T) {
	srv := newFixtureServer(t, loadFixtures(t))
	c := clientFor(t, srv, inverter.Signs{PV: -1, Grid: -1, Battery: -1}, "")
	r, err := c.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.GridW != -39 || r.BatteryW != 2187 || r.PVW != -132 {
		t.Fatalf("signs not applied: %+v", r)
	}
}

func TestClientSerialMismatch(t *testing.T) {
	srv := newFixtureServer(t, loadFixtures(t))
	c := clientFor(t, srv, inverter.Signs{}, "OTHER-SERIAL")
	_, err := c.Identity(context.Background())
	if err == nil || !strings.Contains(err.Error(), "serial mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientSchedule(t *testing.T) {
	srv := newFixtureServer(t, loadFixtures(t))
	c := clientFor(t, srv, inverter.Signs{}, "")
	schedule, err := c.Schedule(context.Background())
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(schedule.Days["Sun"]) != 1 || len(schedule.Days["Mon"]) != 2 {
		t.Errorf("schedule = %+v", schedule.Days)
	}
}

func TestClientHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := clientFor(t, srv, inverter.Signs{}, "")
	_, err := c.Identity(context.Background())
	if err == nil || !strings.Contains(err.Error(), "serial") {
		t.Fatalf("expected 404 hint, got %v", err)
	}
}
