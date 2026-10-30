import type { HistoryBucket, TelemetrySample } from '../api/types'

export type SourceKey = 'solar' | 'battery' | 'grid'

export interface SupplyRow {
  key: SourceKey
  watts: number
  percent: number
}

export interface HouseSupply {
  loadW: number
  rows: SupplyRow[]
  surplusW: number
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

export function finite(value: number | null | undefined): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

export interface PowerParts {
  value: string
  unit: string
}

export function powerParts(value: number | null | undefined, locale: string): PowerParts {
  const watts = finite(value)
  if (watts === null) return { value: '—', unit: '' }
  if (Math.abs(watts) >= 1000) {
    return {
      value: new Intl.NumberFormat(locale, {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(watts / 1000),
      unit: 'kW',
    }
  }
  return {
    value: new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(watts),
    unit: 'W',
  }
}

export function formatPower(value: number | null | undefined, locale: string): string {
  const parts = powerParts(value, locale)
  return parts.unit ? `${parts.value} ${parts.unit}` : parts.value
}

export function formatKwh(value: number | null | undefined, locale: string, digits = 1): string {
  const numeric = finite(value)
  if (numeric === null) return '—'
  const shown = numeric !== 0 && Math.abs(numeric) < 1 ? Math.max(digits, 2) : digits
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: shown,
    maximumFractionDigits: shown,
  }).format(numeric)
}

export function formatMoney(value: number | null | undefined, locale: string, digits = 2): string {
  const numeric = finite(value)
  if (numeric === null) return '—'
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(numeric)
}

export function formatPercent(value: number | null | undefined, locale: string, digits = 0): string {
  const numeric = finite(value)
  if (numeric === null) return '—'
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(numeric)} %`
}

/**
 * Share of consumption covered without importing from the grid. Uses the
 * measured daily bucket; returns null until there is consumption to compare.
 */
export function selfPoweredPct(
  bucket: Pick<HistoryBucket, 'load_kwh' | 'ev_kwh' | 'grid_import_kwh'> | null | undefined,
): number | null {
  if (!bucket) return null
  const consumption = (bucket.load_kwh ?? 0) + (bucket.ev_kwh ?? 0)
  if (!Number.isFinite(consumption) || consumption <= 0) return null
  const imported = Math.max(0, bucket.grid_import_kwh ?? 0)
  return clamp((1 - imported / consumption) * 100, 0, 100)
}

/**
 * Instantaneous estimate of what is serving the house. Priority is solar
 * direct, then battery discharge, then grid; the shares always total 100%.
 */
export function houseSupply(sample: TelemetrySample | null | undefined): HouseSupply {
  if (!sample) {
    return {
      loadW: 0,
      rows: [
        { key: 'solar', watts: 0, percent: 0 },
        { key: 'battery', watts: 0, percent: 0 },
        { key: 'grid', watts: 0, percent: 0 },
      ],
      surplusW: 0,
    }
  }
  const load = Math.max(0, sample.load_w || 0)
  const solar = Math.max(0, sample.pv_w || 0)
  const battery = Math.max(0, -(sample.battery_w || 0))
  const solarDirect = Math.min(solar, load)
  const batteryDirect = Math.min(battery, Math.max(0, load - solarDirect))
  const gridDirect = Math.max(0, load - solarDirect - batteryDirect)
  const total = load > 0 ? load : 1
  return {
    loadW: load,
    rows: [
      { key: 'solar', watts: solarDirect, percent: (solarDirect / total) * 100 },
      { key: 'battery', watts: batteryDirect, percent: (batteryDirect / total) * 100 },
      { key: 'grid', watts: gridDirect, percent: (gridDirect / total) * 100 },
    ],
    surplusW: Math.max(0, solar - solarDirect),
  }
}

function partsOf(value: Date, timeZone: string): Record<string, string> {
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const out: Record<string, string> = {}
  for (const part of formatter.formatToParts(value)) {
    if (part.type !== 'literal') out[part.type] = part.value
  }
  return out
}

/** Local calendar date (YYYY-MM-DD) for an instant in the site timezone. */
export function localDateKey(value: string | number | Date, timeZone: string): string {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const parts = partsOf(date, timeZone)
  return `${parts.year}-${parts.month}-${parts.day}`
}

/** Local clock time (HH:MM) for an instant in the site timezone. */
export function localTimeKey(value: string | number | Date, timeZone: string): string {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const parts = partsOf(date, timeZone)
  return `${parts.hour}:${parts.minute}`
}

/** Adds calendar days to a YYYY-MM-DD key without involving the host timezone. */
export function addDays(dateKey: string, days: number): string {
  const [year, month, day] = dateKey.split('-').map(Number)
  if (!year || !month || !day) return ''
  const date = new Date(Date.UTC(year, month - 1, day + days))
  return date.toISOString().slice(0, 10)
}

function tzOffsetMs(date: Date, timeZone: string): number {
  const parts = partsOf(date, timeZone)
  const asUtc = Date.UTC(
    Number(parts.year),
    Number(parts.month) - 1,
    Number(parts.day),
    Number(parts.hour),
    Number(parts.minute),
    Number(parts.second),
  )
  return asUtc - date.getTime()
}

function zonedMidnight(day: string, timeZone: string): Date {
  const [year, month, date] = day.split('-').map(Number)
  const wallClock = Date.UTC(year, month - 1, date)
  let instant = wallClock - tzOffsetMs(new Date(wallClock), timeZone)
  // One extra pass resolves the offset across a DST boundary.
  instant = wallClock - tzOffsetMs(new Date(instant), timeZone)
  return new Date(instant)
}

/** Start/end instants for a local day; day length is 23, 24 or 25 hours. */
export function zonedDayBounds(day: string, timeZone: string): { start: Date; end: Date } {
  const start = zonedMidnight(day, timeZone)
  const end = zonedMidnight(addDays(day, 1), timeZone)
  return { start, end }
}

export interface TimelineSegment {
  left: number
  width: number
}

/** Fractional segment of [dayStart, dayEnd), clipped to the day. */
export function timelineSegment(
  start: string | Date,
  end: string | Date,
  dayStart: Date,
  dayEnd: Date,
): TimelineSegment | null {
  const from = (start instanceof Date ? start : new Date(start)).getTime()
  const to = (end instanceof Date ? end : new Date(end)).getTime()
  const boundsStart = dayStart.getTime()
  const boundsEnd = dayEnd.getTime()
  if (!Number.isFinite(from) || !Number.isFinite(to) || boundsEnd <= boundsStart) return null
  const clippedStart = Math.max(from, boundsStart)
  const clippedEnd = Math.min(to, boundsEnd)
  if (clippedEnd <= clippedStart) return null
  const total = boundsEnd - boundsStart
  return {
    left: (clippedStart - boundsStart) / total,
    width: (clippedEnd - clippedStart) / total,
  }
}

export function timelineFraction(at: string | Date, dayStart: Date, dayEnd: Date): number {
  const value = (at instanceof Date ? at : new Date(at)).getTime()
  const total = dayEnd.getTime() - dayStart.getTime()
  if (!Number.isFinite(value) || total <= 0) return 0
  return clamp((value - dayStart.getTime()) / total, 0, 1)
}

/**
 * Green-to-dark-orange shade for a price normalized against the day's low/high.
 * The cheaper half stays green (lighter to deeper); the expensive half moves
 * through orange into dark orange.
 */
export function priceShade(ratio: number): string {
  const normalized = clamp(Number.isFinite(ratio) ? ratio : 0.5, 0, 1)
  if (normalized <= 0.5) {
    const green = normalized / 0.5
    const lightness = 60 - green * 28
    return `hsl(138 62% ${lightness.toFixed(1)}%)`
  }
  const orange = (normalized - 0.5) / 0.5
  const lightness = 48 - orange * 10
  const hue = 22 - orange * 10
  return `hsl(${hue.toFixed(1)} 92% ${lightness.toFixed(1)}%)`
}
