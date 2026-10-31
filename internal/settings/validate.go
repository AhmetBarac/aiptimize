package settings

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ValidationError describes a rejected settings value.
type ValidationError struct {
	Section string
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Section, e.Message)
	}
	return fmt.Sprintf("%s.%s: %s", e.Section, e.Field, e.Message)
}

func invalid(section, field, format string, args ...any) error {
	return &ValidationError{Section: section, Field: field, Message: fmt.Sprintf(format, args...)}
}

// ValidateSection validates one section value.
func ValidateSection(section string, value any) error {
	switch section {
	case SectionSite:
		v, ok := value.(*Site)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validateSite(v)
	case SectionInverter:
		v, ok := value.(*Inverter)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validateInverter(v)
	case SectionPV:
		v, ok := value.(*PV)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validatePV(v)
	case SectionPrice:
		v, ok := value.(*Price)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validatePrice(v)
	case SectionPlan:
		v, ok := value.(*Plan)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validatePlan(v)
	case SectionLoad:
		v, ok := value.(*Load)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validateLoad(v)
	case SectionEV:
		v, ok := value.(*EV)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validateEV(v)
	case SectionSafety:
		v, ok := value.(*Safety)
		if !ok {
			return invalid(section, "", "internal type mismatch")
		}
		return validateSafety(v)
	default:
		return fmt.Errorf("unknown settings section %q", section)
	}
}

func validateSite(v *Site) error {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return invalid(SectionSite, "name", "must not be empty")
	}
	if len(v.Name) > 80 {
		return invalid(SectionSite, "name", "must be at most 80 characters")
	}
	if v.Timezone == "" {
		return invalid(SectionSite, "timezone", "must not be empty")
	}
	if _, err := time.LoadLocation(v.Timezone); err != nil {
		return invalid(SectionSite, "timezone", "unknown IANA timezone %q", v.Timezone)
	}
	if v.Latitude < -90 || v.Latitude > 90 {
		return invalid(SectionSite, "latitude", "must be between -90 and 90")
	}
	if v.Longitude < -180 || v.Longitude > 180 {
		return invalid(SectionSite, "longitude", "must be between -180 and 180")
	}
	if v.MaxImportPowerW <= 0 || v.MaxImportPowerW > 100000 {
		return invalid(SectionSite, "max_import_power_w", "must be between 1 and 100000 W")
	}
	v.GridOwner = strings.TrimSpace(v.GridOwner)
	v.FacilityID = strings.TrimSpace(v.FacilityID)
	v.GridTariffProduct = strings.TrimSpace(v.GridTariffProduct)
	v.EltariffURL = strings.TrimSpace(v.EltariffURL)
	if len(v.GridOwner) > 120 || len(v.FacilityID) > 80 || len(v.GridTariffProduct) > 120 {
		return invalid(SectionSite, "grid_tariff", "grid owner, facility ID or product is too long")
	}
	if v.EltariffURL != "" {
		u, err := url.Parse(v.EltariffURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid(SectionSite, "eltariff_url", "must be an absolute http(s) URL")
		}
	}
	switch v.TariffMode {
	case "", "manual":
		v.TariffMode = "manual"
	case "api":
	default:
		return invalid(SectionSite, "tariff_mode", "must be manual or api")
	}
	for name, value := range map[string]float64{
		"transfer_fee_ore_kwh":   v.TransferFeeOre,
		"energy_tax_ore_kwh":     v.EnergyTaxOre,
		"variable_cost_ore_kwh":  v.VariableCostOre,
		"fixed_markup_ore_kwh":   v.FixedMarkupOre,
		"export_benefit_ore_kwh": v.ExportBenefitOre,
		"export_extra_ore_kwh":   v.ExportExtraOre,
	} {
		if value < 0 || value > 10000 {
			return invalid(SectionSite, name, "must be between 0 and 10000 öre/kWh")
		}
	}
	return nil
}

