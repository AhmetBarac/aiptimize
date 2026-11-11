// Package settings owns the typed, validated application settings. Values are
// stored as one JSON document per section in SQLite and are patched section by
// section with strict unknown-field rejection.
package settings

import (
	"fmt"
	"math"
)

// Section names.
const (
	SectionSite     = "site"
	SectionInverter = "inverter"
	SectionPV       = "pv"
	SectionPrice    = "price"
	SectionPlan     = "plan"
	SectionLoad     = "load"
	SectionEV       = "ev"
	SectionSafety   = "safety"
)

// Sections is the accepted order of settings sections.
var Sections = []string{
	SectionSite, SectionInverter, SectionPV, SectionPrice,
	SectionPlan, SectionLoad, SectionEV, SectionSafety,
}

// Values is the complete settings document.
type Values struct {
	Site     Site     `json:"site"`
	Inverter Inverter `json:"inverter"`
	PV       PV       `json:"pv"`
	Price    Price    `json:"price"`
	Plan     Plan     `json:"plan"`
	Load     Load     `json:"load"`
	EV       EV       `json:"ev"`
	Safety   Safety   `json:"safety"`
}

// Site is the physical installation description.
type Site struct {
	Name              string  `json:"name"`
	Timezone          string  `json:"timezone"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	MaxImportPowerW   float64 `json:"max_import_power_w"`
	GridOwner         string  `json:"grid_owner"`
	FacilityID        string  `json:"facility_id"`
	GridTariffProduct string  `json:"grid_tariff_product"`
	EltariffURL       string  `json:"eltariff_url"`
	TariffMode        string  `json:"tariff_mode"`
	TransferFeeOre    float64 `json:"transfer_fee_ore_kwh"`
	EnergyTaxOre      float64 `json:"energy_tax_ore_kwh"`
	VariableCostOre   float64 `json:"variable_cost_ore_kwh"`
	FixedMarkupOre    float64 `json:"fixed_markup_ore_kwh"`
	ExportBenefitOre  float64 `json:"export_benefit_ore_kwh"`
	ExportExtraOre    float64 `json:"export_extra_ore_kwh"`
	ShowFullPrice     bool    `json:"show_full_price"`
	// License is a Docker Hub personal access token used by the update check.
	// It is stored so the credential can be rotated from the UI; the API never
	// returns it.
	License string `json:"license"`
}

// SignOverrides calibrates vendor sign conventions during commissioning.
type SignOverrides struct {
	PV      float64 `json:"pv"`
	Grid    float64 `json:"grid"`
	Battery float64 `json:"battery"`
}

// Inverter describes the battery inverter and its limits.
type Inverter struct {
	Type                string        `json:"type"`
	Host                string        `json:"host"`
	Scheme              string        `json:"scheme"`
	Port                int           `json:"port"`
	Serial              string        `json:"serial"`
	MaxChargeW          float64       `json:"max_charge_w"`
	MaxDischargeW       float64       `json:"max_discharge_w"`
	CapacityKWh         float64       `json:"capacity_kwh"`
	MinSOC              float64       `json:"min_soc"`
	MaxSOC              float64       `json:"max_soc"`
	ChargeEfficiency    float64       `json:"charge_efficiency"`
	DischargeEfficiency float64       `json:"discharge_efficiency"`
	CycleCost           float64       `json:"cycle_cost"`
	ReserveSOC          float64       `json:"reserve_soc"`
	TelemetryIntervalS  int           `json:"telemetry_interval_s"`
	SignOverrides       SignOverrides `json:"sign_overrides"`
}

// PVString is one configured MPPT string.
type PVString struct {
	Name             string  `json:"name"`
	KWP              float64 `json:"kwp"`
	Tilt             float64 `json:"tilt"`
	Azimuth          float64 `json:"azimuth"`
	PerformanceRatio float64 `json:"performance_ratio"`
	TempCoefficient  float64 `json:"temp_coefficient"`
	MaxW             float64 `json:"max_w"`
}

// PV holds the solar forecast configuration.
type PV struct {
	Strings            []PVString `json:"strings"`
	InverterACLimitW   float64    `json:"inverter_ac_limit_w"`
	Confidence         float64    `json:"confidence"`
	RefreshMinutes     int        `json:"refresh_minutes"`
	CalibrationEnabled bool       `json:"calibration_enabled"`
}

// Price holds the tariff and day-ahead market configuration.
type Price struct {
	Provider  string  `json:"provider"`
	Area      string  `json:"area"`
	Currency  string  `json:"currency"`
	GridFee   float64 `json:"grid_fee"`
	EnergyTax float64 `json:"energy_tax"`
	Markup    float64 `json:"markup"`
	VAT       float64 `json:"vat"`
	ExportFee float64 `json:"export_fee"`
}

// Plan holds the battery planner configuration.
type Plan struct {
	MaxSlotsPerDay       int     `json:"max_slots_per_day"`
	MaxSlotMin           int     `json:"max_slot_min"`
	HorizonDays          int     `json:"horizon_days"`
	MinDailySaving       float64 `json:"min_daily_saving"`
	PreserveSOC          bool    `json:"preserve_soc"`
	EndSOCTarget         float64 `json:"end_soc_target"`
	SOCStepPct           float64 `json:"soc_step_pct"`
	ChargeW              float64 `json:"charge_w"`
	DischargeW           float64 `json:"discharge_w"`
	AllowDischargeToGrid bool    `json:"allow_discharge_to_grid"`
	Strategy             string  `json:"strategy"`
	MinPriceDifference   float64 `json:"min_price_difference"`
	MaxPurchasePrice     float64 `json:"max_purchase_price"`
	PeakShavingEnabled   bool    `json:"peak_shaving_enabled"`
	PeakShavingLimitW    float64 `json:"peak_shaving_limit_w"`
}

// Load holds the house-load forecast configuration.
type Load struct {
	DailyKWh                  float64   `json:"daily_kwh"`
	Profile                   []float64 `json:"profile"`
	UseMeasured               bool      `json:"use_measured_profile"`
	UseTemperatureAdjustment  bool      `json:"use_temperature_adjustment"`
	TemperatureReferenceC     float64   `json:"temperature_reference_c"`
	TemperatureSensitivityPct float64   `json:"temperature_sensitivity_pct_c"`
}

// EVPlanDefaults are the defaults applied to new EV plans.
type EVPlanDefaults struct {
	Strategy        string `json:"strategy"`
	PreconditionMin int    `json:"precondition_min"`
}

// EV holds the site-wide EV charging defaults.
type EV struct {
	Provider                string         `json:"provider"`
	ChargerID               string         `json:"charger_id"`
	InstallationID          string         `json:"installation_id"`
	ZaptecUsername          string         `json:"zaptec_username"`
	ZaptecPassword          string         `json:"zaptec_password,omitempty"`
	Enabled                 bool           `json:"enabled"`
	Mode                    string         `json:"mode"`
	DefaultMode             string         `json:"default_mode"`
	MinCurrentA             float64        `json:"min_current_a"`
	MaxCurrentA             float64        `json:"max_current_a"`
	Phases                  int            `json:"phases"`
	VoltageV                float64        `json:"voltage_v"`
	Priority                int            `json:"priority"`
	SolarShare              float64        `json:"solar_share"`
	SmartCostLimit          *float64       `json:"smart_cost_limit_sek_kwh"`
	FeedInPriorityLimit     *float64       `json:"smart_feed_in_priority_limit_sek_kwh"`
	AlwaysCharge            string         `json:"always_charge"`
	HouseBatteryFloorSOC    float64        `json:"house_battery_floor_soc"`
	BatteryDischargeControl bool           `json:"battery_discharge_control"`
	AllowEVBatteryCharging  bool           `json:"allow_ev_battery_charging"`
	GridChargeAllowed       bool           `json:"grid_charge_allowed"`
	BatteryBoost            bool           `json:"battery_boost"`
	BatteryBoostLimitSOC    float64        `json:"battery_boost_limit_soc"`
	PlanDefaults            EVPlanDefaults `json:"plan_defaults"`
}

// Safety holds write gates and watchdog configuration. The *_armed_ui fields
// are only the in-app half of the gate; the environment half is separate.
type Safety struct {
	WritesArmedUI           bool   `json:"writes_armed_ui"`
	EVControlArmedUI        bool   `json:"ev_control_armed_ui"`
	WatchdogClearAfterS     int    `json:"watchdog_clear_after_s"`
	HorizonLimitDays        int    `json:"horizon_limit_days"`
	ForeignScheduleTakeover bool   `json:"foreign_schedule_takeover"`
	AutoScheduleWrite       string `json:"auto_schedule_write"`
}

// Default returns the documented default settings.
func Default() Values {
	profile := make([]float64, len(profileWeights))
	var sum float64
	for _, w := range profileWeights {
		sum += w
	}
	for i, w := range profileWeights {
		profile[i] = math.Round(w/sum*1e6) / 1e6
	}
	// Force the profile to sum to exactly 1.0 at 6 decimals.
	var acc float64
	for _, v := range profile[:23] {
		acc += v
	}
	profile[23] = math.Round((1-acc)*1e6) / 1e6

	return Values{
		Site: Site{
			Name:            "Home",
			Timezone:        "Europe/Stockholm",
			Latitude:        59.3293,
			Longitude:       18.0686,
			MaxImportPowerW: 17250,
			TariffMode:      "manual",
		},
		Inverter: Inverter{
			Type:                "solplanet_ai_dongle",
			Host:                "",
			Scheme:              "http",
			Port:                8484,
			Serial:              "",
			MaxChargeW:          6000,
			MaxDischargeW:       3000,
			CapacityKWh:         15.36,
			MinSOC:              15,
			MaxSOC:              100,
			ChargeEfficiency:    0.95,
			DischargeEfficiency: 0.95,
			CycleCost:           0,
			ReserveSOC:          15,
			TelemetryIntervalS:  30,
			SignOverrides:       SignOverrides{PV: 1, Grid: 1, Battery: 1},
		},
		PV: PV{
			Strings:          []PVString{},
			InverterACLimitW: 8000,
			Confidence:       0.7,
			RefreshMinutes:   30,
		},
		Price: Price{
			Provider:  "nordpool_dataportal",
			Area:      "SE4",
			Currency:  "SEK",
			GridFee:   0,
			EnergyTax: 0,
			Markup:    0,
			VAT:       25,
			ExportFee: 0,
		},
		Plan: Plan{
			MaxSlotsPerDay:       4,
			MaxSlotMin:           240,
			HorizonDays:          2,
			MinDailySaving:       0,
			PreserveSOC:          true,
			EndSOCTarget:         50,
			SOCStepPct:           0.5,
			ChargeW:              6000,
			DischargeW:           3000,
			AllowDischargeToGrid: true,
			Strategy:             "minimize_cost",
		},
		Load: Load{
			DailyKWh:                  12,
			Profile:                   profile,
			UseMeasured:               false,
			TemperatureReferenceC:     18,
			TemperatureSensitivityPct: 2,
		},
		EV: EV{
			Provider:                "zaptec",
			Enabled:                 true,
			Mode:                    "off",
			DefaultMode:             "smart",
			MinCurrentA:             6,
			MaxCurrentA:             16,
			Phases:                  3,
			VoltageV:                230,
			Priority:                0,
			SolarShare:              1.0,
			AlwaysCharge:            "off",
			HouseBatteryFloorSOC:    20,
			BatteryDischargeControl: true,
			AllowEVBatteryCharging:  false,
			GridChargeAllowed:       false,
			BatteryBoost:            false,
			BatteryBoostLimitSOC:    100,
			PlanDefaults:            EVPlanDefaults{Strategy: "cheapest", PreconditionMin: 0},
		},
		Safety: Safety{
			WritesArmedUI:           false,
			EVControlArmedUI:        false,
			WatchdogClearAfterS:     1800,
			HorizonLimitDays:        2,
			ForeignScheduleTakeover: false,
			AutoScheduleWrite:       "off",
		},
	}
}

// profileWeights is the default house-load shape before normalisation.
var profileWeights = []float64{
	30, 26, 24, 23, 24, 30, 40, 45, 40, 34, 32, 32,
	34, 33, 32, 34, 40, 48, 52, 50, 45, 40, 36, 33,
}

// sectionValue returns a pointer to a section for patch application.
func (v *Values) sectionValue(name string) (any, error) {
	switch name {
	case SectionSite:
		return &v.Site, nil
	case SectionInverter:
		return &v.Inverter, nil
	case SectionPV:
		return &v.PV, nil
	case SectionPrice:
		return &v.Price, nil
	case SectionPlan:
		return &v.Plan, nil
	case SectionLoad:
		return &v.Load, nil
	case SectionEV:
		return &v.EV, nil
	case SectionSafety:
		return &v.Safety, nil
	default:
		return nil, fmt.Errorf("unknown settings section %q", name)
	}
}

func validSection(name string) bool {
	for _, s := range Sections {
		if s == name {
			return true
		}
	}
	return false
}
