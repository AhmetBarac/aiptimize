export interface Gates {
  writes_env: boolean
  writes_ui: boolean
  writes_armed: boolean
  ev_control_env: boolean
  ev_control_ui: boolean
  ev_control_armed: boolean
  simulator: boolean
}

export interface Meta {
  version: string
  commit: string
  build_date: string
  now: string
  timezone: string
  setup_required: boolean
  gates: Gates
  features: Record<string, unknown>
  site: {
    name: string
    currency: string
    area: string
    show_full_price: boolean
  }
  ev?: {
    provider: string
    active_provider: string
    restart_required: boolean
  }
}

export interface User {
  id: number
  username: string
}

export interface TelemetrySample {
  ts: string
  soc: number
  pv_w: number
  pv_strings: number[]
  grid_w: number
  battery_w: number
  load_w: number
  ev_w: number
  inverter_mode: number
  extra?: Record<string, unknown>
}

export interface TelemetryStatus {
  available: boolean
  stale: boolean
  age_s?: number
  updated_at?: string
  sample: TelemetrySample | null
}

export interface ForecastString {
  name: string
  kwp: number
  tilt: number
  azimuth: number
}

export interface ForecastPoint {
  start: string
  end: string
  pv_w: number
  pv_strings: number[]
  load_w: number
}

export interface ForecastSnapshot {
  generated_at: string
  horizon_start: string
  horizon_end: string
  timezone: string
  confidence: number
  calibration_factor: number
  calibration_samples: number
  stale: boolean
  pv_total_kwh: number
  load_total_kwh: number
  strings: ForecastString[]
  points: ForecastPoint[]
  warnings: string[]
  source: string
}

export interface ForecastStatus {
  available: boolean
  stale: boolean
  generated_at?: string
  pv_total_kwh?: number
  load_total_kwh?: number
  source?: string
}

export interface SolarPoint {
  start: string
  end: string
  actual_w: number | null
  expected_w: number
  min_w: number
  max_w: number
  load_w: number
}

export interface SolarSnapshot {
  date: string
  timezone: string
  now: string
  current_w: number
  actual_kwh: number
  expected_kwh: number
  min_kwh: number
  max_kwh: number
  load_kwh: number
  has_forecast: boolean
  stale: boolean
  generated_at?: string
  points: SolarPoint[]
  warnings: string[]
}

export interface PVStringSettings {
  name: string
  kwp: number
  tilt: number
  azimuth: number
  performance_ratio: number
  temp_coefficient: number
  max_w: number
}

export interface PVSettings {
  strings: PVStringSettings[]
  inverter_ac_limit_w: number
  confidence: number
  refresh_minutes: number
  calibration_enabled: boolean
}

export interface LoadSettings {
  daily_kwh: number
  profile: number[]
  use_measured_profile: boolean
  use_temperature_adjustment: boolean
  temperature_reference_c: number
  temperature_sensitivity_pct_c: number
}

export interface PlanSettings {
  max_slots_per_day: number
  max_slot_min: number
  horizon_days: number
  min_daily_saving: number
  preserve_soc: boolean
  end_soc_target: number
  soc_step_pct: number
  charge_w: number
  discharge_w: number
  allow_discharge_to_grid: boolean
  strategy: string
  min_price_difference: number
  max_purchase_price: number
  peak_shaving_enabled: boolean
  peak_shaving_limit_w: number
}

export interface PlanSummary {
  cost: number
  baseline_cost: number
  savings: number
  charge_kwh: number
  discharge_kwh: number
  cycles: number
  slot_count: number
  currency: string
}

export interface PlanSlot {
  id?: number
  day_local: string
  start: string
  end: string
  direction: 'charge' | 'discharge'
  power_w: number
  energy_kwh: number
  avg_import_price: number
  expected_saving: number
  reason?: string
}

export interface PlanCurvePoint {
  start: string
  end: string
  price_import: number
  price_export: number
  pv_w: number
  pv_plan_w: number
  load_w: number
  ev_w: number
  battery_w: number
  soc: number
  cost: number
  missing: boolean
}

export interface Plan {
  id: number
  created_at: string
  horizon_start: string
  horizon_end: string
  timezone: string
  status: 'suggestion' | 'draft' | 'applied' | 'superseded' | 'expired'
  origin: 'auto' | 'manual'
  revision: number
  locked_day?: string
  note?: string
  inputs_hash?: string
  summary: PlanSummary
  slots: PlanSlot[]
  curve: PlanCurvePoint[]
  warnings: string[]
  no_plan_reason?: string
  inputs?: Record<string, unknown>
}

