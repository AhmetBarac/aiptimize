package prices

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

var stockholm = mustLocation("Europe/Stockholm")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func fixtureServer(t *testing.T, file string, status int) *httptest.Server {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "nordpool", file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", file, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"test"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientFetchDay(t *testing.T) {
	var gotQuery map[string]string
	path := filepath.Join("..", "..", "testdata", "nordpool", "se4-2026-10-29.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = map[string]string{
			"date":       r.URL.Query().Get("date"),
			"market":     r.URL.Query().Get("market"),
			"area":       r.URL.Query().Get("deliveryArea"),
			"currency":   r.URL.Query().Get("currency"),
			"user_agent": r.UserAgent(),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "aiptimize/test")
	c.MinInterval = 0
	day, err := c.FetchDay(context.Background(), Request{
		Area:     "SE4",
		Currency: "SEK",
		Date:     time.Date(2026, 10, 29, 0, 0, 0, 0, stockholm),
		Location: stockholm,
	})
	if err != nil {
		t.Fatalf("FetchDay: %v", err)
	}
	if len(day.Points) != 96 {
		t.Fatalf("points = %d, want 96", len(day.Points))
	}
	if day.State != domain.PriceStateFinal {
		t.Errorf("state = %s", day.State)
	}
	if day.Currency != "SEK" || day.Area != "SE4" {
		t.Errorf("area/currency = %s/%s", day.Area, day.Currency)
	}
	if got := day.Points[0].DeliveryStart.UTC(); !got.Equal(time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("first start = %s", got)
	}
	if got := day.Points[len(day.Points)-1].DeliveryStart.UTC(); !got.Equal(time.Date(2026, 10, 29, 22, 45, 0, 0, time.UTC)) {
		t.Errorf("last start = %s", got)
	}
	if math.Abs(day.Points[0].PriceKWh-0.1) > 1e-9 {
		t.Errorf("first price/kWh = %v", day.Points[0].PriceKWh)
	}
	want := map[string]string{"date": "2026-10-29", "market": "DayAhead", "area": "SE4", "currency": "SEK"}
	for k, v := range want {
		if gotQuery[k] != v {
			t.Errorf("query %s = %q, want %q", k, gotQuery[k], v)
		}
	}
	if gotQuery["user_agent"] != "aiptimize/test" {
		t.Errorf("user agent = %q", gotQuery["user_agent"])
	}
}

func TestClientErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"not published", http.StatusNoContent, ErrNotPublished},
		{"unauthorized", http.StatusUnauthorized, nil},
		{"bad request", http.StatusBadRequest, nil},
		{"server error", http.StatusInternalServerError, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fixtureServer(t, "se4-2026-10-29.json", tc.status)
			c := NewClient(srv.URL, "test")
			c.MinInterval = 0
			_, err := c.FetchDay(context.Background(), Request{
				Area: "SE4", Currency: "SEK",
				Date:     time.Date(2026, 10, 29, 0, 0, 0, 0, stockholm),
				Location: stockholm,
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var httpErr *HTTPError
			if tc.status != http.StatusNoContent && !errors.As(err, &httpErr) {
				t.Fatalf("expected HTTPError, got %T", err)
			}
		})
	}
}

func TestClientRejectsUnknownArea(t *testing.T) {
	srv := fixtureServer(t, "se4-2026-10-29.json", http.StatusOK)
	c := NewClient(srv.URL, "test")
	c.MinInterval = 0
	_, err := c.FetchDay(context.Background(), Request{
		Area: "NO1", Currency: "SEK",
		Date:     time.Date(2026, 10, 29, 0, 0, 0, 0, stockholm),
		Location: stockholm,
	})
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("err = %v, want ErrNoData", err)
	}
}

