// Package domain contains the shared value types of Aiptimize.
//
// Timestamps are always stored and transferred in UTC. Local conversion only
// happens for display and for aligning slots to local wall-clock boundaries.
package domain

import (
	"encoding/json"
	"time"
)

// Direction is the energy direction of a battery slot.
type Direction string

const (
	DirectionCharge    Direction = "charge"
	DirectionDischarge Direction = "discharge"
)

// Valid reports whether d is a known direction.
func (d Direction) Valid() bool {
	return d == DirectionCharge || d == DirectionDischarge
}

// PriceState mirrors the Nord Pool area state.
type PriceState string

const (
	PriceStateFinal       PriceState = "Final"
	PriceStatePreliminary PriceState = "Preliminary"
	PriceStateMissing     PriceState = "Missing"
)

// PricePoint is one raw 15-minute day-ahead price point.
type PricePoint struct {
	DeliveryStart time.Time  `json:"delivery_start"`
	DeliveryEnd   time.Time  `json:"delivery_end"`
	PriceMWh      float64    `json:"price_mwh"`
	PriceKWh      float64    `json:"price_kwh"`
	State         PriceState `json:"state"`
}

// PriceBlock is a 30-minute block aggregated from two 15-minute points.
type PriceBlock struct {
	Start       time.Time  `json:"start"`
	End         time.Time  `json:"end"`
	PriceMWh    float64    `json:"price_mwh"`
	PriceKWh    float64    `json:"price_kwh"`
	ImportPrice float64    `json:"import_price"`
	ExportPrice float64    `json:"export_price"`
	State       PriceState `json:"state"`
	Missing     bool       `json:"missing"`
	Points      int        `json:"points"`
}

// Tariff describes how spot prices become import/export prices.
type Tariff struct {
	GridFee   float64 `json:"grid_fee"`
	EnergyTax float64 `json:"energy_tax"`
	Markup    float64 `json:"markup"`
	VAT       float64 `json:"vat"`
	ExportFee float64 `json:"export_fee"`
}

// ImportPrice applies the tariff formula for an import price.
func (t Tariff) ImportPrice(spot float64) float64 {
	return (spot + t.GridFee + t.EnergyTax + t.Markup) * (1 + t.VAT/100)
}

// ExportPrice applies the export formula to a spot price.
func (t Tariff) ExportPrice(spot float64) float64 {
	return spot - t.ExportFee
}

// Slot is one battery charge or discharge window.
type Slot struct {
	ID             int64     `json:"id,omitempty"`
	DayLocal       string    `json:"day_local"` // YYYY-MM-DD in the site timezone
	Start          time.Time `json:"start"`
	End            time.Time `json:"end"`
	Direction      Direction `json:"direction"`
	PowerW         float64   `json:"power_w"`
	EnergyKWh      float64   `json:"energy_kwh"`
	AvgImportPrice float64   `json:"avg_import_price"`
	ExpectedSaving float64   `json:"expected_saving"`
	Reason         string    `json:"reason,omitempty"`
	AlreadyStarted bool      `json:"already_started,omitempty"`
	Immutable      bool      `json:"immutable,omitempty"`
	OriginalStart  time.Time `json:"-"`
	OriginalEnd    time.Time `json:"-"`
	OriginalDir    Direction `json:"-"`
}

// DurationMinutes returns the slot length in whole minutes.
func (s Slot) DurationMinutes() int {
	return int(s.End.Sub(s.Start).Minutes())
}

// PlanOrigin distinguishes automatic and user-edited plans.
type PlanOrigin string

const (
	OriginAuto   PlanOrigin = "auto"
	OriginManual PlanOrigin = "manual"
)

// PlanStatus is the lifecycle state of a plan.
type PlanStatus string

const (
	PlanStatusSuggestion PlanStatus = "suggestion"
	PlanStatusDraft      PlanStatus = "draft"
	PlanStatusApplied    PlanStatus = "applied"
	PlanStatusSuperseded PlanStatus = "superseded"
	PlanStatusExpired    PlanStatus = "expired"
)

// PlanSummary is the aggregate cost result of a plan.
type PlanSummary struct {
	Cost         float64 `json:"cost"`
	BaselineCost float64 `json:"baseline_cost"`
	Savings      float64 `json:"savings"`
	ChargeKWh    float64 `json:"charge_kwh"`
	DischargeKWh float64 `json:"discharge_kwh"`
	Cycles       float64 `json:"cycles"`
	SlotCount    int     `json:"slot_count"`
	Currency     string  `json:"currency"`
}