func validateInverter(v *Inverter) error {
	if v.Type == "" {
		return invalid(SectionInverter, "type", "must not be empty")
	}
	if v.Scheme != "http" && v.Scheme != "https" {
		return invalid(SectionInverter, "scheme", "must be http or https")
	}
	if v.Port < 1 || v.Port > 65535 {
		return invalid(SectionInverter, "port", "must be between 1 and 65535")
	}
	if v.MaxChargeW <= 0 || v.MaxChargeW > 50000 {
		return invalid(SectionInverter, "max_charge_w", "must be between 1 and 50000 W")
	}
	if v.MaxDischargeW <= 0 || v.MaxDischargeW > 50000 {
		return invalid(SectionInverter, "max_discharge_w", "must be between 1 and 50000 W")
	}
	if v.CapacityKWh <= 0 || v.CapacityKWh > 500 {
		return invalid(SectionInverter, "capacity_kwh", "must be between 0 and 500 kWh")
	}
	if v.MinSOC < 0 || v.MinSOC >= 100 {
		return invalid(SectionInverter, "min_soc", "must be between 0 and 100")
	}
	if v.MaxSOC <= 0 || v.MaxSOC > 100 || v.MaxSOC <= v.MinSOC {
		return invalid(SectionInverter, "max_soc", "must be greater than min_soc and at most 100")
	}
	if v.ReserveSOC < v.MinSOC || v.ReserveSOC > v.MaxSOC {
		return invalid(SectionInverter, "reserve_soc", "must be between min_soc and max_soc")
	}
	if v.ChargeEfficiency <= 0 || v.ChargeEfficiency > 1 {
		return invalid(SectionInverter, "charge_efficiency", "must be in (0, 1]")
	}
	if v.DischargeEfficiency <= 0 || v.DischargeEfficiency > 1 {
		return invalid(SectionInverter, "discharge_efficiency", "must be in (0, 1]")
	}
	if v.CycleCost < 0 {
		return invalid(SectionInverter, "cycle_cost", "must not be negative")
	}
	if v.TelemetryIntervalS < 5 || v.TelemetryIntervalS > 3600 {
		return invalid(SectionInverter, "telemetry_interval_s", "must be between 5 and 3600 seconds")
	}
	for name, sign := range map[string]float64{"pv": v.SignOverrides.PV, "grid": v.SignOverrides.Grid, "battery": v.SignOverrides.Battery} {
		if sign != 1 && sign != -1 {
			return invalid(SectionInverter, "sign_overrides."+name, "must be 1 or -1")
		}
	}
	return nil
}

func validatePV(v *PV) error {
	if len(v.Strings) > 3 {
		return invalid(SectionPV, "strings", "at most 3 strings are supported")
	}
	names := map[string]bool{}
	for i := range v.Strings {
		s := &v.Strings[i]
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			s.Name = fmt.Sprintf("MPPT %d", i+1)
		}
		if names[s.Name] {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].name", i), "duplicate name %q", s.Name)
		}
		names[s.Name] = true
		if s.KWP <= 0 || s.KWP > 100 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].kwp", i), "must be between 0 and 100 kWp")
		}
		if s.Tilt < 0 || s.Tilt > 90 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].tilt", i), "must be between 0 and 90 degrees")
		}
		if s.Azimuth < 0 || s.Azimuth >= 360 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].azimuth", i), "must be between 0 and 359 degrees")
		}
		if s.PerformanceRatio <= 0 || s.PerformanceRatio > 1.5 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].performance_ratio", i), "must be in (0, 1.5]")
		}
		if s.TempCoefficient > 0 || s.TempCoefficient < -0.02 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].temp_coefficient", i), "must be between -0.02 and 0 per degree C")
		}
		if s.MaxW < 0 {
			return invalid(SectionPV, fmt.Sprintf("strings[%d].max_w", i), "must not be negative")
		}
	}
	if v.InverterACLimitW <= 0 || v.InverterACLimitW > 50000 {
		return invalid(SectionPV, "inverter_ac_limit_w", "must be between 1 and 50000 W")
	}
	if v.Confidence < 0 || v.Confidence > 1 {
		return invalid(SectionPV, "confidence", "must be between 0 and 1")
	}
	if v.RefreshMinutes < 5 || v.RefreshMinutes > 360 {
		return invalid(SectionPV, "refresh_minutes", "must be between 5 and 360")
	}
	return nil
}

