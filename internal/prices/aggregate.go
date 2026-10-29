package prices

import (
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// Aggregate turns 15-minute points into 30-minute blocks and applies the
// tariff. Block boundaries are derived from UTC instants, so DST days produce
// 46/48/50 blocks without merging the repeated autumn hour. For the supported
// market zones (whole-hour UTC offsets) these instants coincide with local
// :00/:30 boundaries.
func Aggregate(points []domain.PricePoint, tariff domain.Tariff, loc *time.Location) []domain.PriceBlock {
	if len(points) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}

	type accumulator struct {
		points []domain.PricePoint
	}
	grouped := make(map[time.Time]*accumulator, len(points)/2)
	var minStart, maxStart time.Time
	for _, p := range points {
		start := p.DeliveryStart.UTC().Truncate(30 * time.Minute)
		acc := grouped[start]
		if acc == nil {
			acc = &accumulator{}
			grouped[start] = acc
		}
		acc.points = append(acc.points, p)
		if minStart.IsZero() || start.Before(minStart) {
			minStart = start
		}
		if maxStart.IsZero() || start.After(maxStart) {
			maxStart = start
		}
	}

	blocks := make([]domain.PriceBlock, 0, len(grouped))
	for start := minStart; !start.After(maxStart); start = start.Add(30 * time.Minute) {
		acc := grouped[start]
		if acc == nil {
			blocks = append(blocks, domain.PriceBlock{
				Start:   start,
				End:     start.Add(30 * time.Minute),
				State:   domain.PriceStateMissing,
				Missing: true,
				Points:  0,
			})
			continue
		}
		var sum float64
		state := domain.PriceStateFinal
		for _, p := range acc.points {
			sum += p.PriceMWh
			if p.State == domain.PriceStatePreliminary {
				state = domain.PriceStatePreliminary
			}
			if p.State == domain.PriceStateMissing {
				state = domain.PriceStateMissing
			}
		}
		mean := sum / float64(len(acc.points))
		block := domain.PriceBlock{
			Start:       start,
			End:         start.Add(30 * time.Minute),
			PriceMWh:    mean,
			PriceKWh:    mean / 1000,
			ImportPrice: tariff.ImportPrice(mean / 1000),
			ExportPrice: tariff.ExportPrice(mean / 1000),
			State:       state,
			Points:      len(acc.points),
			Missing:     len(acc.points) < 2,
		}
		if block.Missing && len(acc.points) == 0 {
			block.State = domain.PriceStateMissing
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// LocalStart returns the block start in the site timezone.
func LocalStart(b domain.PriceBlock, loc *time.Location) time.Time {
	return b.Start.In(loc)
}

// LocalDay returns the local calendar date of an instant.
func LocalDay(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// Window is a contiguous candidate charging window.
type Window struct {
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	AvgImportPrice float64   `json:"avg_import_price"`
	AvgExportPrice float64   `json:"avg_export_price"`
	Blocks         int       `json:"blocks"`
}

// Summary aggregates a price range.
type Summary struct {
	MinImport float64  `json:"min_import"`
	MaxImport float64  `json:"max_import"`
	AvgImport float64  `json:"avg_import"`
	Points    int      `json:"points"`
	Blocks    int      `json:"blocks"`
	Cheapest  []Window `json:"cheapest_windows"`
}

// Summarize computes statistics and the cheapest contiguous windows.
func Summarize(points []domain.PricePoint, blocks []domain.PriceBlock, windowMinutes, count int) Summary {
	sum := Summary{Points: len(points), Blocks: len(blocks)}
	first := true
	var total float64
	var valid int
	for _, b := range blocks {
		if b.Missing {
			continue
		}
		if first {
			sum.MinImport = b.ImportPrice
			sum.MaxImport = b.ImportPrice
			first = false
		} else {
			if b.ImportPrice < sum.MinImport {
				sum.MinImport = b.ImportPrice
			}
			if b.ImportPrice > sum.MaxImport {
				sum.MaxImport = b.ImportPrice
			}
		}
		total += b.ImportPrice
		valid++
	}
	if valid > 0 {
		sum.AvgImport = total / float64(valid)
	}
	sum.Cheapest = CheapestWindows(blocks, time.Duration(windowMinutes)*time.Minute, count)
	return sum
}

// CheapestWindows returns up to count non-overlapping contiguous windows of the
// requested duration, cheapest first by average import price.
func CheapestWindows(blocks []domain.PriceBlock, window time.Duration, count int) []Window {
	span := int(window / (30 * time.Minute))
	if span <= 0 || count <= 0 || len(blocks) < span {
		return nil
	}
	// Split into contiguous runs of complete blocks.
	runs := make([][]domain.PriceBlock, 0, 1)
	current := make([]domain.PriceBlock, 0, len(blocks))
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, current)
			current = make([]domain.PriceBlock, 0, len(blocks))
		}
	}
	for _, b := range blocks {
		if b.Missing {
			flush()
			continue
		}
		if len(current) > 0 && !current[len(current)-1].End.Equal(b.Start) {
			flush()
		}
		current = append(current, b)
	}
	flush()

	var candidates []Window
	for _, run := range runs {
		if len(run) < span {
			continue
		}
		for i := 0; i+span <= len(run); i++ {
			var importSum, exportSum float64
			for _, b := range run[i : i+span] {
				importSum += b.ImportPrice
				exportSum += b.ExportPrice
			}
			candidates = append(candidates, Window{
				Start:          run[i].Start,
				End:            run[i+span-1].End,
				AvgImportPrice: importSum / float64(span),
				AvgExportPrice: exportSum / float64(span),
				Blocks:         span,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].AvgImportPrice != candidates[j].AvgImportPrice {
			return candidates[i].AvgImportPrice < candidates[j].AvgImportPrice
		}
		return candidates[i].Start.After(candidates[j].Start)
	})

	picked := make([]Window, 0, count)
	for _, c := range candidates {
		if len(picked) == count {
			break
		}
		overlaps := false
		for _, p := range picked {
			if c.Start.Before(p.End) && p.Start.Before(c.End) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			picked = append(picked, c)
		}
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].Start.Before(picked[j].Start) })
	return picked
}
