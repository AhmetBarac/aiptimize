// Package planner implements the deterministic battery slot optimiser.
package planner

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// Inputs are the exact planner inputs. The service persists them with each plan.
type Inputs struct {
	Timezone             string    `json:"timezone"`
	HorizonStart         time.Time `json:"horizon_start"`
	HorizonEnd           time.Time `json:"horizon_end"`
	Now                  time.Time `json:"now"`
	StartSOC             float64   `json:"start_soc"`
	CapacityKWh          float64   `json:"capacity_kwh"`
	MinSOC               float64   `json:"min_soc"`
	MaxSOC               float64   `json:"max_soc"`
	ReserveSOC           float64   `json:"reserve_soc"`
	MaxChargeW           float64   `json:"max_charge_w"`
	MaxDischargeW        float64   `json:"max_discharge_w"`
	ChargeW              float64   `json:"charge_w"`
	DischargeW           float64   `json:"discharge_w"`
	ChargeEfficiency     float64   `json:"charge_efficiency"`
	DischargeEfficiency  float64   `json:"discharge_efficiency"`
	CycleCost            float64   `json:"cycle_cost"`
	SiteImportW          float64   `json:"site_import_w"`
	MaxSlotsPerDay       int       `json:"max_slots_per_day"`
	MaxSlotBlocks        int       `json:"max_slot_blocks"`
	PreserveSOC          bool      `json:"preserve_soc"`
	EndSOCTarget         float64   `json:"end_soc_target"`
	MinDailySaving       float64   `json:"min_daily_saving"`
	PVConfidence         float64   `json:"pv_confidence"`
	AllowDischargeToGrid bool      `json:"allow_discharge_to_grid"`
	Currency             string    `json:"currency"`
	EVWindows            []bool    `json:"ev_windows,omitempty"`
	Blocks               []Block   `json:"blocks"`
}

// Block is one 30-minute planning block.
type Block struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	ImportPrice float64   `json:"import_price"`
	ExportPrice float64   `json:"export_price"`
	PVW         float64   `json:"pv_w"`
	LoadW       float64   `json:"load_w"`
	EVW         float64   `json:"ev_w"`
	Missing     bool      `json:"missing"`
}

// CurvePoint is one simulated block of the plan.
type CurvePoint = domain.PlanCurvePoint

// Result is the optimiser output.
type Result struct {
	Slots        []domain.Slot      `json:"slots"`
	Curve        []CurvePoint       `json:"curve"`
	Summary      domain.PlanSummary `json:"summary"`
	Warnings     []string           `json:"warnings"`
	NoPlanReason string             `json:"no_plan_reason,omitempty"`
}

// ErrInfeasible means no schedule satisfies the terminal SOC policy.
var ErrInfeasible = errors.New("planner: no feasible schedule")

const socStepPct = 0.5

type stateKey struct {
	i    int
	soc  int
	used int
}

type action struct {
	kind int8 // 0 idle, 1 charge, 2 discharge
	dur  int8
}

type optimizer struct {
	in    Inputs
	n     int
	steps int
	used  int
	loc   *time.Location
	pv    []float64
	day   []int
	// memo indexed [i][soc][used]
	cost   []float64
	choice []action
	seen   []bool
}

// Optimize computes the minimum-cost schedule.
func Optimize(in Inputs) (Result, error) {
	if err := validate(in); err != nil {
		return Result{}, err
	}
	if in.PVConfidence <= 0 || in.PVConfidence > 1 {
		in.PVConfidence = 0.7
	}
	o := newOptimizer(in)

	startSOC := math.Max(in.MinSOC, math.Min(in.MaxSOC, in.StartSOC))
	startIdx := o.socIndex(startSOC)
	best, err := o.solve(0, startIdx, 0)
	if err != nil {
		return Result{}, err
	}
	if math.IsInf(best, 1) {
		return Result{}, ErrInfeasible
	}
	result := o.reconstruct(startIdx)
	if result.Summary.Savings < in.MinDailySaving {
		return emptyPlan(in), nil
	}
	return result, nil
}

func newOptimizer(in Inputs) *optimizer {
	o := &optimizer{
		in:    in,
		n:     len(in.Blocks),
		steps: int(math.Round((in.MaxSOC-in.MinSOC)/socStepPct)) + 1,
		used:  in.MaxSlotsPerDay + 1,
		loc:   mustLoc(in.Timezone),
		pv:    make([]float64, len(in.Blocks)),
		day:   make([]int, len(in.Blocks)),
	}
	for i, b := range in.Blocks {
		o.pv[i] = math.Max(0, b.PVW*in.PVConfidence)
		local := b.Start.In(o.loc)
		o.day[i] = local.Year()*1000 + local.YearDay()
	}
	o.cost = make([]float64, (o.n+1)*o.steps*o.used)
	o.choice = make([]action, len(o.cost))
	o.seen = make([]bool, len(o.cost))
	for i := range o.cost {
		o.cost[i] = math.Inf(1)
	}
	return o
}

