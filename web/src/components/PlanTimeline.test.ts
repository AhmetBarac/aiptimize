import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { PlanSlot } from '../api/types'
import PlanTimeline from './PlanTimeline.vue'

const slots: PlanSlot[] = [
  {
    day_local: '2026-01-01',
    start: '2026-01-01T09:00:00Z',
    end: '2026-01-01T10:00:00Z',
    direction: 'discharge',
    power_w: 3000,
    energy_kwh: 3,
    avg_import_price: 0.2,
    expected_saving: 1,
  },
  {
    day_local: '2026-01-01',
    start: '2026-01-01T22:00:00Z',
    end: '2026-01-01T23:00:00Z',
    direction: 'charge',
    power_w: 5000,
    energy_kwh: 5,
    avg_import_price: 0.1,
    expected_saving: 2,
  },
]

describe('PlanTimeline', () => {
  it('renders charge and discharge blocks and a now marker', () => {
    const wrapper = mount(PlanTimeline, {
      props: {
        slots,
        day: '2026-01-01',
        timezone: 'UTC',
        locale: 'en',
        now: '2026-01-01T12:00:00Z',
        labels: { charge: 'Charge', discharge: 'Discharge', now: 'Now', empty: 'No slots' },
      },
    })
    expect(wrapper.findAll('.timeline-block.charge')).toHaveLength(1)
    expect(wrapper.findAll('.timeline-block.discharge')).toHaveLength(1)
    expect(wrapper.find('.timeline-now').exists()).toBe(true)
    expect(wrapper.text()).toContain('Discharge 09:00–10:00')
    expect(wrapper.find('.timeline-block.charge').attributes('style')).toContain('width')
  })

  it('shows the empty state when no slot intersects the day', () => {
    const wrapper = mount(PlanTimeline, {
      props: {
        slots: [],
        day: '2026-01-01',
        timezone: 'UTC',
        locale: 'en',
        labels: { charge: 'Charge', discharge: 'Discharge', now: 'Now', empty: 'No slots today' },
      },
    })
    expect(wrapper.text()).toContain('No slots today')
    expect(wrapper.findAll('.timeline-block')).toHaveLength(0)
  })
})
