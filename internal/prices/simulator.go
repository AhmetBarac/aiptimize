package prices

import (
	"context"
	"math"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// SimulatorFetcher produces a deterministic, plausible price day without any
// network access. It is used with AIPTIMIZE_SIMULATOR=true for demos and
// end-to-end tests.
type SimulatorFetcher struct {
	Now func() time.Time
}

// FetchDay implements Fetcher.
func (s *SimulatorFetcher) FetchDay(_ context.Context, req Request) (Day, error) {
	loc := req.Location
	if loc == nil {
		loc = time.UTC
	}
	dayStart := localMidnight(req.Date, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	fetchedAt := now().UTC()

	points := make([]domain.PricePoint, 0, 100)
	seed := float64(dayStart.YearDay()%7) * 12
	for t := dayStart; t.Before(dayEnd); t = t.Add(15 * time.Minute) {
		local := t.In(loc)
		hour := float64(local.Hour()) + float64(local.Minute())/60
		base := 420 +
			260*math.Sin(2*math.Pi*(hour-7)/24) +
			140*math.Sin(4*math.Pi*(hour-17)/24) +
			seed
		price := math.Round(base*100) / 100
		points = append(points, domain.PricePoint{
			DeliveryStart: t.UTC(),
			DeliveryEnd:   t.Add(15 * time.Minute).UTC(),
			PriceMWh:      price,
			PriceKWh:      price / 1000,
			State:         domain.PriceStateFinal,
		})
	}
	return Day{
		Area:      req.Area,
		Currency:  req.Currency,
		Date:      dayStart.Format("2006-01-02"),
		State:     domain.PriceStateFinal,
		Points:    points,
		FetchedAt: fetchedAt,
		UpdatedAt: fetchedAt,
		Version:   1,
	}, nil
}
