package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

var testLoc = mustLoad("Europe/Stockholm")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

type fakePriceFetcher struct {
	mu    sync.Mutex
	calls map[string]int
	fail  map[string]error
	now   func() time.Time
}

func (f *fakePriceFetcher) FetchDay(_ context.Context, req prices.Request) (prices.Day, error) {
	loc := req.Location
	if loc == nil {
		loc = time.UTC
	}
	key := req.Date.In(loc).Format("2006-01-02")
	f.mu.Lock()
	f.calls[key]++
	err := f.fail[key]
	f.mu.Unlock()
	if err != nil {
		return prices.Day{}, err
	}
	start := time.Date(req.Date.In(loc).Year(), req.Date.In(loc).Month(), req.Date.In(loc).Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	var points []domain.PricePoint
	for ts := start; ts.Before(end); ts = ts.Add(15 * time.Minute) {
		points = append(points, domain.PricePoint{
			DeliveryStart: ts.UTC(), DeliveryEnd: ts.Add(15 * time.Minute).UTC(),
			PriceMWh: 500, PriceKWh: 0.5, State: domain.PriceStateFinal,
		})
	}
	return prices.Day{
		Area: req.Area, Currency: req.Currency, Date: key,
		State: domain.PriceStateFinal, Points: points,
		FetchedAt: f.now().UTC(),
	}, nil
}

func (f *fakePriceFetcher) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

func newTestPoller(t *testing.T, clock *time.Time) (*PricePoller, *fakePriceFetcher) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "poller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &fakePriceFetcher{
		calls: make(map[string]int),
		fail:  make(map[string]error),
		now:   func() time.Time { return *clock },
	}
	svc := prices.New(st, cfg, fetcher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	poller := NewPricePoller(svc, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	poller.now = func() time.Time { return *clock }
	return poller, fetcher
}

func TestPricePollerSchedule(t *testing.T) {
	clock := time.Date(2026, 10, 29, 12, 0, 0, 0, testLoc)
	poller, fetcher := newTestPoller(t, &clock)
	ctx := context.Background()

	poller.tick(ctx)
	if got := fetcher.count("2026-10-29"); got != 1 {
		t.Fatalf("today calls = %d, want 1", got)
	}
	if got := fetcher.count("2026-10-30"); got != 0 {
		t.Fatalf("tomorrow calls before 13:02 = %d, want 0", got)
	}

	clock = time.Date(2026, 10, 29, 13, 5, 0, 0, testLoc)
	poller.tick(ctx)
	if got := fetcher.count("2026-10-30"); got != 1 {
		t.Fatalf("tomorrow calls = %d, want 1", got)
	}
	if got := fetcher.count("2026-10-29"); got != 1 {
		t.Fatalf("today calls after tomorrow fetch = %d, want 1", got)
	}

	clock = time.Date(2026, 10, 29, 18, 5, 0, 0, testLoc)
	poller.tick(ctx)
	if got := fetcher.count("2026-10-29"); got != 2 {
		t.Fatalf("today calls after 6h = %d, want 2", got)
	}
}

func TestPricePollerRetriesNotPublished(t *testing.T) {
	clock := time.Date(2026, 10, 29, 13, 5, 0, 0, testLoc)
	poller, fetcher := newTestPoller(t, &clock)
	ctx := context.Background()
	fetcher.fail["2026-10-30"] = prices.ErrNotPublished

	poller.tick(ctx)
	if got := fetcher.count("2026-10-30"); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	clock = clock.Add(5 * time.Minute)
	poller.tick(ctx)
	if got := fetcher.count("2026-10-30"); got != 1 {
		t.Fatalf("retried too early: %d", got)
	}
	clock = clock.Add(11 * time.Minute) // 16 minutes since the first attempt
	poller.tick(ctx)
	if got := fetcher.count("2026-10-30"); got != 2 {
		t.Fatalf("attempts after retry interval = %d, want 2", got)
	}
}

func TestPricePollerRetriesTomorrowLate(t *testing.T) {
	clock := time.Date(2026, 10, 29, 23, 30, 0, 0, testLoc)
	poller, fetcher := newTestPoller(t, &clock)
	poller.tick(context.Background())
	if got := fetcher.count("2026-10-30"); got != 1 {
		t.Fatalf("tomorrow attempts at 23:30 = %d, want 1", got)
	}
}