func validate(in Inputs) error {
	if len(in.Blocks) == 0 {
		return errors.New("planner: no blocks")
	}
	if in.CapacityKWh <= 0 {
		return errors.New("planner: capacity must be positive")
	}
	if in.MaxSOC <= in.MinSOC {
		return errors.New("planner: invalid SOC range")
	}
	if in.MaxSlotsPerDay < 1 || in.MaxSlotsPerDay > 6 {
		return errors.New("planner: invalid slot budget")
	}
	if in.MaxSlotBlocks < 1 || in.MaxSlotBlocks > 8 {
		return errors.New("planner: invalid max slot length")
	}
	for i := 1; i < len(in.Blocks); i++ {
		if !in.Blocks[i].Start.Equal(in.Blocks[i-1].End) {
			return fmt.Errorf("planner: non-contiguous blocks at %d", i)
		}
	}
	return nil
}

func (o *optimizer) socIndex(soc float64) int {
	idx := int(math.Round((soc - o.in.MinSOC) / socStepPct))
	if idx < 0 {
		idx = 0
	}
	if idx >= o.steps {
		idx = o.steps - 1
	}
	return idx
}

func (o *optimizer) socValue(idx int) float64 {
	return o.in.MinSOC + float64(idx)*socStepPct
}

func (o *optimizer) index(i, soc, used int) int {
	return (i*o.steps+soc)*o.used + used
}

func (o *optimizer) solve(i, soc, used int) (float64, error) {
	if i >= o.n {
		if o.in.PreserveSOC {
			target := math.Min(o.in.EndSOCTarget, o.in.StartSOC)
			if o.socValue(soc)+1e-9 < target {
				return math.Inf(1), nil
			}
		}
		return 0, nil
	}
	if i > 0 && o.day[i] != o.day[i-1] {
		used = 0
	}
	idx := o.index(i, soc, used)
	if o.seen[idx] {
		return o.cost[idx], nil
	}
	o.seen[idx] = true
	best := math.Inf(1)
	var bestAction action

	idle := o.blockCost(i, 0)
	if idle < math.Inf(1) {
		value := idle + o.mustSolve(i+1, soc, used)
		best = value
		bestAction = action{kind: 0}
	}

	if used < o.in.MaxSlotsPerDay {
		maxD := o.maxDuration(i)
		power := math.Min(o.in.ChargeW, o.in.MaxChargeW)
		for d := 1; d <= maxD; d++ {
			if clamp := o.chargePower(i, d, power); clamp < power {
				power = clamp
			}
			if power < 1 {
				break
			}
			value, newSOC, ok := o.simulate(i, d, soc, power)
			if !ok {
				continue
			}
			value += 1e-6 + o.mustSolve(i+d, newSOC, used+1)
			if value < best-1e-12 {
				best = value
				bestAction = action{kind: 1, dur: int8(d)}
			}
		}
		power = math.Min(o.in.DischargeW, o.in.MaxDischargeW)
		for d := 1; d <= maxD; d++ {
			if clamp := o.dischargePower(i, d, power); clamp < power {
				power = clamp
			}
			if power < 1 {
				break
			}
			value, newSOC, ok := o.simulate(i, d, soc, -power)
			if !ok {
				continue
			}
			value += 1e-6 + o.mustSolve(i+d, newSOC, used+1)
			if value < best-1e-12 {
				best = value
				bestAction = action{kind: 2, dur: int8(d)}
			}
		}
	}
	o.cost[idx] = best
	o.choice[idx] = bestAction
	return best, nil
}

func (o *optimizer) mustSolve(i, soc, used int) float64 {
	if i > o.n {
		return math.Inf(1)
	}
	value, err := o.solve(i, soc, used)
	if err != nil {
		return math.Inf(1)
	}
	return value
}

func (o *optimizer) maxDuration(i int) int {
	maxD := o.in.MaxSlotBlocks
	if remaining := o.n - i; remaining < maxD {
		maxD = remaining
	}
	local := o.in.Blocks[i].Start.In(o.loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).AddDate(0, 0, 1)
	for d := 1; d <= maxD; d++ {
		if !o.in.Blocks[i+d-1].End.After(midnight) {
			continue
		}
		return d - 1
	}
	return maxD
}

