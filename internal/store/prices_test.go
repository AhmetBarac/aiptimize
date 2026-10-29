package store

import (
	"context"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

func TestReplaceAndQueryPrices(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)

	point := func(offset time.Duration, price float64, state domain.PriceState) domain.PricePoint {
		return domain.PricePoint{
			DeliveryStart: base.Add(offset),
			DeliveryEnd:   base.Add(offset + 15*time.Minute),
			PriceMWh:      price,
			PriceKWh:      price / 1000,
			State:         state,
		}
	}
	points := []domain.PricePoint{
		point(0, 100, domain.PriceStateFinal),
		point(15*time.Minute, 200, domain.PriceStateFinal),
		point(30*time.Minute, 300, domain.PriceStatePreliminary),
	}
	fetched := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	to := base.Add(24 * time.Hour)
	if err := st.ReplacePrices(ctx, "SE4", "SEK", base, to, points, fetched); err != nil {
		t.Fatalf("ReplacePrices: %v", err)
	}

	got, err := st.PricesBetween(ctx, "SE4", "SEK", base, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("points = %d, want 3", len(got))
	}
	if got[2].State != domain.PriceStatePreliminary || got[2].PriceMWh != 300 {
		t.Fatalf("third point = %+v", got[2])
	}
	if !got[0].DeliveryStart.Equal(base) || got[0].PriceKWh != 0.1 {
		t.Fatalf("first point = %+v", got[0])
	}

	// Replacing the day must remove stale points, not merge.
	replacement := []domain.PricePoint{point(0, 999, domain.PriceStateFinal)}
	if err := st.ReplacePrices(ctx, "SE4", "SEK", base, to, replacement, fetched); err != nil {
		t.Fatal(err)
	}
	got, _ = st.PricesBetween(ctx, "SE4", "SEK", base, to)
	if len(got) != 1 || got[0].PriceMWh != 999 {
		t.Fatalf("replacement = %+v", got)
	}

	stats, err := st.PriceStatsFor(ctx, "SE4", "SEK", base, to)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Points != 1 || !stats.FetchedAt.Equal(fetched) || stats.Preliminary != 0 {
		t.Fatalf("stats = %+v", stats)
	}

	latest, err := st.LatestPriceFetch(ctx, "SE4", "SEK")
	if err != nil || !latest.Equal(fetched) {
		t.Fatalf("latest = %v, %v", latest, err)
	}

	empty, err := st.PricesBetween(ctx, "NO1", "NOK", base, to)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty query = %v, %v", empty, err)
	}
}

func TestPriceStatsEmpty(t *testing.T) {
	st := openTest(t)
	stats, err := st.PriceStatsFor(context.Background(), "SE4", "SEK",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Points != 0 || !stats.FetchedAt.IsZero() {
		t.Fatalf("stats = %+v", stats)
	}
	if _, err := st.LatestPriceFetch(context.Background(), "SE4", "SEK"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