func validatePrice(v *Price) error {
	if v.Provider == "" {
		return invalid(SectionPrice, "provider", "must not be empty")
	}
	if v.Area == "" {
		return invalid(SectionPrice, "area", "must not be empty")
	}
	if len(v.Currency) != 3 || strings.ToUpper(v.Currency) != v.Currency {
		return invalid(SectionPrice, "currency", "must be a 3-letter uppercase ISO code")
	}
	for name, fee := range map[string]float64{
		"grid_fee": v.GridFee, "energy_tax": v.EnergyTax, "markup": v.Markup, "export_fee": v.ExportFee,
	} {
		if fee < 0 {
			return invalid(SectionPrice, name, "must not be negative")
		}
	}
	if v.VAT < 0 || v.VAT > 100 {
		return invalid(SectionPrice, "vat", "must be between 0 and 100 percent")
	}
	return nil
}

func validatePlan(v *Plan) error {
	if v.MaxSlotsPerDay < 1 || v.MaxSlotsPerDay > 6 {
		return invalid(SectionPlan, "max_slots_per_day", "must be between 1 and 6")
	}
	if v.MaxSlotMin < 30 || v.MaxSlotMin > 240 || v.MaxSlotMin%30 != 0 {
		return invalid(SectionPlan, "max_slot_min", "must be 30-240 minutes in 30-minute steps")
	}
	if v.HorizonDays < 1 || v.HorizonDays > 2 {
		return invalid(SectionPlan, "horizon_days", "must be 1 or 2")
	}
	if v.MinDailySaving < 0 {
		return invalid(SectionPlan, "min_daily_saving", "must not be negative")
	}
	if v.EndSOCTarget < 0 || v.EndSOCTarget > 100 {
		return invalid(SectionPlan, "end_soc_target", "must be between 0 and 100")
	}
	if v.SOCStepPct <= 0 || v.SOCStepPct > 5 {
		return invalid(SectionPlan, "soc_step_pct", "must be in (0, 5]")
	}
	if v.ChargeW <= 0 || v.ChargeW > 50000 {
		return invalid(SectionPlan, "charge_w", "must be between 1 and 50000 W")
	}
	if v.DischargeW <= 0 || v.DischargeW > 50000 {
		return invalid(SectionPlan, "discharge_w", "must be between 1 and 50000 W")
	}
	switch v.Strategy {
	case "", "savings":
		v.Strategy = "minimize_cost"
	case "minimize_cost", "maximize_profit":
	default:
		return invalid(SectionPlan, "strategy", "must be minimize_cost or maximize_profit")
	}
	if v.MinPriceDifference < 0 {
		return invalid(SectionPlan, "min_price_difference", "must not be negative")
	}
	if v.MaxPurchasePrice < 0 {
		return invalid(SectionPlan, "max_purchase_price", "must not be negative")
	}
	if v.PeakShavingLimitW < 0 {
		return invalid(SectionPlan, "peak_shaving_limit_w", "must not be negative")
	}
	if v.PeakShavingEnabled && v.PeakShavingLimitW <= 0 {
		return invalid(SectionPlan, "peak_shaving_limit_w", "must be positive when peak shaving is enabled")
	}
	return nil
}

func validateLoad(v *Load) error {
	if v.DailyKWh < 0 || v.DailyKWh > 500 {
		return invalid(SectionLoad, "daily_kwh", "must be between 0 and 500 kWh")
	}
	if len(v.Profile) != 24 {
		return invalid(SectionLoad, "profile", "must contain 24 hourly values")
	}
	var sum float64
	for i, p := range v.Profile {
		if p < 0 || p > 1 {
			return invalid(SectionLoad, fmt.Sprintf("profile[%d]", i), "must be between 0 and 1")
		}
		sum += p
	}
	if sum < 0.9 || sum > 1.1 {
		return invalid(SectionLoad, "profile", "must sum to approximately 1.0 (got %.3f)", sum)
	}
	if v.TemperatureReferenceC < -40 || v.TemperatureReferenceC > 50 {
		return invalid(SectionLoad, "temperature_reference_c", "must be between -40 and 50 °C")
	}
	if v.TemperatureSensitivityPct < 0 || v.TemperatureSensitivityPct > 20 {
		return invalid(SectionLoad, "temperature_sensitivity_pct_c", "must be between 0 and 20 %%/°C")
	}
	return nil
}