// PlanCurvePoint is one simulated block of a battery plan.
type PlanCurvePoint struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	PriceImport float64   `json:"price_import"`
	PriceExport float64   `json:"price_export"`
	PVW         float64   `json:"pv_w"`
	PVPlanW     float64   `json:"pv_plan_w"`
	LoadW       float64   `json:"load_w"`
	EVW         float64   `json:"ev_w"`
	BatteryW    float64   `json:"battery_w"`
	SOC         float64   `json:"soc"`
	Cost        float64   `json:"cost"`
	Missing     bool      `json:"missing"`
}

// Plan is a complete battery schedule over a horizon.
type Plan struct {
	ID           int64            `json:"id"`
	CreatedAt    time.Time        `json:"created_at"`
	HorizonStart time.Time        `json:"horizon_start"`
	HorizonEnd   time.Time        `json:"horizon_end"`
	Timezone     string           `json:"timezone"`
	Status       PlanStatus       `json:"status"`
	Origin       PlanOrigin       `json:"origin"`
	Revision     int              `json:"revision"`
	LockedDay    string           `json:"locked_day,omitempty"`
	Note         string           `json:"note,omitempty"`
	InputsHash   string           `json:"inputs_hash,omitempty"`
	Summary      PlanSummary      `json:"summary"`
	Slots        []Slot           `json:"slots"`
	Curve        []PlanCurvePoint `json:"curve,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	NoPlanReason string           `json:"no_plan_reason,omitempty"`
	Inputs       json.RawMessage  `json:"inputs,omitempty"`
	AppliedAt    *time.Time       `json:"applied_at,omitempty"`
	AppliedBy    string           `json:"applied_by,omitempty"`
	ParentPlanID *int64           `json:"parent_plan_id,omitempty"`
}

// Telemetry is one inverter/meter/battery sample.
type Telemetry struct {
	TS           time.Time      `json:"ts"`
	SOC          float64        `json:"soc"`
	PVW          float64        `json:"pv_w"`
	PVStrings    []float64      `json:"pv_strings,omitempty"`
	GridW        float64        `json:"grid_w"`
	BatteryW     float64        `json:"battery_w"`
	LoadW        float64        `json:"load_w"`
	EVW          float64        `json:"ev_w"`
	InverterMode int            `json:"inverter_mode"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// ScheduleSlot is one decoded hardware schedule entry.
type ScheduleSlot struct {
	Hour        int    `json:"hour"`
	Minute      int    `json:"minute"`
	DurationMin int    `json:"duration_min"`
	Discharge   bool   `json:"discharge"`
	Code        uint32 `json:"code"`
}

// Schedule is a decoded weekly inverter schedule.
type Schedule struct {
	Days  map[string][]ScheduleSlot `json:"days"` // keys Mon..Sun as the dongle spells them
	PinW  float64                   `json:"pin_w"`
	PoutW float64                   `json:"pout_w"`
}

// ChargerStatus is a normalized charger snapshot.
type ChargerStatus struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Online     bool      `json:"online"`
	Connected  bool      `json:"connected"`
	Charging   bool      `json:"charging"`
	PowerW     float64   `json:"power_w"`
	CurrentA   float64   `json:"current_a"`
	Phases     int       `json:"phases"`
	SessionKWh float64   `json:"session_kwh"`
	Mode       string    `json:"mode"`
	Paused     bool      `json:"paused"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EVPlanState is the runtime state of an EV charging plan.
type EVPlanState string

const (
	EVStateNone          EVPlanState = "none"
	EVStateScheduled     EVPlanState = "scheduled"
	EVStateActive        EVPlanState = "active"
	EVStateTargetReached EVPlanState = "target_reached"
	EVStateOverrun       EVPlanState = "overrun"
	EVStateUnreachable   EVPlanState = "unreachable"
	EVStateExpired       EVPlanState = "expired"
	EVStateCleared       EVPlanState = "cleared_on_unplug"
	EVStateModeMismatch  EVPlanState = "mode_mismatch"
)

// EVWindow is one charging window produced by the EV planner.
type EVWindow struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	PowerW    float64   `json:"power_w"`
	EnergyKWh float64   `json:"energy_kwh"`
	Source    string    `json:"source"` // grid | pv
	Cost      float64   `json:"cost"`
}

// EVPlanPreview is the result of an EV planning run.
type EVPlanPreview struct {
	ChargerID    string      `json:"charger_id"`
	ComputedAt   time.Time   `json:"computed_at"`
	Deadline     time.Time   `json:"deadline"`
	TargetKWh    float64     `json:"target_kwh"`
	RemainingKWh float64     `json:"remaining_kwh"`
	RequiredMin  int         `json:"required_min"`
	Windows      []EVWindow  `json:"windows"`
	Cost         float64     `json:"cost"`
	State        EVPlanState `json:"state"`
	Warnings     []string    `json:"warnings,omitempty"`
	ModeMismatch bool        `json:"mode_mismatch,omitempty"`
}
