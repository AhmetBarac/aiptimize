package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// HistoryBucket is one day or week of measured energy and cost.
type HistoryBucket struct {
	Label               string    `json:"label"`
	Start               time.Time `json:"start"`
	End                 time.Time `json:"end"`
	PVKWh               float64   `json:"pv_kwh"`
	LoadKWh             float64   `json:"load_kwh"`
	EVKWh               float64   `json:"ev_kwh"`
	GridImportKWh       float64   `json:"grid_import_kwh"`
	GridExportKWh       float64   `json:"grid_export_kwh"`
	BatteryChargeKWh    float64   `json:"battery_charge_kwh"`
	BatteryDischargeKWh float64   `json:"battery_discharge_kwh"`
	PlanBatteryKWh      float64   `json:"plan_battery_kwh"`
	Cost                float64   `json:"cost"`
	BaselineCost        float64   `json:"baseline_cost"`
	Savings             float64   `json:"savings"`
}

// HistorySnapshot is the aggregate response.
type HistorySnapshot struct {
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Resolution string          `json:"resolution"`
	Timezone   string          `json:"timezone"`
	Currency   string          `json:"currency"`
	Buckets    []HistoryBucket `json:"buckets"`
	Totals     HistoryBucket   `json:"totals"`
	Warnings   []string        `json:"warnings"`
}

// HistoryService aggregates telemetry and prices into daily/weekly history.
type HistoryService struct {
	st     *store.Store
	cfg    *settings.Service
	prices *prices.Service
	plans  *PlannerService
	now    func() time.Time
}

// NewHistoryService builds the history service.
func NewHistoryService(st *store.Store, cfg *settings.Service, priceSvc *prices.Service, planner *PlannerService) *HistoryService {
	return &HistoryService{st: st, cfg: cfg, prices: priceSvc, plans: planner, now: time.Now}
}

