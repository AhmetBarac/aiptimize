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
  plan: unknown
  ev: { available: boolean }
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
}

export interface SafetySettings {
  writes_armed_ui: boolean
  ev_control_armed_ui: boolean
  watchdog_clear_after_s: number
  horizon_limit_days: number
  foreign_schedule_takeover: boolean
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
  plan: Record<string, unknown>
  load: LoadSettings
  ev: Record<string, unknown>
}