func (o *optimizer) chargePower(i, d int, power float64) float64 {
	for j := i; j < i+d; j++ {
		b := o.in.Blocks[j]
		if b.Missing {
			return 0
		}
		headroom := o.in.SiteImportW - (b.LoadW + b.EVW - o.pv[j])
		if headroom < power {
			power = headroom
		}
	}
	if power < 0 {
		return 0
	}
	return power
}

func (o *optimizer) dischargePower(i, d int, power float64) float64 {
	if !o.in.AllowDischargeToGrid {
		for j := i; j < i+d; j++ {
			b := o.in.Blocks[j]
			if b.Missing {
				return 0
			}
			available := b.LoadW + b.EVW - o.pv[j]
			if available < power {
				power = available
			}
		}
	}
	if power < 0 {
		return 0
	}
	return power
}

func (o *optimizer) simulate(i, d, soc int, powerW float64) (float64, int, bool) {
	cost := 0.0
	cur := soc
	for j := i; j < i+d; j++ {
		b := o.in.Blocks[j]
		if b.Missing {
			return 0, soc, false
		}
		blockCost := o.blockCost(j, powerW)
		if math.IsInf(blockCost, 1) {
			return 0, soc, false
		}
		if powerW > 0 {
			energy := powerW * 0.5 / 1000 * o.in.ChargeEfficiency
			delta := int(math.Round(energy / o.in.CapacityKWh * 100 / socStepPct))
			if delta < 1 {
				delta = 1
			}
			cur += delta
			if cur >= o.steps {
				// Allow the last step to clamp exactly to max SOC.
				if o.socValue(cur-delta)+float64(delta)*socStepPct > o.in.MaxSOC+1e-9 {
					return 0, soc, false
				}
				cur = o.steps - 1
			}
		} else if powerW < 0 {
			if j < len(o.in.EVWindows) && o.in.EVWindows[j] {
				return 0, soc, false
			}
			if !o.in.AllowDischargeToGrid {
				net := b.LoadW + b.EVW - o.pv[j] + powerW
				if net < 0 {
					return 0, soc, false
				}
			}
			energy := -powerW * 0.5 / 1000 / o.in.DischargeEfficiency
			delta := int(math.Round(energy / o.in.CapacityKWh * 100 / socStepPct))
			if delta < 1 {
				delta = 1
			}
			cur -= delta
			reserveIdx := int(math.Ceil((math.Max(o.in.ReserveSOC, o.in.MinSOC) - o.in.MinSOC) / socStepPct))
			if cur < reserveIdx {
				return 0, soc, false
			}
		}
		cost += blockCost
	}
	return cost, cur, true
}

func (o *optimizer) blockCost(i int, batteryW float64) float64 {
	b := o.in.Blocks[i]
	if b.Missing {
		return math.Inf(1)
	}
	net := b.LoadW + b.EVW + batteryW - o.pv[i]
	if net >= 0 {
		return net * 0.5 / 1000 * b.ImportPrice
	}
	cost := net * 0.5 / 1000 * b.ExportPrice // negative price × negative energy = revenue
	if batteryW < 0 {
		cost += o.in.CycleCost * (-batteryW) * 0.5 / 1000
	}
	return cost
}