// Snapshot aggregates [from, to) at day or week resolution.
func (s *HistoryService) Snapshot(ctx context.Context, from, to time.Time, resolution string) (HistorySnapshot, error) {
	loc := s.cfg.Location()
	if resolution != "week" {
		resolution = "day"
	}
	if from.IsZero() {
		local := s.now().In(loc)
		from = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -13)
	}
	if to.IsZero() {
		local := s.now().In(loc)
		to = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	}
	from = from.In(loc)
	to = to.In(loc)
	if !to.After(from) {
		return HistorySnapshot{}, fmt.Errorf("invalid history range")
	}
	if to.Sub(from) > 92*24*time.Hour {
		return HistorySnapshot{}, fmt.Errorf("history range is limited to 92 days")
	}
	out := HistorySnapshot{From: from.UTC(), To: to.UTC(), Resolution: resolution,
		Timezone: s.cfg.Get().Site.Timezone, Currency: s.cfg.Get().Price.Currency,
		Warnings: []string{}}
	buckets := map[string]*HistoryBucket{}
	for start := truncateBucket(from, resolution); start.Before(to); start = nextBucket(start, resolution) {
		label := bucketLabel(start, resolution)
		buckets[label] = &HistoryBucket{Label: label, Start: start.UTC(), End: nextBucket(start, resolution).UTC()}
	}

	priceSnap, err := s.prices.Snapshot(ctx, from, to)
	if err != nil {
		out.Warnings = append(out.Warnings, "Price history unavailable: "+err.Error())
	}
	priceByStart := map[int64]domain.PriceBlock{}
	for _, block := range priceSnap.Blocks {
		priceByStart[block.Start.Unix()] = block
	}

	samples, err := s.st.TelemetryBetween(ctx, from, to, 100000)
	if err != nil {
		return HistorySnapshot{}, err
	}
	if len(samples) < 2 {
		out.Warnings = append(out.Warnings, "Not enough telemetry for this range.")
	}
	planSlots := map[int64][]domain.Slot{}
	if s.plans != nil {
		if plan, err := s.plans.Latest(ctx); err == nil {
			for _, slot := range plan.Slots {
				start := slot.Start.UTC().Truncate(30 * time.Minute).Unix()
				planSlots[start] = append(planSlots[start], slot)
			}
		}
	}
	for i := 1; i < len(samples); i++ {
		prev := samples[i-1]
		cur := samples[i]
		dt := cur.TS.Sub(prev.TS).Hours()
		if dt <= 0 {
			continue
		}
		if dt > 5.0/60 {
			dt = 5.0 / 60
		}
		local := prev.TS.In(loc)
		label := bucketLabel(truncateBucket(local, resolution), resolution)
		bucket := buckets[label]
		if bucket == nil {
			continue
		}
		pv := (prev.PVW + cur.PVW) / 2
		load := (prev.LoadW + cur.LoadW) / 2
		ev := (prev.EVW + cur.EVW) / 2
		grid := (prev.GridW + cur.GridW) / 2
		battery := (prev.BatteryW + cur.BatteryW) / 2
		bucket.PVKWh += pv * dt / 1000
		bucket.LoadKWh += load * dt / 1000
		bucket.EVKWh += ev * dt / 1000
		if grid > 0 {
			bucket.GridImportKWh += grid * dt / 1000
		} else {
			bucket.GridExportKWh += -grid * dt / 1000
		}
		if battery > 0 {
			bucket.BatteryChargeKWh += battery * dt / 1000
		} else {
			bucket.BatteryDischargeKWh += -battery * dt / 1000
		}
		blockStart := prev.TS.UTC().Truncate(30 * time.Minute).Unix()
		price := priceByStart[blockStart]
		actual := math.Max(grid, 0)*dt/1000*price.ImportPrice -
			math.Max(-grid, 0)*dt/1000*price.ExportPrice
		net := load + ev - pv
		baseline := math.Max(net, 0)*dt/1000*price.ImportPrice -
			math.Max(-net, 0)*dt/1000*price.ExportPrice
		bucket.Cost += actual
		bucket.BaselineCost += baseline
	}
	for start, slots := range planSlots {
		local := time.Unix(start, 0).In(loc)
		label := bucketLabel(truncateBucket(local, resolution), resolution)
		bucket := buckets[label]
		if bucket == nil {
			continue
		}
		for _, slot := range slots {
			hours := slot.End.Sub(slot.Start).Hours()
			energy := slot.PowerW * hours / 1000
			if slot.Direction == domain.DirectionCharge {
				bucket.PlanBatteryKWh += energy
			} else {
				bucket.PlanBatteryKWh -= energy
			}
		}
	}

	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		bucket := buckets[key]
		bucket.PVKWh = round2(bucket.PVKWh)
		bucket.LoadKWh = round2(bucket.LoadKWh)
		bucket.EVKWh = round2(bucket.EVKWh)
		bucket.GridImportKWh = round2(bucket.GridImportKWh)
		bucket.GridExportKWh = round2(bucket.GridExportKWh)
		bucket.BatteryChargeKWh = round2(bucket.BatteryChargeKWh)
		bucket.BatteryDischargeKWh = round2(bucket.BatteryDischargeKWh)
		bucket.PlanBatteryKWh = round2(bucket.PlanBatteryKWh)
		bucket.Cost = round2(bucket.Cost)
		bucket.BaselineCost = round2(bucket.BaselineCost)
		bucket.Savings = round2(bucket.BaselineCost - bucket.Cost)
		out.Buckets = append(out.Buckets, *bucket)
		out.Totals.PVKWh += bucket.PVKWh
		out.Totals.LoadKWh += bucket.LoadKWh
		out.Totals.EVKWh += bucket.EVKWh
		out.Totals.GridImportKWh += bucket.GridImportKWh
		out.Totals.GridExportKWh += bucket.GridExportKWh
		out.Totals.BatteryChargeKWh += bucket.BatteryChargeKWh
		out.Totals.BatteryDischargeKWh += bucket.BatteryDischargeKWh
		out.Totals.PlanBatteryKWh += bucket.PlanBatteryKWh
		out.Totals.Cost += bucket.Cost
		out.Totals.BaselineCost += bucket.BaselineCost
		out.Totals.Savings += bucket.Savings
	}
	out.Totals = roundBucket(out.Totals)
	return out, nil
}

func truncateBucket(t time.Time, resolution string) time.Time {
	if resolution == "week" {
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		offset := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func nextBucket(t time.Time, resolution string) time.Time {
	if resolution == "week" {
		return t.AddDate(0, 0, 7)
	}
	return t.AddDate(0, 0, 1)
}

func bucketLabel(t time.Time, resolution string) string {
	if resolution == "week" {
		year, week := t.ISOWeek()
		return fmt.Sprintf("%d-W%02d", year, week)
	}
	return t.Format("2006-01-02")
}

func roundBucket(b HistoryBucket) HistoryBucket {
	b.PVKWh = round2(b.PVKWh)
	b.LoadKWh = round2(b.LoadKWh)
	b.EVKWh = round2(b.EVKWh)
	b.GridImportKWh = round2(b.GridImportKWh)
	b.GridExportKWh = round2(b.GridExportKWh)
	b.BatteryChargeKWh = round2(b.BatteryChargeKWh)
	b.BatteryDischargeKWh = round2(b.BatteryDischargeKWh)
	b.PlanBatteryKWh = round2(b.PlanBatteryKWh)
	b.Cost = round2(b.Cost)
	b.BaselineCost = round2(b.BaselineCost)
	b.Savings = round2(b.Savings)
	return b
}
