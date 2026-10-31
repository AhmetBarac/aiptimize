package prices

import (
	"context"
	"log/slog"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Service stores fetched prices and serves aggregated snapshots.
type Service struct {
	st      *store.Store
	cfg     *settings.Service
	fetcher Fetcher
	log     *slog.Logger
	now     func() time.Time
}

// New builds a price service.
func New(st *store.Store, cfg *settings.Service, fetcher Fetcher, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{st: st, cfg: cfg, fetcher: fetcher, log: log, now: time.Now}
}

// Snapshot is the API representation of a price range.
type Snapshot struct {
	Area        string              `json:"area"`
	Currency    string              `json:"currency"`
	Timezone    string              `json:"timezone"`
	From        time.Time           `json:"from"`
	To          time.Time           `json:"to"`
	Points      []domain.PricePoint `json:"points"`
	Blocks      []domain.PriceBlock `json:"blocks"`
	Tariff      domain.Tariff       `json:"tariff"`
	Summary     Summary             `json:"summary"`
	State       domain.PriceState   `json:"state"`
	FetchedAt   *time.Time          `json:"fetched_at,omitempty"`
	Source      string              `json:"source"`
	Attribution string              `json:"attribution"`
	Stale       bool                `json:"stale"`
}

const (
	// SourceName is shown in the UI.
	SourceName = "Nord Pool Data Portal"
	// Attribution is the required credit line.
	Attribution = "Spot prices: Nord Pool Data Portal"
)

// DayInfo describes what is stored for one local day.
type DayInfo struct {
	Points    int
	FetchedAt time.Time
	State     domain.PriceState
}

// Refresh fetches and replaces one local delivery day.
func (s *Service) Refresh(ctx context.Context, day time.Time) (Day, error) {
	loc := s.cfg.Location()
	p := s.cfg.Get().Price
	d, err := s.fetcher.FetchDay(ctx, Request{
		Area:     p.Area,
		Currency: p.Currency,
		Date:     day.In(loc),
		Location: loc,
	})
	if err != nil {
		return Day{}, err
	}
	start := localMidnight(day, loc)
	end := start.AddDate(0, 0, 1)
	if err := s.st.ReplacePrices(ctx, p.Area, p.Currency, start, end, d.Points, d.FetchedAt); err != nil {
		return Day{}, err
	}
	s.log.Info("prices stored",
		"area", p.Area, "currency", p.Currency, "date", d.Date,
		"points", len(d.Points), "state", d.State)
	return d, nil
}

// DayInfo returns the stored coverage of one local day.
func (s *Service) DayInfo(ctx context.Context, day time.Time) (DayInfo, error) {
	loc := s.cfg.Location()
	p := s.cfg.Get().Price
	start := localMidnight(day, loc)
	stats, err := s.st.PriceStatsFor(ctx, p.Area, p.Currency, start, start.AddDate(0, 0, 1))
	if err != nil {
		return DayInfo{}, err
	}
	state := domain.PriceStateMissing
	if stats.Points > 0 {
		state = domain.PriceStateFinal
		if stats.Preliminary > 0 {
			state = domain.PriceStatePreliminary
		}
	}
	return DayInfo{Points: stats.Points, FetchedAt: stats.FetchedAt, State: state}, nil
}

// Snapshot returns stored points and derived blocks for [from, to).
func (s *Service) Snapshot(ctx context.Context, from, to time.Time) (Snapshot, error) {
	loc := s.cfg.Location()
	v := s.cfg.Get()
	p := v.Price
	from = from.UTC()
	to = to.UTC()
	points, err := s.st.PricesBetween(ctx, p.Area, p.Currency, from, to)
	if err != nil {
		return Snapshot{}, err
	}
	tariff := tariffFrom(v)
	blocks := Aggregate(points, tariff, loc)
	stats, err := s.st.PriceStatsFor(ctx, p.Area, p.Currency, from, to)
	if err != nil {
		return Snapshot{}, err
	}
	state := domain.PriceStateMissing
	if len(points) > 0 {
		state = domain.PriceStateFinal
		if stats.Preliminary > 0 {
			state = domain.PriceStatePreliminary
		}
	}
	snap := Snapshot{
		Area:        p.Area,
		Currency:    p.Currency,
		Timezone:    s.cfg.Get().Site.Timezone,
		From:        from,
		To:          to,
		Points:      points,
		Blocks:      blocks,
		Tariff:      tariff,
		Summary:     Summarize(points, blocks, 120, 3),
		State:       state,
		Source:      SourceName,
		Attribution: Attribution,
	}
	if !stats.FetchedAt.IsZero() {
		fetched := stats.FetchedAt
		snap.FetchedAt = &fetched
		snap.Stale = s.now().Sub(fetched) > 24*time.Hour
	}
	if snap.Points == nil {
		snap.Points = []domain.PricePoint{}
	}
	if snap.Blocks == nil {
		snap.Blocks = []domain.PriceBlock{}
	}
	return snap, nil
}

// Tariff returns the configured tariff.
func (s *Service) Tariff() domain.Tariff {
	return tariffFrom(s.cfg.Get())
}

func tariffFrom(v settings.Values) domain.Tariff {
	p := v.Price
	t := domain.Tariff{
		GridFee:   p.GridFee,
		EnergyTax: p.EnergyTax,
		Markup:    p.Markup,
		VAT:       p.VAT,
		ExportFee: p.ExportFee,
	}
	if v.Site.TariffMode == "manual" {
		t.GridFee += v.Site.TransferFeeOre / 100
		t.EnergyTax += v.Site.EnergyTaxOre / 100
		t.Markup += (v.Site.VariableCostOre + v.Site.FixedMarkupOre) / 100
		t.ExportFee -= (v.Site.ExportBenefitOre + v.Site.ExportExtraOre) / 100
	}
	return t
}

func localMidnight(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}