func (o *optimizer) reconstruct(startSOC int) Result {
	loc := o.loc
	result := Result{Warnings: []string{}}
	curve := make([]CurvePoint, 0, o.n)
	slots := make([]domain.Slot, 0)
	soc := startSOC
	used := 0
	i := 0
	baseline := 0.0
	total := 0.0
	chargeKWh := 0.0
	dischargeKWh := 0.0
	for i < o.n {
		if i > 0 && o.day[i] != o.day[i-1] {
			used = 0
		}
		idx := o.index(i, soc, used)
		act := o.choice[idx]
		if act.kind == 0 || act.dur == 0 {
			base := o.baselineBlockCost(i)
			cost := o.blockCost(i, 0)
			baseline += base
			total += cost
			curve = append(curve, o.curvePoint(i, 0, soc, cost))
			i++
			continue
		}
		power := math.Min(o.in.ChargeW, o.in.MaxChargeW)
		if act.kind == 2 {
			power = -o.dischargePower(i, int(act.dur), math.Min(o.in.DischargeW, o.in.MaxDischargeW))
		} else {
			power = o.chargePower(i, int(act.dur), power)
		}
		slotStart := o.in.Blocks[i].Start
		slotEnd := o.in.Blocks[i+int(act.dur)-1].End
		slotCost := 0.0
		slotBaseline := 0.0
		avgPrice := 0.0
		for j := i; j < i+int(act.dur); j++ {
			b := o.in.Blocks[j]
			blockCost := o.blockCost(j, power)
			base := o.baselineBlockCost(j)
			baseline += base
			total += blockCost
			slotCost += blockCost
			slotBaseline += base
			avgPrice += b.ImportPrice
			if power > 0 {
				energy := power * 0.5 / 1000 * o.in.ChargeEfficiency
				delta := int(math.Round(energy / o.in.CapacityKWh * 100 / socStepPct))
				if delta < 1 {
					delta = 1
				}
				soc += delta
				if soc >= o.steps {
					soc = o.steps - 1
				}
				chargeKWh += energy
			} else {
				energy := -power * 0.5 / 1000 / o.in.DischargeEfficiency
				delta := int(math.Round(energy / o.in.CapacityKWh * 100 / socStepPct))
				if delta < 1 {
					delta = 1
				}
				soc -= delta
				dischargeKWh += energy
			}
			curve = append(curve, o.curvePoint(j, power, soc, blockCost))
		}
		direction := domain.DirectionCharge
		reason := "Store low-cost energy for later."
		if power < 0 {
			direction = domain.DirectionDischarge
			reason = "Displace expensive import with stored energy."
		}
		slots = append(slots, domain.Slot{
			DayLocal:       slotStart.In(loc).Format("2006-01-02"),
			Start:          slotStart.UTC(),
			End:            slotEnd.UTC(),
			Direction:      direction,
			PowerW:         math.Round(math.Abs(power)),
			EnergyKWh:      round2(math.Abs(power) * float64(act.dur) * 0.5 / 1000),
			AvgImportPrice: avgPrice / float64(act.dur),
			ExpectedSaving: round2(slotBaseline - slotCost),
			Reason:         reason,
		})
		i += int(act.dur)
		used++
	}
	result.Slots = slots
	result.Curve = curve
	result.Summary = domain.PlanSummary{
		Cost:         round2(total),
		BaselineCost: round2(baseline),
		Savings:      round2(baseline - total),
		ChargeKWh:    round2(chargeKWh),
		DischargeKWh: round2(dischargeKWh),
		Cycles:       round2(dischargeKWh / o.in.CapacityKWh),
		SlotCount:    len(slots),
		Currency:     o.in.Currency,
	}
	return result
}

func (o *optimizer) curvePoint(i int, batteryW float64, socIndex int, cost float64) CurvePoint {
	b := o.in.Blocks[i]
	socValue := o.socValue(socIndex)
	return CurvePoint{
		Start: b.Start.UTC(), End: b.End.UTC(),
		PriceImport: b.ImportPrice, PriceExport: b.ExportPrice,
		PVW: b.PVW, PVPlanW: round1(o.pv[i]),
		LoadW: b.LoadW, EVW: b.EVW, BatteryW: round1(batteryW),
		SOC: round2(socValue), Cost: round2(cost), Missing: b.Missing,
	}
}

func (o *optimizer) baselineBlockCost(i int) float64 {
	b := o.in.Blocks[i]
	net := b.LoadW + b.EVW - o.pv[i]
	if net >= 0 {
		return net * 0.5 / 1000 * b.ImportPrice
	}
	return net * 0.5 / 1000 * b.ExportPrice
}

func emptyPlan(in Inputs) Result {
	curve := make([]CurvePoint, 0, len(in.Blocks))
	baseline := 0.0
	for i, b := range in.Blocks {
		pv := math.Max(0, b.PVW*in.PVConfidence)
		net := b.LoadW + b.EVW - pv
		cost := 0.0
		if net >= 0 {
			cost = net * 0.5 / 1000 * b.ImportPrice
		} else {
			cost = net * 0.5 / 1000 * b.ExportPrice
		}
		baseline += cost
		curve = append(curve, CurvePoint{
			Start: b.Start.UTC(), End: b.End.UTC(),
			PriceImport: b.ImportPrice, PriceExport: b.ExportPrice,
			PVW: b.PVW, PVPlanW: round1(pv), LoadW: b.LoadW, EVW: b.EVW,
			SOC: round2(in.StartSOC), Cost: round2(cost), Missing: b.Missing,
		})
		_ = i
	}
	return Result{
		Slots: []domain.Slot{},
		Curve: curve,
		Summary: domain.PlanSummary{
			Cost: round2(baseline), BaselineCost: round2(baseline),
			Savings: 0, SlotCount: 0, Currency: in.Currency,
		},
		Warnings:     []string{"Savings below the configured threshold; self-consumption plan."},
		NoPlanReason: "savings below threshold",
	}
}

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func round1(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*10) / 10
}

func round2(v float64) float64 {
	if v == 0 {
		return 0
	}
	return math.Round(v*100) / 100
}
