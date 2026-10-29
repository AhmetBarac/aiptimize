package solplanet

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/inverter"
)

// TestLiveDongleReadOnly is the HIL read-only smoke test. It is skipped unless
// AIPTIMIZE_HIL_HOST is set, so CI never depends on hardware. It only issues GET
// requests; writes are not implemented by the client.
//
//	AIPTIMIZE_HIL_HOST=<dongle-ip> go test ./internal/inverter/solplanet -run Live -v
func TestLiveDongleReadOnly(t *testing.T) {
	host := os.Getenv("AIPTIMIZE_HIL_HOST")
	if host == "" {
		t.Skip("set AIPTIMIZE_HIL_HOST to run the read-only hardware test")
	}
	serial := os.Getenv("AIPTIMIZE_HIL_SERIAL")
	client := New(Config{Host: host, Serial: serial, Timeout: 10 * time.Second})
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	identity, err := client.Identity(ctx)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if identity.Serial == "" || identity.Model == "" {
		t.Fatalf("incomplete identity: %+v", identity)
	}
	t.Logf("identity: model=%s serial=%s firmware=%s capacity=%.3fkWh mod_r=%d",
		identity.Model, identity.Serial, identity.Firmware, identity.CapacityKWh, identity.WorkMode)

	reading, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reading.SOC < 0 || reading.SOC > 100 {
		t.Fatalf("soc out of range: %v", reading.SOC)
	}
	if reading.LoadW < 0 {
		t.Fatalf("negative load: %v", reading.LoadW)
	}
	t.Logf("reading: soc=%.0f%% pv=%.0fW strings=%v grid=%.0fW battery=%.0fW load=%.0fW mode=%d",
		reading.SOC, reading.PVW, reading.PVStrings, reading.GridW, reading.BatteryW, reading.LoadW, reading.Mode)

	schedule, err := client.Schedule(ctx)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if len(schedule.Days) != 7 {
		t.Fatalf("schedule days = %d", len(schedule.Days))
	}
	for _, day := range DayKeys {
		t.Logf("%s: %+v", day, schedule.Days[day])
	}

	var _ inverter.Reader = client
}