func validateEV(v *EV) error {
	v.Provider = strings.ToLower(strings.TrimSpace(v.Provider))
	v.ChargerID = strings.TrimSpace(v.ChargerID)
	v.InstallationID = strings.TrimSpace(v.InstallationID)
	v.ZaptecUsername = strings.TrimSpace(v.ZaptecUsername)
	if v.Provider == "" {
		v.Provider = "zaptec"
	}
	switch v.Provider {
	case "zaptec", "simulator":
	default:
		return invalid(SectionEV, "provider", "must be zaptec or simulator")
	}
	if len(v.ChargerID) > 120 || len(v.InstallationID) > 120 {
		return invalid(SectionEV, "charger_id", "must be at most 120 characters")
	}
	if len(v.ZaptecUsername) > 200 || len(v.ZaptecPassword) > 500 {
		return invalid(SectionEV, "zaptec_credentials", "credentials are too long")
	}
	if v.Mode != "off" && v.Mode != "now" && v.Mode != "smart" && v.Mode != "pv" && v.Mode != "minpv" {
		return invalid(SectionEV, "mode", "must be off, now or smart")
	}
	if v.DefaultMode != "off" && v.DefaultMode != "now" && v.DefaultMode != "smart" {
		return invalid(SectionEV, "default_mode", "must be off, now or smart")
	}
	if v.MinCurrentA < 6 || v.MinCurrentA > 32 {
		return invalid(SectionEV, "min_current_a", "must be between 6 and 32 A")
	}
	if v.MaxCurrentA < v.MinCurrentA || v.MaxCurrentA > 32 {
		return invalid(SectionEV, "max_current_a", "must be between min_current_a and 32 A")
	}
	if v.Phases < 1 || v.Phases > 3 {
		return invalid(SectionEV, "phases", "must be 1, 2 or 3")
	}
	if v.VoltageV < 100 || v.VoltageV > 300 {
		return invalid(SectionEV, "voltage_v", "must be between 100 and 300 V")
	}
	if v.SolarShare < 0 || v.SolarShare > 1 {
		return invalid(SectionEV, "solar_share", "must be between 0 and 1")
	}
	if v.AlwaysCharge != "off" && v.AlwaysCharge != "on" && v.AlwaysCharge != "once" {
		return invalid(SectionEV, "always_charge", "must be off, on or once")
	}
	if v.HouseBatteryFloorSOC < 0 || v.HouseBatteryFloorSOC > 100 {
		return invalid(SectionEV, "house_battery_floor_soc", "must be between 0 and 100")
	}
	if v.BatteryBoostLimitSOC < 0 || v.BatteryBoostLimitSOC > 100 {
		return invalid(SectionEV, "battery_boost_limit_soc", "must be between 0 and 100")
	}
	if v.PlanDefaults.Strategy != "cheapest" && v.PlanDefaults.Strategy != "continuous" {
		return invalid(SectionEV, "plan_defaults.strategy", "must be cheapest or continuous")
	}
	if v.PlanDefaults.PreconditionMin < 0 || v.PlanDefaults.PreconditionMin > 720 {
		return invalid(SectionEV, "plan_defaults.precondition_min", "must be between 0 and 720")
	}
	if v.PlanDefaults.PreconditionMin%5 != 0 {
		return invalid(SectionEV, "plan_defaults.precondition_min", "must be a multiple of 5 minutes")
	}
	return nil
}

func validateSafety(v *Safety) error {
	if v.WatchdogClearAfterS < 60 || v.WatchdogClearAfterS > 86400 {
		return invalid(SectionSafety, "watchdog_clear_after_s", "must be between 60 and 86400 seconds")
	}
	if v.HorizonLimitDays < 1 || v.HorizonLimitDays > 2 {
		return invalid(SectionSafety, "horizon_limit_days", "must be 1 or 2")
	}
	return nil
}