func TestClientTrimsToLocalDay(t *testing.T) {
	srv := fixtureServer(t, "se4-2026-10-29.json", http.StatusOK)
	c := NewClient(srv.URL, "test")
	c.MinInterval = 0
	// UTC midnight is one hour later than Stockholm midnight; the first four
	// points belong to the previous local day and must be dropped.
	day, err := c.FetchDay(context.Background(), Request{
		Area: "SE4", Currency: "SEK",
		Date:     time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
		Location: time.UTC,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Points) != 92 {
		t.Fatalf("points = %d, want 92 after trimming", len(day.Points))
	}
	if !day.Points[0].DeliveryStart.Equal(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("first = %s", day.Points[0].DeliveryStart)
	}
}

func TestAggregateTariffAndMissing(t *testing.T) {
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	points := []domain.PricePoint{
		{DeliveryStart: base, DeliveryEnd: base.Add(15 * time.Minute), PriceMWh: 1000, PriceKWh: 1, State: domain.PriceStateFinal},
		{DeliveryStart: base.Add(15 * time.Minute), DeliveryEnd: base.Add(30 * time.Minute), PriceMWh: 500, PriceKWh: 0.5, State: domain.PriceStateFinal},
		// gap at 00:30-01:00
		{DeliveryStart: base.Add(time.Hour), DeliveryEnd: base.Add(75 * time.Minute), PriceMWh: -200, PriceKWh: -0.2, State: domain.PriceStatePreliminary},
		{DeliveryStart: base.Add(75 * time.Minute), DeliveryEnd: base.Add(90 * time.Minute), PriceMWh: -100, PriceKWh: -0.1, State: domain.PriceStatePreliminary},
	}
	tariff := domain.Tariff{GridFee: 0.5, EnergyTax: 0.25, VAT: 25, ExportFee: 0.1}
	blocks := Aggregate(points, tariff, stockholm)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	if blocks[0].PriceMWh != 750 || blocks[0].PriceKWh != 0.75 {
		t.Errorf("first block price = %+v", blocks[0])
	}
	if blocks[0].ImportPrice != (0.75+0.75)*1.25 {
		t.Errorf("import price = %v", blocks[0].ImportPrice)
	}
	if blocks[0].ExportPrice != 0.65 {
		t.Errorf("export price = %v", blocks[0].ExportPrice)
	}
	if !blocks[1].Missing || blocks[1].State != domain.PriceStateMissing {
		t.Errorf("middle block should be missing: %+v", blocks[1])
	}
	if blocks[2].State != domain.PriceStatePreliminary {
		t.Errorf("preliminary state not propagated: %+v", blocks[2])
	}
}

func TestTariffFromManualSiteFees(t *testing.T) {
	v := settings.Default()
	v.Site.TariffMode = "manual"
	v.Site.TransferFeeOre = 25
	v.Site.EnergyTaxOre = 40
	v.Site.VariableCostOre = 10
	v.Site.FixedMarkupOre = 5
	v.Site.ExportBenefitOre = 6
	v.Site.ExportExtraOre = 4

	got := tariffFrom(v)
	if math.Abs(got.GridFee-(v.Price.GridFee+0.25)) > 1e-9 {
		t.Errorf("grid fee = %v", got.GridFee)
	}
	if math.Abs(got.EnergyTax-(v.Price.EnergyTax+0.40)) > 1e-9 {
		t.Errorf("energy tax = %v", got.EnergyTax)
	}
	if math.Abs(got.Markup-(v.Price.Markup+0.15)) > 1e-9 {
		t.Errorf("markup = %v", got.Markup)
	}
	if math.Abs(got.ExportFee-(v.Price.ExportFee-0.10)) > 1e-9 {
		t.Errorf("export fee = %v", got.ExportFee)
	}
}

func TestTariffFromIgnoresManualFeesInAPIMode(t *testing.T) {
	v := settings.Default()
	v.Site.TariffMode = "api"
	v.Site.TransferFeeOre = 25
	got := tariffFrom(v)
	if got.GridFee != v.Price.GridFee {
		t.Errorf("grid fee = %v, want %v", got.GridFee, v.Price.GridFee)
	}
}

func TestAggregateDST(t *testing.T) {
	t.Run("spring forward 23h", func(t *testing.T) {
		start := time.Date(2026, 3, 29, 0, 0, 0, 0, stockholm)
		end := start.AddDate(0, 0, 1)
		var points []domain.PricePoint
		for ts := start; ts.Before(end); ts = ts.Add(15 * time.Minute) {
			points = append(points, domain.PricePoint{
				DeliveryStart: ts.UTC(), DeliveryEnd: ts.Add(15 * time.Minute).UTC(),
				PriceMWh: 100, PriceKWh: 0.1, State: domain.PriceStateFinal,
			})
		}
		if len(points) != 92 {
			t.Fatalf("points = %d, want 92", len(points))
		}
		blocks := Aggregate(points, domain.Tariff{}, stockholm)
		if len(blocks) != 46 {
			t.Fatalf("blocks = %d, want 46", len(blocks))
		}
		for _, b := range blocks {
			if b.Missing {
				t.Fatalf("unexpected missing block at %s", b.Start)
			}
		}
	})

	t.Run("fall back 25h", func(t *testing.T) {
		srv := fixtureServer(t, "se4-2026-10-25.json", http.StatusOK)
		c := NewClient(srv.URL, "test")
		c.MinInterval = 0
		day, err := c.FetchDay(context.Background(), Request{
			Area: "SE4", Currency: "SEK",
			Date:     time.Date(2026, 10, 25, 0, 0, 0, 0, stockholm),
			Location: stockholm,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(day.Points) != 100 {
			t.Fatalf("points = %d, want 100", len(day.Points))
		}
		blocks := Aggregate(day.Points, domain.Tariff{}, stockholm)
		if len(blocks) != 50 {
			t.Fatalf("blocks = %d, want 50", len(blocks))
		}
		// The repeated local 02:00 hour must stay as two distinct UTC blocks.
		var repeats int
		for _, b := range blocks {
			if b.Start.In(stockholm).Hour() == 2 {
				repeats++
			}
		}
		if repeats != 4 {
			t.Errorf("local 02: blocks = %d, want 4", repeats)
		}
		for i := 1; i < len(blocks); i++ {
			if !blocks[i].Start.Equal(blocks[i-1].End) {
				t.Fatalf("blocks not contiguous at %d", i)
			}
		}
	})
}

func TestCheapestWindows(t *testing.T) {
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	var blocks []domain.PriceBlock
	pricesList := []float64{2, 1, 1, 3, 0.5, 0.5, 4, 4}
	for i, p := range pricesList {
		start := base.Add(time.Duration(i) * 30 * time.Minute)
		blocks = append(blocks, domain.PriceBlock{
			Start: start, End: start.Add(30 * time.Minute), ImportPrice: p, ExportPrice: p,
		})
	}
	windows := CheapestWindows(blocks, time.Hour, 2)
	if len(windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(windows))
	}
	// Chronological order: 00:30-01:30 then 02:00-03:00.
	if !windows[0].Start.Equal(base.Add(30*time.Minute)) || windows[0].AvgImportPrice != 1.0 {
		t.Errorf("first window = %+v", windows[0])
	}
	if !windows[1].Start.Equal(base.Add(2*time.Hour)) || windows[1].AvgImportPrice != 0.5 {
		t.Errorf("second window = %+v", windows[1])
	}
}

func TestSummarize(t *testing.T) {
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	blocks := []domain.PriceBlock{
		{Start: base, End: base.Add(30 * time.Minute), ImportPrice: 1},
		{Start: base.Add(30 * time.Minute), End: base.Add(time.Hour), ImportPrice: 3},
		{Start: base.Add(time.Hour), End: base.Add(90 * time.Minute), ImportPrice: 2},
	}
	s := Summarize(nil, blocks, 60, 1)
	if s.MinImport != 1 || s.MaxImport != 3 || s.AvgImport != 2 {
		t.Fatalf("summary = %+v", s)
	}
	if len(s.Cheapest) != 1 || s.Cheapest[0].AvgImportPrice != 2 {
		t.Fatalf("cheapest = %+v", s.Cheapest)
	}
}

func TestSimulator(t *testing.T) {
	sim := &SimulatorFetcher{Now: func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }}
	req := Request{Area: "SE4", Currency: "SEK", Date: time.Date(2026, 10, 29, 0, 0, 0, 0, stockholm), Location: stockholm}
	a, err := sim.FetchDay(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := sim.FetchDay(context.Background(), req)
	if len(a.Points) != 96 || len(b.Points) != 96 {
		t.Fatalf("simulator points = %d", len(a.Points))
	}
	if a.Points[10].PriceMWh != b.Points[10].PriceMWh {
		t.Error("simulator must be deterministic")
	}
	if a.Points[10].PriceMWh <= 0 {
		t.Errorf("unexpected price %v", a.Points[10].PriceMWh)
	}
}

func TestServiceStoresAndSnapshots(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "prices.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg, err := settings.New(ctx, st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Patch(ctx, settings.SectionPrice, []byte(`{"grid_fee":0.5,"energy_tax":0.25,"vat":25,"export_fee":0.1}`)); err != nil {
		t.Fatal(err)
	}

	fetcher := &fakeFetcher{priceMWh: 1000, now: time.Now}
	svc := New(st, cfg, fetcher, slog.New(slog.NewTextHandler(io.Discard, nil)))
	day := time.Date(2026, 10, 29, 0, 0, 0, 0, stockholm)
	if _, err := svc.Refresh(ctx, day); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	info, err := svc.DayInfo(ctx, day)
	if err != nil || info.Points != 96 || info.State != domain.PriceStateFinal {
		t.Fatalf("DayInfo = %+v, %v", info, err)
	}
	snap, err := svc.Snapshot(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Points) != 96 || len(snap.Blocks) != 48 {
		t.Fatalf("snapshot = %d points, %d blocks", len(snap.Points), len(snap.Blocks))
	}
	if snap.Blocks[0].ImportPrice != (1+0.75)*1.25 {
		t.Errorf("import price = %v", snap.Blocks[0].ImportPrice)
	}
	if snap.Summary.Points != 96 || snap.Summary.Blocks != 48 {
		t.Errorf("summary = %+v", snap.Summary)
	}
	if snap.State != domain.PriceStateFinal || snap.Attribution == "" || snap.Source == "" {
		t.Errorf("metadata = %+v", snap)
	}
	if snap.FetchedAt == nil {
		t.Error("fetched_at missing")
	}

	// A preliminary re-publication replaces the day and updates the state.
	fetcher.preliminary = true
	fetcher.priceMWh = 2000
	if _, err := svc.Refresh(ctx, day); err != nil {
		t.Fatal(err)
	}
	snap, err = svc.Snapshot(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Points) != 96 {
		t.Fatalf("points after refresh = %d", len(snap.Points))
	}
	if snap.State != domain.PriceStatePreliminary {
		t.Errorf("state = %s", snap.State)
	}
	if snap.Blocks[0].PriceMWh != 2000 {
		t.Errorf("price not replaced: %v", snap.Blocks[0].PriceMWh)
	}
}

// fakeFetcher generates a local-day price series for service tests.
type fakeFetcher struct {
	mu          sync.Mutex
	priceMWh    float64
	preliminary bool
	now         func() time.Time
}

func (f *fakeFetcher) FetchDay(_ context.Context, req Request) (Day, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	loc := req.Location
	if loc == nil {
		loc = time.UTC
	}
	start := time.Date(req.Date.In(loc).Year(), req.Date.In(loc).Month(), req.Date.In(loc).Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	state := domain.PriceStateFinal
	if f.preliminary {
		state = domain.PriceStatePreliminary
	}
	var points []domain.PricePoint
	for ts := start; ts.Before(end); ts = ts.Add(15 * time.Minute) {
		points = append(points, domain.PricePoint{
			DeliveryStart: ts.UTC(), DeliveryEnd: ts.Add(15 * time.Minute).UTC(),
			PriceMWh: f.priceMWh, PriceKWh: f.priceMWh / 1000, State: state,
		})
	}
	now := time.Now
	if f.now != nil {
		now = f.now
	}
	return Day{
		Area: req.Area, Currency: req.Currency,
		Date: start.Format("2006-01-02"), State: state,
		Points: points, FetchedAt: now().UTC(),
	}, nil
}
