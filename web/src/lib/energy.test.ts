import { describe, expect, it } from 'vitest'
import type { HistoryBucket, TelemetrySample } from '../api/types'
import {
  addDays,
  formatKwh,
  houseSupply,
  localDateKey,
  powerParts,
  selfPoweredPct,
  timelineFraction,
  timelineSegment,
  zonedDayBounds,
} from './energy'

function sample(overrides: Partial<TelemetrySample>): TelemetrySample {
  return {
    ts: '2026-01-01T12:00:00Z',
    soc: 50,
    pv_w: 0,
    pv_strings: [],
    grid_w: 0,
    battery_w: 0,
    load_w: 0,
    ev_w: 0,
    inverter_mode: 0,
    ...overrides,
  }
}

function bucket(overrides: Partial<HistoryBucket>): HistoryBucket {
  return {
    label: '2026-01-01',
    start: '2026-01-01T00:00:00Z',
    end: '2026-01-02T00:00:00Z',
    pv_kwh: 0,
    load_kwh: 0,
    ev_kwh: 0,
    grid_import_kwh: 0,
    grid_export_kwh: 0,
    battery_charge_kwh: 0,
    battery_discharge_kwh: 0,
    plan_battery_kwh: 0,
    cost: 0,
    baseline_cost: 0,
    savings: 0,
    ...overrides,
  }
}

const day = { start: new Date('2026-01-01T00:00:00Z'), end: new Date('2026-01-02T00:00:00Z') }

describe('powerParts', () => {
  it('uses kW above 1000 W and whole watts below', () => {
    expect(powerParts(4140, 'en')).toEqual({ value: '4.14', unit: 'kW' })
    expect(powerParts(999, 'en')).toEqual({ value: '999', unit: 'W' })
    expect(powerParts(null, 'en')).toEqual({ value: '—', unit: '' })
  })
})

describe('formatKwh', () => {
  it('keeps two decimals for small non-zero values', () => {
    expect(formatKwh(0.04, 'en')).toBe('0.04')
    expect(formatKwh(3.2, 'en')).toBe('3.2')
    expect(formatKwh(null, 'en')).toBe('—')
  })
})

describe('selfPoweredPct', () => {
  it('computes the non-imported share of consumption', () => {
    expect(selfPoweredPct(bucket({ load_kwh: 10, ev_kwh: 2, grid_import_kwh: 3 }))).toBe(75)
  })

  it('clamps and handles missing values', () => {
    expect(selfPoweredPct(bucket({ load_kwh: 1, grid_import_kwh: 4 }))).toBe(0)
    expect(selfPoweredPct(bucket({ load_kwh: 0 }))).toBeNull()
    expect(selfPoweredPct(null)).toBeNull()
  })
})

describe('houseSupply', () => {
  it('prioritises solar, then battery, then grid', () => {
    const result = houseSupply(
      sample({ pv_w: 1000, load_w: 3000, battery_w: -1000, grid_w: 1000 }),
    )
    expect(result.loadW).toBe(3000)
    expect(result.rows.map((row) => row.watts)).toEqual([1000, 1000, 1000])
    expect(result.rows.reduce((sum, row) => sum + row.percent, 0)).toBeCloseTo(100)
    expect(result.surplusW).toBe(0)
  })

  it('reports surplus solar and handles a missing sample', () => {
    const surplus = houseSupply(sample({ pv_w: 5000, load_w: 1000, battery_w: -500, grid_w: -3500 }))
    expect(surplus.rows[0]).toMatchObject({ key: 'solar', watts: 1000, percent: 100 })
    expect(surplus.surplusW).toBe(4000)
    expect(houseSupply(null).rows.every((row) => row.watts === 0)).toBe(true)
  })
})

describe('timezone helpers', () => {
  it('maps instants to local dates', () => {
    expect(localDateKey('2026-01-01T00:30:00Z', 'Europe/Stockholm')).toBe('2026-01-01')
    expect(localDateKey('2026-01-01T23:30:00Z', 'Europe/Stockholm')).toBe('2026-01-02')
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2026-03-01', -1)).toBe('2026-02-28')
  })

  it('returns 23 and 25 hour days across DST', () => {
    const spring = zonedDayBounds('2026-03-29', 'Europe/Stockholm')
    expect((spring.end.getTime() - spring.start.getTime()) / 3_600_000).toBe(23)
    const autumn = zonedDayBounds('2026-10-25', 'Europe/Stockholm')
    expect((autumn.end.getTime() - autumn.start.getTime()) / 3_600_000).toBe(25)
  })
})

describe('timeline helpers', () => {
  it('places and clips segments within a day', () => {
    expect(timelineSegment('2026-01-01T12:00:00Z', '2026-01-01T18:00:00Z', day.start, day.end)).toEqual({
      left: 0.5,
      width: 0.25,
    })
    const overnight = timelineSegment(
      '2026-01-01T23:00:00Z',
      '2026-01-02T01:00:00Z',
      day.start,
      day.end,
    )
    expect(overnight?.left).toBeCloseTo(23 / 24)
    expect(overnight?.width).toBeCloseTo(1 / 24)
    expect(
      timelineSegment('2025-12-31T20:00:00Z', '2026-01-01T02:00:00Z', day.start, day.end),
    ).toEqual({ left: 0, width: 2 / 24 })
    expect(timelineSegment('2026-01-03T00:00:00Z', '2026-01-03T01:00:00Z', day.start, day.end)).toBeNull()
  })

  it('returns a clamped fraction for a marker', () => {
    expect(timelineFraction('2026-01-01T12:00:00Z', day.start, day.end)).toBe(0.5)
    expect(timelineFraction('2025-12-31T12:00:00Z', day.start, day.end)).toBe(0)
    expect(timelineFraction('2026-01-02T12:00:00Z', day.start, day.end)).toBe(1)
  })
})
