package simulator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/inverter/solplanet"
)

func fixedInverter(t *testing.T, now *time.Time) *Inverter {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Serial:        "SIM-ASW08KH-0001",
		Strings:       2,
		PeakW:         6000,
		CapacityKWh:   15.36,
		MaxChargeW:    6000,
		MaxDischargeW: 3000,
		StartSOC:      50,
		Location:      loc,
		Now:           func() time.Time { return *now },
	})
}

func TestSimulatorReadAndSOC(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	sim := fixedInverter(t, &now)
	reading, err := sim.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.PVW <= 0 {
		t.Errorf("pv = %v at noon", reading.PVW)
	}
	if len(reading.PVStrings) != 2 || reading.PVStrings[0] <= 0 {
		t.Errorf("pv strings = %v", reading.PVStrings)
	}
	if reading.SOC < 50 || reading.SOC > 56 {
		t.Errorf("soc = %v", reading.SOC)
	}
	if reading.LoadW < 100 {
		t.Errorf("load = %v", reading.LoadW)
	}
	balance := reading.PVW + reading.GridW - reading.BatteryW
	if diff := balance - reading.LoadW; diff > 1 || diff < -1 {
		t.Errorf("energy balance off by %v: %+v", diff, reading)
	}
	startSOC := reading.SOC
	now = now.Add(time.Hour)
	reading, err = sim.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.SOC <= startSOC {
		t.Errorf("soc should rise while charging: %v -> %v", startSOC, reading.SOC)
	}
}

func TestSimulatorNightDischarges(t *testing.T) {
	now := time.Date(2026, 12, 21, 23, 0, 0, 0, time.UTC)
	sim := fixedInverter(t, &now)
	reading, err := sim.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.PVW != 0 {
		t.Errorf("pv at night = %v", reading.PVW)
	}
	if reading.BatteryW >= 0 {
		t.Errorf("battery should discharge at night: %v", reading.BatteryW)
	}
	if reading.GridW < 0 {
		t.Errorf("grid should not export at night: %v", reading.GridW)
	}
}

func TestSimulatorHTTPHandlerWithClient(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	sim := fixedInverter(t, &now)
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	client := solplanet.New(solplanet.Config{
		Scheme: u.Scheme,
		Host:   u.Hostname(),
		Port:   port,
		Serial: "SIM-ASW08KH-0001",
	})
	identity, err := client.Identity(context.Background())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if identity.Serial != "SIM-ASW08KH-0001" || identity.CapacityKWh != 15.36 {
		t.Errorf("identity = %+v", identity)
	}
	reading, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.SOC <= 0 || reading.PVW <= 0 {
		t.Errorf("reading = %+v", reading)
	}
	schedule, err := client.Schedule(context.Background())
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(schedule.Days) != 7 {
		t.Errorf("schedule days = %d", len(schedule.Days))
	}
}

func TestSimulatorRejectsWrites(t *testing.T) {
	now := time.Now()
	sim := fixedInverter(t, &now)
	srv := httptest.NewServer(sim.Handler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/setting.cgi", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("write status = %d, want 405", resp.StatusCode)
	}
}
