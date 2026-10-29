package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
	"github.com/AhmetBarac/aiptimize/internal/planner"
	"github.com/AhmetBarac/aiptimize/internal/prices"
	"github.com/AhmetBarac/aiptimize/internal/settings"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// Planner errors are mapped to API codes.
var (
	ErrSOCUnavailable    = errors.New("planner: no fresh battery SOC")
	ErrPricesUnavailable = errors.New("planner: no price blocks")
	ErrPlanUnavailable   = errors.New("planner: no plan yet")
)

// PlannerService builds and stores the automatic battery suggestion.
type PlannerService struct {
	st        *store.Store
	cfg       *settings.Service
	prices    *prices.Service
	forecast  *ForecastService
	telemetry *TelemetryPoller
	log       *slog.Logger
	now       func() time.Time
	publish   func(string, any)
}

// NewPlannerService builds the planner.
func NewPlannerService(st *store.Store, cfg *settings.Service, priceSvc *prices.Service,
	forecastSvc *ForecastService, telemetry *TelemetryPoller, log *slog.Logger,
	publish func(string, any)) *PlannerService {
	if log == nil {
		log = slog.Default()
	}
	return &PlannerService{st: st, cfg: cfg, prices: priceSvc, forecast: forecastSvc,
		telemetry: telemetry, log: log, now: time.Now, publish: publish}
}

// Latest returns the newest stored plan.
func (s *PlannerService) Latest(ctx context.Context) (domain.Plan, error) {
	plan, err := s.st.LatestPlan(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Plan{}, ErrPlanUnavailable
	}
	return plan, err
}

// BuildInputs assembles the current planner inputs from prices, forecast,
// telemetry and settings. It is shared by the optimiser and the draft preview.
func (s *PlannerService) BuildInputs(ctx context.Context) (planner.Inputs, []string, error) {
	values := s.cfg.Get()
	loc := s.cfg.Location()
	now := s.now().In(loc)
	from := now.UTC().Truncate(30 * time.Minute)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	days := values.Plan.HorizonDays
	if days < 1 {
		days = 1
	}
	if days > 2 {
		days = 2
	}
	horizonEnd := dayStart.AddDate(0, 0, days)

	sample, receivedAt, ok := s.telemetry.Latest()
	if !ok {
		return planner.Inputs{}, nil, ErrSOCUnavailable
	}
	if s.now().Sub(receivedAt) > 5*time.Minute {
		return planner.Inputs{}, nil, ErrSOCUnavailable
	}

	priceSnap, err := s.prices.Snapshot(ctx, from, horizonEnd)
	if err != nil {
		return planner.Inputs{}, nil, err
	}
	byStart := make(map[int64]domain.PriceBlock, len(priceSnap.Blocks))
	for _, block := range priceSnap.Blocks {
		byStart[block.Start.Unix()] = block
	}

	warnings := []string{}
	blocks := make([]planner.Block, 0, 96)
	for ts := from; ts.Before(horizonEnd); ts = ts.Add(30 * time.Minute) {
		price, found := byStart[ts.Unix()]
		if !found || price.Missing {
			warnings = append(warnings, fmt.Sprintf("Prices are missing from %s; the plan stops there.",
				ts.In(loc).Format("15:04")))
			break
		}
		blocks = append(blocks, planner.Block{
			Start: ts, End: ts.Add(30 * time.Minute),
			ImportPrice: price.ImportPrice, ExportPrice: price.ExportPrice,
		})
	}
	if len(blocks) == 0 {
		return planner.Inputs{}, nil, ErrPricesUnavailable
	}
	if len(blocks) < int(horizonEnd.Sub(from)/(30*time.Minute)) {
		warnings = append(warnings, "The price horizon is shorter than the configured plan horizon.")
	}

	if s.forecast != nil {
		if snap, ok := s.forecast.Latest(); ok {
			if snap.Stale {
				warnings = append(warnings, "The PV/load forecast is stale.")
			}
			oldPV := s.now().Sub(snap.GeneratedAt) > 3*time.Hour
			if oldPV {
				warnings = append(warnings, "The PV forecast is older than 3 hours; planning with PV = 0.")
			}
			pvByStart := make(map[int64]float64, len(snap.Points))
			loadByStart := make(map[int64]float64, len(snap.Points))
			for _, point := range snap.Points {
				pvByStart[point.Start.Unix()] = point.PVW
				loadByStart[point.Start.Unix()] = point.LoadW
			}
			for i := range blocks {
				if !oldPV {
					blocks[i].PVW = pvByStart[blocks[i].Start.Unix()]
				}
				blocks[i].LoadW = loadByStart[blocks[i].Start.Unix()]
			}
		} else {
			warnings = append(warnings, "No forecast is available; planning with PV = 0.")
		}
	}

	inv := values.Inverter
	planCfg := values.Plan
	chargeW := planCfg.ChargeW
	if chargeW <= 0 {
		chargeW = inv.MaxChargeW
	}
	dischargeW := planCfg.DischargeW
	if dischargeW <= 0 {
		dischargeW = inv.MaxDischargeW
	}
	maxSlotBlocks := planCfg.MaxSlotMin / 30
	if maxSlotBlocks < 1 {
		maxSlotBlocks = 1
	}
	if maxSlotBlocks > 8 {
		maxSlotBlocks = 8
	}
	return planner.Inputs{
		Timezone: values.Site.Timezone, HorizonStart: blocks[0].Start, HorizonEnd: blocks[len(blocks)-1].End,
		Now: now.UTC(), StartSOC: sample.SOC, CapacityKWh: inv.CapacityKWh,
		MinSOC: inv.MinSOC, MaxSOC: inv.MaxSOC, ReserveSOC: math.Max(inv.MinSOC, inv.ReserveSOC),
		MaxChargeW: inv.MaxChargeW, MaxDischargeW: inv.MaxDischargeW,
		ChargeW: chargeW, DischargeW: dischargeW,
		ChargeEfficiency: inv.ChargeEfficiency, DischargeEfficiency: inv.DischargeEfficiency,
		CycleCost: inv.CycleCost, SiteImportW: values.Site.MaxImportPowerW,
		MaxSlotsPerDay: planCfg.MaxSlotsPerDay, MaxSlotBlocks: maxSlotBlocks,
		PreserveSOC: planCfg.PreserveSOC, EndSOCTarget: planCfg.EndSOCTarget,
		MinDailySaving: planCfg.MinDailySaving, PVConfidence: values.PV.Confidence,
		AllowDischargeToGrid: planCfg.AllowDischargeToGrid,
		Currency:             values.Price.Currency, Blocks: blocks,
	}, warnings, nil
}

