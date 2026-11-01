package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

type fakeInverter struct {
	mu       sync.Mutex
	reading  inverter.Reading
	err      error
	calls    int
	closed   int
	schedule domain.Schedule
}

func (f *fakeInverter) Identity(context.Context) (inverter.Identity, error) {
	return inverter.Identity{Serial: "FAKE-1", Model: "fake", CapacityKWh: 15}, nil
}

func (f *fakeInverter) Read(context.Context) (inverter.Reading, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
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

func (f *fakeInverter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

type capturePublisher struct {
	mu     sync.Mutex
	events []string
}

func (p *capturePublisher) Publish(event string, _ any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func newTelemetryTest(t *testing.T) (*TelemetryPoller, *fakeInverter, *settings.Service, *store.Store, *capturePublisher) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 21, 14, 6, 0, time.UTC)
	fake := &fakeInverter{reading: inverter.Reading{
		At: now, PVW: 132, PVStrings: []float64{65.7, 46.5},
		GridW: 39, BatteryW: -2187, LoadW: 2358, SOC: 94, SOH: 98, Mode: 2,
		Extra: map[string]any{"bst": 3},
	}}
	pub := &capturePublisher{}
	poller := NewTelemetryPoller(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), pub, TelemetryOptions{
		NewReader: func(settings.Values) (inverter.Reader, error) { return fake, nil },
	})
	poller.now = func() time.Time { return now }
	return poller, fake, cfg, st, pub
}

func TestTelemetryPollerStoresAndPublishes(t *testing.T) {
	poller, _, _, st, pub := newTelemetryTest(t)
	sample, err := poller.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if sample.SOC != 94 || sample.GridW != 39 || sample.BatteryW != -2187 {
		t.Fatalf("sample = %+v", sample)
	}
	stored, err := st.LatestTelemetry(context.Background())
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	if stored.SOC != 94 || len(stored.PVStrings) != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	latest, at, ok := poller.Latest()
	if !ok || latest.SOC != 94 || at.IsZero() {
		t.Fatalf("latest = %+v, %v, %v", latest, at, ok)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.events) != 1 || pub.events[0] != "telemetry" {
		t.Fatalf("events = %v", pub.events)
	}
}

func TestTelemetryPollerKeepsLastSampleOnError(t *testing.T) {
	poller, fake, _, _, _ := newTelemetryTest(t)
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.err = errors.New("dongle offline")
	fake.mu.Unlock()
	if _, err := poller.PollOnce(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	latest, _, ok := poller.Latest()
	if !ok || latest.SOC != 94 {
		t.Fatalf("last sample lost: %+v", latest)
	}
}

func TestTelemetryPollerRebuildsReaderOnSettingsChange(t *testing.T) {
	poller, _, cfg, _, _ := newTelemetryTest(t)
	builds := 0
	poller.opts.NewReader = func(settings.Values) (inverter.Reader, error) {
		builds++
		return &fakeInverter{reading: inverter.Reading{At: time.Now(), SOC: 50}}, nil
	}
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if builds != 1 {
		t.Fatalf("reader built %d times, want 1", builds)
	}
	if _, err := cfg.Patch(context.Background(), settings.SectionInverter, []byte(`{"telemetry_interval_s":60}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if builds != 2 {
		t.Fatalf("reader built %d times after settings change, want 2", builds)
	}
}

func TestTelemetryPollerTestReadsWithoutStoring(t *testing.T) {
	poller, _, _, st, _ := newTelemetryTest(t)
	identity, reading, err := poller.Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Serial != "FAKE-1" || reading.SOC != 94 {
		t.Fatalf("test = %+v %+v", identity, reading)
	}
	if _, err := st.LatestTelemetry(context.Background()); err == nil {
		t.Fatal("Test must not store a sample")
	}
}