export interface PlanStatus {
  available: boolean
  id?: number
  created_at?: string
  horizon_start?: string
  horizon_end?: string
  status?: string
  summary?: PlanSummary
  slots?: PlanSlot[]
  next_slot?: PlanSlot | null
  warnings?: string[]
  no_plan_reason?: string
}

export interface PlanValidation {
  errors?: string[] | null
  warnings?: string[] | null
}

export interface PlanView {
  date: string
  auto?: Plan
  draft?: Plan
  applied?: Plan
  effective?: Plan
  inverter_plan?: Plan
  locked: boolean
  validation: PlanValidation
  schedule_diff: string[]
  inverter_match: boolean
  inverter_read_ok?: boolean
  foreign_schedule: boolean
  sync?: PlanSync
  last_write?: ScheduleWrite
  warnings: string[]
}

export interface PlanSync {
  id: number
  ts: string
  day: string
  plan_id?: number
  plan_revision: number
  origin?: 'auto' | 'manual'
  status?: 'suggestion' | 'draft' | 'applied' | 'superseded' | 'expired'
  match: boolean
  diff: string[]
  source: string
}

export interface ScheduleWrite {
  id: number
  ts: string
  kind: string
  ok: boolean
  plan_id?: number
  plan_revision?: number
}

export interface DraftSlot {
  start: string
  end: string
  direction: 'charge' | 'discharge'
  power_w: number
}

export interface ScheduleSlot {
  hour: number
  minute: number
  duration_min: number
  discharge: boolean
  code: number
}

export interface Schedule {
  days: Record<string, ScheduleSlot[]>
  pin_w: number
  pout_w: number
}

export interface SchedulePayload {
  schedule: Schedule
  matches?: boolean | null
  diff?: string[]
  foreign_schedule?: boolean
}

export interface PlanApplyResult {
  plan: Plan
  schedule: Schedule
  schedule_diff: string[]
  wrote: boolean
  message: string
}

export interface EVWindow {
  start: string
  end: string
  power_w: number
  energy_kwh: number
  source: 'grid' | 'pv'
  cost: number
}

export interface EVPreview {
  charger_id: string
  computed_at: string
  deadline: string
  target_kwh: number
  remaining_kwh: number
  required_min: number
  windows: EVWindow[]
  cost: number
  state: string
  warnings?: string[]
  mode_mismatch?: boolean
}

export interface EVPlan {
  id: number
  charger_id: string
  kind: 'static' | 'repeating'
  weekdays?: number[]
  deadline_local: string
  target_energy_kwh: number
  precondition_min: number
  strategy: string
  active: boolean
  created_at: string
  updated_at: string
}

export interface ChargerStatus {
  id: string
  type: string
  online: boolean
  connected: boolean
  charging: boolean
  power_w: number
  current_a: number
  phases: number
  session_kwh: number
  mode: string
  paused: boolean
  updated_at: string
}

export interface ChargerCapabilities {
  min_current_a: number
  max_current_a: number
  phases: number
  current_scope: string
  supports_pause: boolean
  supports_phases: boolean
  control_latency: number
}

export interface EVSettings {
  provider: string
  charger_id: string
  installation_id: string
  zaptec_username: string
  zaptec_password?: string
  enabled: boolean
  mode: string
  default_mode: string
  min_current_a: number
  max_current_a: number
  phases: number
  voltage_v: number
  priority: number
  solar_share: number
  smart_cost_limit_sek_kwh?: number | null
  smart_feed_in_priority_limit_sek_kwh?: number | null
  always_charge: string
  house_battery_floor_soc: number
  battery_discharge_control: boolean
  grid_charge_allowed: boolean
  battery_boost: boolean
  battery_boost_limit_soc: number
  plan_defaults: { strategy: string; precondition_min: number }
}

export interface EVConfig {
  provider: string
  charger_id: string
  installation_id: string
  zaptec_username: string
  zaptec_password_set: boolean
  active_provider: string
  restart_required: boolean
  credentials: {
    zaptec: boolean
    easee: boolean
    ocpp_token: boolean
  }
}