// Compute builds, stores and returns a fresh suggestion.
func (s *PlannerService) Compute(ctx context.Context) (domain.Plan, error) {
	values := s.cfg.Get()
	inputs, warnings, err := s.BuildInputs(ctx)
	if err != nil {
		return domain.Plan{}, err
	}
	result, err := planner.Optimize(inputs)
	if err != nil {
		return domain.Plan{}, err
	}
	rawInputs, err := json.Marshal(inputs)
	if err != nil {
		return domain.Plan{}, err
	}
	hash := sha256.Sum256(rawInputs)
	plan := domain.Plan{
		CreatedAt: s.now().UTC(), HorizonStart: inputs.HorizonStart.UTC(), HorizonEnd: inputs.HorizonEnd.UTC(),
		Timezone: values.Site.Timezone, Status: domain.PlanStatusSuggestion, Origin: domain.OriginAuto,
		Revision: 1, InputsHash: hex.EncodeToString(hash[:8]), Summary: result.Summary,
		Slots: result.Slots, Curve: result.Curve, Warnings: append(warnings, result.Warnings...),
		NoPlanReason: result.NoPlanReason, Inputs: rawInputs,
	}
	if plan.Slots == nil {
		plan.Slots = []domain.Slot{}
	}
	stored, err := s.st.SavePlan(ctx, plan)
	if err != nil {
		return domain.Plan{}, err
	}
	s.log.Info("plan computed", "id", stored.ID, "slots", len(stored.Slots),
		"savings", stored.Summary.Savings, "currency", stored.Summary.Currency)
	if s.publish != nil {
		s.publish("plan", map[string]any{
			"id": stored.ID, "horizon_start": stored.HorizonStart,
			"horizon_end": stored.HorizonEnd, "summary": stored.Summary,
			"slots": stored.Slots, "no_plan_reason": stored.NoPlanReason,
		})
	}
	return stored, nil
}

// Run computes a suggestion at startup and periodically thereafter. Errors keep
// the previous plan; no hardware is ever written.
func (s *PlannerService) Run(ctx context.Context) {
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if _, err := s.Compute(ctx); err != nil {
				s.log.Warn("automatic plan failed; keeping previous", "err", err)
			}
			timer.Reset(5 * time.Minute)
		}
	}
}
