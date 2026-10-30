import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { TelemetrySample } from '../api/types'
import EnergyFlow from './EnergyFlow.vue'

const sample: TelemetrySample = {
  ts: '2026-01-01T12:00:00Z',
  soc: 55,
  pv_w: 1600,
  pv_strings: [800, 800],
  grid_w: 0,
  battery_w: -500,
  load_w: 1100,
  ev_w: 0,
  inverter_mode: 0,
}

const labels = {
  flowTitle: 'Energy flow',
  flowSubtitle: 'Live now',
  solar: 'Solar',
  grid: 'Grid',
  battery: 'Battery',
  house: 'House',
  car: 'Car',
  charging: 'Charging',
  discharging: 'Discharging',
  producing: 'Producing',
  idle: 'Idle',
  importing: 'Importing',
  exporting: 'Exporting',
  connected: 'Connected',
  unplugged: 'Unplugged',
  liveLoad: 'Live load',
  inspectHouse: 'View supply mix',
  noData: 'No data',
}

function mountFlow(evAvailable: boolean) {
  return mount(EnergyFlow, {
    props: {
      sample,
      soc: 55,
      evPowerW: evAvailable ? 4140 : null,
      evAvailable,
      evConnected: evAvailable,
      evCharging: evAvailable,
      stale: false,
      locale: 'en',
      labels,
    },
    global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
  })
}

describe('EnergyFlow', () => {
  it('emits open-house and shows the car only when configured', async () => {
    const wrapper = mountFlow(true)
    expect(wrapper.findAll('.flow-car')).toHaveLength(1)
    await wrapper.find('.flow-house').trigger('click')
    expect(wrapper.emitted('open-house')).toHaveLength(1)
  })

  it('rebalances without an EV and keeps accessible values', () => {
    const wrapper = mountFlow(false)
    expect(wrapper.findAll('.flow-car')).toHaveLength(0)
    expect(wrapper.classes()).toContain('flow-no-ev')
    expect(wrapper.find('.flow-soc').text()).toContain('55')
    expect(wrapper.text()).toContain('1.60 kW')
  })
})
