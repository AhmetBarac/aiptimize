package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/inverter"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

func TestPlannerRequiresFreshSOCAndPrices(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "planner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	priceSvc := prices.New(st, cfg, nil, log)
	fake := &fakeInverter{reading: inverter.Reading{At: time.Now(), SOC: 50}}
	poller := NewTelemetryPoller(cfg, st, log, nil, TelemetryOptions{
		NewReader: func(settings.Values) (inverter.Reader, error) { return fake, nil },
	})
	plannerSvc := NewPlannerService(st, cfg, priceSvc, nil, poller, log, nil)

	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrSOCUnavailable) {
		t.Fatalf("without telemetry = %v", err)
	}
	if _, err := poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	plannerSvc.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrSOCUnavailable) {
		t.Fatalf("stale telemetry = %v", err)
	}
	plannerSvc.now = time.Now
	if _, err := plannerSvc.Compute(ctx); !errors.Is(err, ErrPricesUnavailable) {
		t.Fatalf("without prices = %v", err)
	}
}

func TestPlannerLatestUnavailable(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "planner-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, _ := settings.New(ctx, st)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	plannerSvc := NewPlannerService(st, cfg, nil, nil, nil, log, nil)
	if _, err := plannerSvc.Latest(ctx); !errors.Is(err, ErrPlanUnavailable) {
		t.Fatalf("latest = %v", err)
	}
}