export interface ChargerPayload {
  id: string
  type: string
  capabilities: ChargerCapabilities
  status: ChargerStatus
  config: EVSettings
  preview?: EVPreview
}

export interface ChargerPlanPayload {
  plans: EVPlan[]
  session?: unknown
  has_preview: boolean
  preview?: EVPreview
}

export interface HistoryBucket {
  label: string
  start: string
  end: string
  pv_kwh: number
  load_kwh: number
  ev_kwh: number
  grid_import_kwh: number
  grid_export_kwh: number
  battery_charge_kwh: number
  battery_discharge_kwh: number
  plan_battery_kwh: number
  cost: number
  baseline_cost: number
  savings: number
}

export interface HistorySnapshot {
  from: string
  to: string
  resolution: 'day' | 'week'
  timezone: string
  currency: string
  buckets: HistoryBucket[]
  totals: HistoryBucket
  warnings: string[]
}

export interface Status {
  version: string
  now: string
  uptime_s: number
  site: {
    name: string
    timezone: string
    currency: string
    area: string
  }
  gates: Gates
  telemetry: TelemetryStatus
  prices: PriceStatus
  forecast: ForecastStatus
  plan: PlanStatus
  ev: { available: boolean; id?: string; status?: ChargerStatus; plan?: EVPreview }
  schedule: { available: boolean }
  checks: Record<string, string>
}

export interface PricePoint {
  delivery_start: string
  delivery_end: string
  price_mwh: number
  price_kwh: number
  state: string
}

export interface PriceBlock {
  start: string
  end: string
  price_mwh: number
  price_kwh: number
  import_price: number
  export_price: number
  state: string
  missing: boolean
  points: number
}

export interface PriceWindow {
  start: string
  end: string
  avg_import_price: number
  avg_export_price: number
  blocks: number
}

export interface PriceTariff {
  grid_fee: number
  energy_tax: number
  markup: number
  vat: number
  export_fee: number
}

export interface PriceSummary {
  min_import: number
  max_import: number
  avg_import: number
  points: number
  blocks: number
  cheapest_windows: PriceWindow[]
}

export interface PriceSnapshot {
  area: string
  currency: string
  timezone: string
  from: string
  to: string
  points: PricePoint[]
  blocks: PriceBlock[]
  tariff: PriceTariff
  summary: PriceSummary
  state: string
  fetched_at?: string
  source: string
  attribution: string
  stale: boolean
}

export interface PriceStatus {
  available: boolean
  points: number
  blocks: number
  state: string
  area?: string
  currency?: string
  stale?: boolean
  updated_at?: string
  now_import?: number
  now_spot?: number
  next_import?: number
  next_start?: string
  cheapest_window?: PriceWindow
}

export interface SiteSettings {
  name: string
  timezone: string
  latitude: number
  longitude: number
  max_import_power_w: number
  grid_owner: string
  facility_id: string
  grid_tariff_product: string
  eltariff_url: string
  tariff_mode: string
  transfer_fee_ore_kwh: number
  energy_tax_ore_kwh: number
  variable_cost_ore_kwh: number
  fixed_markup_ore_kwh: number
  export_benefit_ore_kwh: number
  export_extra_ore_kwh: number
  show_full_price: boolean
}

export interface SafetySettings {
  writes_armed_ui: boolean
  ev_control_armed_ui: boolean
  watchdog_clear_after_s: number
  horizon_limit_days: number
  foreign_schedule_takeover: boolean
  auto_schedule_write: 'off' | 'when_changed'
}

export interface InverterSettings {
  type: string
  host: string
  scheme: string
  port: number
  serial: string
  max_charge_w: number
  max_discharge_w: number
  capacity_kwh: number
  min_soc: number
  max_soc: number
  charge_efficiency: number
  discharge_efficiency: number
  cycle_cost: number
  reserve_soc: number
  telemetry_interval_s: number
  sign_overrides: { pv: number; grid: number; battery: number }
}

export interface PriceSettings {
  provider: string
  area: string
  currency: string
  grid_fee: number
  energy_tax: number
  markup: number
  vat: number
  export_fee: number
}

export interface SettingsDocument {
  site: SiteSettings
  inverter: InverterSettings
  price: PriceSettings
  safety: SafetySettings
  pv: PVSettings
  plan: PlanSettings
  load: LoadSettings
  ev: EVSettings
}
