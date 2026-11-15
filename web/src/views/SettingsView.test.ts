import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { i18n } from '../i18n'
import { session } from '../session'

// The settings screen fetches its document on mount; stub the client so the test
// controls exactly what comes back.
const settingsDoc = {
  site: {
    name: 'Home',
    timezone: 'UTC',
    latitude: 0,
    longitude: 0,
    max_import_power_w: 11000,
    grid_owner: '',
    facility_id: '',
    grid_tariff_product: '',
    eltariff_url: '',
    tariff_mode: 'manual',
    transfer_fee_ore_kwh: 0,
    energy_tax_ore_kwh: 0,
    variable_cost_ore_kwh: 0,
    fixed_markup_ore_kwh: 0,
    export_benefit_ore_kwh: 0,
    export_extra_ore_kwh: 0,
    show_full_price: false,
  },
  inverter: {
    type: 'saj',
    host: '10.0.0.9',
    port: 502,
    serial: '',
    unit_id: 1,
    max_charge_w: 6000,
    max_discharge_w: 3000,
    capacity_kwh: 15.36,
    min_soc: 10,
    max_soc: 100,
    charge_efficiency: 0.95,
    discharge_efficiency: 0.95,
    cycle_cost: 0.1,
    reserve_soc: 15,
    telemetry_interval_s: 30,
    sign_overrides: { pv: 1, grid: 1, battery: 1 },
    scheme: 'http',
  },
  pv: { strings: [] },
  load: {},
  plan: {},
  ev: { provider: 'zaptec' },
  safety: { writes_armed_ui: false, ev_control_armed_ui: false },
  price: { area: 'SE3', currency: 'SEK' },
}

vi.mock('../api/client', () => ({
  api: {
    get: vi.fn(async (path: string) => {
      if (path === '/settings') return settingsDoc
      if (path === '/users') return { users: [{ id: 1, username: 'ahmet', role: 'admin', created_at: '' }] }
      if (path === '/status') return {}
      return {}
    }),
    post: vi.fn(async () => ({})),
    patch: vi.fn(async () => ({})),
    put: vi.fn(async () => ({})),
    del: vi.fn(async () => ({})),
  },
  ApiError: class extends Error {},
}))

async function mountSettings(section: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/settings/:section?', name: 'settings', component: { template: '<div />' } }],
  })
  await router.push(`/settings/${section}`)
  await router.isReady()
  const { default: SettingsView } = await import('./SettingsView.vue')
  return mount(SettingsView, { global: { plugins: [i18n, router] } })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('SettingsView sections', () => {
  it('renders the system card with the inverter it talks to', async () => {
    session.user = { id: 1, username: 'ahmet', role: 'admin' } as never
    const wrapper = await mountSettings('system')
    await new Promise((resolve) => setTimeout(resolve, 0))
    await wrapper.vm.$nextTick()

    const text = wrapper.text()
    expect(text).toContain('System')
    expect(text).toContain('saj')
    expect(text).toContain('10.0.0.9')
    expect(text).not.toContain('Section not found')
    wrapper.unmount()
  })

  it('renders the users card with the account list and role picker', async () => {
    session.user = { id: 1, username: 'ahmet', role: 'admin' } as never
    const wrapper = await mountSettings('users')
    await new Promise((resolve) => setTimeout(resolve, 0))
    await wrapper.vm.$nextTick()

    const text = wrapper.text()
    expect(text).toContain('ahmet')
    expect(text).toContain('Administrator')
    expect(wrapper.find('#user-role-user').exists()).toBe(true)
    expect(wrapper.find('#user-role-admin').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back to the site section for an unknown section name', async () => {
    session.user = { id: 1, username: 'ahmet', role: 'admin' } as never
    const wrapper = await mountSettings('nonsense')
    await new Promise((resolve) => setTimeout(resolve, 0))
    await wrapper.vm.$nextTick()

    // Unknown names are resolved against the known list, so the first card is
    // shown rather than an empty page.
    expect(wrapper.text()).toContain('Site name')
    wrapper.unmount()
  })
})
