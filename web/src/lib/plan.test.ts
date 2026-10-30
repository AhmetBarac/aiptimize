import { describe, expect, it } from 'vitest'
import type { Plan, PlanView } from '../api/types'
import { basePlanID, baseRevision } from './plan'

function plan(id: number, revision: number): Plan {
  return { id, revision } as Plan
}

function view(overrides: Partial<PlanView>): PlanView {
  return {
    date: '2026-10-30',
    locked: false,
    validation: {},
    schedule_diff: [],
    inverter_match: true,
    foreign_schedule: false,
    warnings: [],
    ...overrides,
  }
}

describe('plan editor base', () => {
  it('prefers the draft being edited', () => {
    const v = view({ draft: plan(2, 4), applied: plan(1, 3), auto: plan(9, 1) })
    expect(baseRevision(v)).toBe(4)
    expect(basePlanID(v)).toBe(2)
  })

  it('uses the applied plan when there is no draft', () => {
    // Regression: an applied manual plan must be editable; the automatic
    // suggestion's revision used to be sent and every save conflicted.
    const v = view({ applied: plan(17, 3), auto: plan(9, 1) })
    expect(baseRevision(v)).toBe(3)
    expect(basePlanID(v)).toBe(17)
  })

  it('falls back to the automatic suggestion', () => {
    const v = view({ auto: plan(9, 1) })
    expect(baseRevision(v)).toBe(1)
    expect(basePlanID(v)).toBe(9)
  })

  it('returns 0 when no plan is available', () => {
    expect(baseRevision(view({}))).toBe(0)
    expect(basePlanID(view({}))).toBe(0)
    expect(baseRevision(null)).toBe(0)
    expect(basePlanID(undefined)).toBe(0)
  })
})
