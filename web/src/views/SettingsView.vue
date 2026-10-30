<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, ApiError } from '../api/client'
import type { PVStringSettings, SettingsDocument } from '../api/types'
import { loadSettings, patchSettings, session } from '../session'
import PageHead from '../components/PageHead.vue'

const { t } = useI18n()
const route = useRoute()

const sections = ['site', 'inverter', 'solar', 'load', 'plan', 'ev', 'safety', 'system'] as const
const section = computed(() => {
  const value = String(route.params.section ?? 'site')
  return (sections as readonly string[]).includes(value) ? value : 'site'
})

const settings = ref<SettingsDocument | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref('')

const siteForm = reactive({
  name: '',
  timezone: '',
  latitude: 0,
  longitude: 0,
  maxImport: 0,
})

const inverterForm = reactive({
  host: '',
  scheme: 'http',
  port: 8484,
  serial: '',
  maxCharge: 6000,
  maxDischarge: 3000,
  capacity: 15.36,
  minSoc: 15,
  maxSoc: 100,
  reserveSoc: 15,
  interval: 30,
  chargeEfficiency: 0.95,
  dischargeEfficiency: 0.95,
  signPv: 1,
  signGrid: 1,
  signBattery: 1,
})

interface InverterTestResult {
  ok: boolean
  identity: {
    serial: string
    model: string
    firmware: string
    rated_power_w: number
    capacity_kwh: number
    work_mode: number
  }
  telemetry: {
    soc: number
    pv_w: number
    grid_w: number
    battery_w: number
    load_w: number
    ts: string
  }
}

const testing = ref(false)
const testResult = ref<InverterTestResult | null>(null)
const testError = ref('')

const safetyForm = reactive({
  writes: false,
  ev: false,
  watchdog: 1800,
  horizon: 2,
})

const pvForm = reactive({
  strings: [] as PVStringSettings[],
  acLimit: 8000,
  confidence: 0.7,
  refresh: 30,
  calibration: false,
})

const loadForm = reactive({
  dailyKwh: 12,
  useMeasured: false,
  profile: [] as number[],
})

const planForm = reactive({
  maxSlots: 4,
  maxSlotMin: 240,
  horizonDays: 2,
  minDailySaving: 1,
  preserveSoc: true,
  endSoc: 50,
  chargeW: 6000,
  dischargeW: 3000,
  allowGrid: true,
})

const evForm = reactive({
  mode: 'off',
  minCurrent: 6,
  maxCurrent: 16,
  phases: 3,
  voltage: 230,
  solarShare: 1,
  gridChargeAllowed: false,
  alwaysCharge: 'off',
  batteryFloor: 20,
  smartCostLimit: '' as number | '',
  feedInLimit: '' as number | '',
})

onMounted(load)

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const doc = await loadSettings()
    settings.value = doc
    siteForm.name = doc.site.name
    siteForm.timezone = doc.site.timezone
    siteForm.latitude = doc.site.latitude
    siteForm.longitude = doc.site.longitude
    siteForm.maxImport = doc.site.max_import_power_w
    inverterForm.host = doc.inverter.host
    inverterForm.scheme = doc.inverter.scheme
    inverterForm.port = doc.inverter.port
    inverterForm.serial = doc.inverter.serial
    inverterForm.maxCharge = doc.inverter.max_charge_w
    inverterForm.maxDischarge = doc.inverter.max_discharge_w
    inverterForm.capacity = doc.inverter.capacity_kwh
    inverterForm.minSoc = doc.inverter.min_soc
    inverterForm.maxSoc = doc.inverter.max_soc
    inverterForm.reserveSoc = doc.inverter.reserve_soc
    inverterForm.interval = doc.inverter.telemetry_interval_s
    inverterForm.chargeEfficiency = doc.inverter.charge_efficiency
    inverterForm.dischargeEfficiency = doc.inverter.discharge_efficiency
    inverterForm.signPv = doc.inverter.sign_overrides.pv
    inverterForm.signGrid = doc.inverter.sign_overrides.grid
    inverterForm.signBattery = doc.inverter.sign_overrides.battery
    pvForm.strings = doc.pv.strings.map((str) => ({ ...str }))
    pvForm.acLimit = doc.pv.inverter_ac_limit_w
    pvForm.confidence = doc.pv.confidence
    pvForm.refresh = doc.pv.refresh_minutes
    pvForm.calibration = doc.pv.calibration_enabled
    loadForm.dailyKwh = doc.load.daily_kwh
    loadForm.useMeasured = doc.load.use_measured_profile
    loadForm.profile = [...doc.load.profile]
    planForm.maxSlots = doc.plan.max_slots_per_day
    planForm.maxSlotMin = doc.plan.max_slot_min
    planForm.horizonDays = doc.plan.horizon_days
    planForm.minDailySaving = doc.plan.min_daily_saving
    planForm.preserveSoc = doc.plan.preserve_soc
    planForm.endSoc = doc.plan.end_soc_target
    planForm.chargeW = doc.plan.charge_w
    planForm.dischargeW = doc.plan.discharge_w
    planForm.allowGrid = doc.plan.allow_discharge_to_grid
    evForm.mode = doc.ev.mode
    evForm.minCurrent = doc.ev.min_current_a
    evForm.maxCurrent = doc.ev.max_current_a
    evForm.phases = doc.ev.phases
    evForm.voltage = doc.ev.voltage_v
    evForm.solarShare = doc.ev.solar_share
    evForm.gridChargeAllowed = doc.ev.grid_charge_allowed
    evForm.alwaysCharge = doc.ev.always_charge
    evForm.batteryFloor = doc.ev.house_battery_floor_soc
    evForm.smartCostLimit = doc.ev.smart_cost_limit_sek_kwh ?? ''
    evForm.feedInLimit = doc.ev.smart_feed_in_priority_limit_sek_kwh ?? ''
    safetyForm.writes = doc.safety.writes_armed_ui
    safetyForm.ev = doc.safety.ev_control_armed_ui
    safetyForm.watchdog = doc.safety.watchdog_clear_after_s
    safetyForm.horizon = doc.safety.horizon_limit_days
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.loadFailed')
  } finally {
    loading.value = false
  }
}

async function saveSite(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('site', {
      name: siteForm.name,
      timezone: siteForm.timezone,
      latitude: Number(siteForm.latitude),
      longitude: Number(siteForm.longitude),
      max_import_power_w: Number(siteForm.maxImport),
    })
    saved.value = t('settings.siteSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveSafety(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('safety', {
      writes_armed_ui: safetyForm.writes,
      ev_control_armed_ui: safetyForm.ev,
      watchdog_clear_after_s: Number(safetyForm.watchdog),
      horizon_limit_days: Number(safetyForm.horizon),
    })
    saved.value = t('settings.safetySaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveInverter(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  testResult.value = null
  testError.value = ''
  try {
    await patchSettings('inverter', {
      host: inverterForm.host.trim(),
      scheme: inverterForm.scheme,
      port: Number(inverterForm.port),
      serial: inverterForm.serial.trim(),
      max_charge_w: Number(inverterForm.maxCharge),
      max_discharge_w: Number(inverterForm.maxDischarge),
      capacity_kwh: Number(inverterForm.capacity),
      min_soc: Number(inverterForm.minSoc),
      max_soc: Number(inverterForm.maxSoc),
      reserve_soc: Number(inverterForm.reserveSoc),
      telemetry_interval_s: Number(inverterForm.interval),
      charge_efficiency: Number(inverterForm.chargeEfficiency),
      discharge_efficiency: Number(inverterForm.dischargeEfficiency),
      sign_overrides: {
        pv: Number(inverterForm.signPv),
        grid: Number(inverterForm.signGrid),
        battery: Number(inverterForm.signBattery),
      },
    })
    saved.value = t('settings.inverterSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function testInverter(): Promise<void> {
  testing.value = true
  testError.value = ''
  testResult.value = null
  try {
    testResult.value = await api.post<InverterTestResult>('/settings/test/inverter', {})
  } catch (err) {
    if (err instanceof ApiError) {
      const details = err.details as { error?: string } | undefined
      testError.value = details?.error ?? err.message
    } else {
      testError.value = t('settings.testFailed')
    }
  } finally {
    testing.value = false
  }
}

function onOff(value: boolean | undefined): string {
  return value ? t('settings.on') : t('settings.off')
}

function addString(): void {
  if (pvForm.strings.length >= 3) return
  pvForm.strings.push({
    name: `MPPT ${pvForm.strings.length + 1}`,
    kwp: 4,
    tilt: 35,
    azimuth: 180,
    performance_ratio: 0.85,
    temp_coefficient: -0.004,
    max_w: 0,
  })
}

function removeString(index: number): void {
  pvForm.strings.splice(index, 1)
}

async function saveSolar(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('pv', {
      strings: pvForm.strings.map((str) => ({
        name: str.name.trim() || 'MPPT',
        kwp: Number(str.kwp),
        tilt: Number(str.tilt),
        azimuth: Number(str.azimuth),
        performance_ratio: Number(str.performance_ratio),
        temp_coefficient: Number(str.temp_coefficient),
        max_w: Number(str.max_w),
      })),
      inverter_ac_limit_w: Number(pvForm.acLimit),
      confidence: Number(pvForm.confidence),
      refresh_minutes: Number(pvForm.refresh),
      calibration_enabled: pvForm.calibration,
    })
    saved.value = t('settings.solarSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveLoad(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    const sum = loadForm.profile.reduce((a, b) => a + b, 0)
    const profile = sum > 0 ? loadForm.profile.map((v) => v / sum) : loadForm.profile
    await patchSettings('load', {
      daily_kwh: Number(loadForm.dailyKwh),
      profile,
      use_measured_profile: loadForm.useMeasured,
    })
    saved.value = t('settings.loadSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function savePlan(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('plan', {
      max_slots_per_day: Number(planForm.maxSlots),
      max_slot_min: Number(planForm.maxSlotMin),
      horizon_days: Number(planForm.horizonDays),
      min_daily_saving: Number(planForm.minDailySaving),
      preserve_soc: planForm.preserveSoc,
      end_soc_target: Number(planForm.endSoc),
      charge_w: Number(planForm.chargeW),
      discharge_w: Number(planForm.dischargeW),
      allow_discharge_to_grid: planForm.allowGrid,
    })
    saved.value = t('settings.planSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveEV(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('ev', {
      mode: evForm.mode,
      min_current_a: Number(evForm.minCurrent),
      max_current_a: Number(evForm.maxCurrent),
      phases: Number(evForm.phases),
      voltage_v: Number(evForm.voltage),
      solar_share: Number(evForm.solarShare),
      grid_charge_allowed: evForm.gridChargeAllowed,
      always_charge: evForm.alwaysCharge,
      house_battery_floor_soc: Number(evForm.batteryFloor),
      smart_cost_limit_sek_kwh: evForm.smartCostLimit === '' ? null : Number(evForm.smartCostLimit),
      smart_feed_in_priority_limit_sek_kwh: evForm.feedInLimit === '' ? null : Number(evForm.feedInLimit),
    })
    saved.value = t('settings.evSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function exportSettings(): Promise<void> {
  error.value = ''
  try {
    const data = await api.get<Record<string, unknown>>('/settings/export')
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = 'aiptimize-settings.json'
    link.click()
    URL.revokeObjectURL(link.href)
    saved.value = t('settings.exported')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settings.exportFailed')
  }
}

async function importSettings(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  saving.value = true
  error.value = ''
  try {
    const text = await file.text()
    const parsed = JSON.parse(text) as Record<string, unknown>
    await api.post('/settings/import', parsed)
    saved.value = t('settings.imported')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settings.importFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageHead :title="t('settings.title')" :subtitle="t('settings.subtitle')" />

  <nav class="settings-nav" aria-label="Settings sections">
    <router-link
      v-for="item in sections"
      :key="item"
      :to="`/settings/${item}`"
      :class="{ active: section === item }"
    >
      {{ t(`settings.sections.${item}`) }}
    </router-link>
  </nav>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="saved" class="alert alert-success" role="status">{{ saved }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="settings">
    <section v-if="section === 'site'" class="aipt-card">
      <h2>{{ t('settings.sections.site') }}</h2>
      <form novalidate @submit.prevent="saveSite">
        <div class="row g-3">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="name">{{ t('settings.siteName') }}</label>
            <input id="name" v-model="siteForm.name" class="form-control" type="text" required />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="tz">{{ t('settings.timezone') }}</label>
            <input id="tz" v-model="siteForm.timezone" class="form-control" type="text" required />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lat">{{ t('settings.latitude') }}</label>
            <input id="lat" v-model.number="siteForm.latitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lon">{{ t('settings.longitude') }}</label>
            <input id="lon" v-model.number="siteForm.longitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="max-import">{{ t('settings.maxImport') }}</label>
            <input
              id="max-import"
              v-model.number="siteForm.maxImport"
              class="form-control"
              type="number"
              min="1"
              step="100"
            />
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSite') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'inverter'" class="aipt-card">
      <h2>{{ t('settings.sections.inverter') }}</h2>
      <p class="text-muted small">{{ t('settings.inverterIntro') }}</p>
      <form novalidate @submit.prevent="saveInverter">
        <div class="row g-3">
          <div class="col-12 col-sm-5">
            <label class="form-label" for="inv-host">{{ t('settings.host') }}</label>
            <input id="inv-host" v-model="inverterForm.host" class="form-control mono" type="text" placeholder="192.0.2.10" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-scheme">{{ t('settings.scheme') }}</label>
            <select id="inv-scheme" v-model="inverterForm.scheme" class="form-select">
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </div>
          <div class="col-6 col-sm-4">
            <label class="form-label" for="inv-port">{{ t('settings.port') }}</label>
            <input id="inv-port" v-model.number="inverterForm.port" class="form-control" type="number" min="1" max="65535" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="inv-serial">{{ t('settings.serial') }}</label>
            <input id="inv-serial" v-model="inverterForm.serial" class="form-control mono" type="text" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-capacity">{{ t('settings.batteryCapacity') }}</label>
            <input id="inv-capacity" v-model.number="inverterForm.capacity" class="form-control" type="number" step="0.01" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-interval">{{ t('settings.telemetryInterval') }}</label>
            <input id="inv-interval" v-model.number="inverterForm.interval" class="form-control" type="number" min="5" step="5" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-max-charge">{{ t('settings.maxCharge') }}</label>
            <input id="inv-max-charge" v-model.number="inverterForm.maxCharge" class="form-control" type="number" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-max-discharge">{{ t('settings.maxDischarge') }}</label>
            <input id="inv-max-discharge" v-model.number="inverterForm.maxDischarge" class="form-control" type="number" step="100" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="inv-min-soc">{{ t('settings.minSoc') }}</label>
            <input id="inv-min-soc" v-model.number="inverterForm.minSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="inv-max-soc">{{ t('settings.maxSoc') }}</label>
            <input id="inv-max-soc" v-model.number="inverterForm.maxSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="inv-reserve-soc">{{ t('settings.reserveSoc') }}</label>
            <input id="inv-reserve-soc" v-model.number="inverterForm.reserveSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-charge-eff">{{ t('settings.chargeEfficiency') }}</label>
            <input id="inv-charge-eff" v-model.number="inverterForm.chargeEfficiency" class="form-control" type="number" step="0.01" min="0.5" max="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-discharge-eff">{{ t('settings.dischargeEfficiency') }}</label>
            <input id="inv-discharge-eff" v-model.number="inverterForm.dischargeEfficiency" class="form-control" type="number" step="0.01" min="0.5" max="1" />
          </div>
        </div>

        <h2 class="mt-4">{{ t('settings.signOverrides') }}</h2>
        <p class="text-muted small mb-2">{{ t('settings.signHelp') }}</p>
        <div class="row g-3">
          <div class="col-4">
            <label class="form-label" for="sign-pv">{{ t('settings.signPv') }}</label>
            <select id="sign-pv" v-model.number="inverterForm.signPv" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
          <div class="col-4">
            <label class="form-label" for="sign-grid">{{ t('settings.signGrid') }}</label>
            <select id="sign-grid" v-model.number="inverterForm.signGrid" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
          <div class="col-4">
            <label class="form-label" for="sign-battery">{{ t('settings.signBattery') }}</label>
            <select id="sign-battery" v-model.number="inverterForm.signBattery" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
        </div>

        <div class="d-flex flex-wrap gap-2 mt-4">
          <button class="btn btn-primary" type="submit" :disabled="saving">
            <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ saving ? t('common.saving') : t('settings.saveInverter') }}
          </button>
          <button class="btn btn-outline-secondary" type="button" :disabled="testing" @click="testInverter">
            <span v-if="testing" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ testing ? t('settings.testing') : t('settings.testConnection') }}
          </button>
        </div>
        <p class="text-muted small mt-2 mb-0">{{ t('settings.testHint') }}</p>
      </form>

      <div v-if="testError" class="alert alert-danger mt-3 small" role="alert">{{ testError }}</div>
      <div v-if="testResult" class="alert alert-success mt-3 small" role="status">
        <div class="fw-semibold mb-2">
          <i class="bi bi-check-circle me-1" aria-hidden="true"></i>
          {{ t('settings.testOk') }} — {{ testResult.identity.model }}
        </div>
        <dl class="mb-0">
          <div class="key-value">
            <dt>{{ t('settings.serial') }}</dt>
            <dd class="mono">{{ testResult.identity.serial }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.firmware') }}</dt>
            <dd class="mono">{{ testResult.identity.firmware }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.ratedPower') }}</dt>
            <dd>{{ testResult.identity.rated_power_w }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.soc') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.soc) }} %</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.solar') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.pv_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.grid') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.grid_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.battery') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.battery_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.house') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.load_w) }} W</dd>
          </div>
        </dl>
      </div>
    </section>

    <section v-else-if="section === 'solar'" class="aipt-card">
      <h2>{{ t('settings.sections.solar') }}</h2>
      <p class="text-muted small">{{ t('settings.solarIntro') }}</p>
      <form novalidate @submit.prevent="saveSolar">
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-ac">{{ t('settings.acLimit') }}</label>
            <input id="pv-ac" v-model.number="pvForm.acLimit" class="form-control" type="number" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-conf">{{ t('settings.confidence') }}</label>
            <input id="pv-conf" v-model.number="pvForm.confidence" class="form-control" type="number" step="0.05" min="0" max="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-refresh">{{ t('settings.refreshMinutes') }}</label>
            <input id="pv-refresh" v-model.number="pvForm.refresh" class="form-control" type="number" min="5" step="5" />
          </div>
          <div class="col-12 col-sm-3 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="pv-cal" v-model="pvForm.calibration" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="pv-cal">{{ t('settings.calibrationEnabled') }}</label>
            </div>
          </div>
        </div>

        <div v-for="(str, index) in pvForm.strings" :key="index" class="string-card">
          <div class="d-flex align-items-center gap-2 mb-3">
            <input
              v-model="str.name"
              class="form-control"
              type="text"
              :aria-label="t('settings.stringName')"
              :placeholder="t('settings.stringName')"
            />
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="removeString(index)">
              <i class="bi bi-trash" aria-hidden="true"></i>
              <span class="d-none d-sm-inline ms-1">{{ t('settings.removeString') }}</span>
            </button>
          </div>
          <div class="row g-3">
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`kwp-${index}`">{{ t('settings.kwp') }}</label>
              <input :id="`kwp-${index}`" v-model.number="str.kwp" class="form-control" type="number" step="0.1" min="0" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`tilt-${index}`">{{ t('settings.tilt') }}</label>
              <input :id="`tilt-${index}`" v-model.number="str.tilt" class="form-control" type="number" min="0" max="90" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`azimuth-${index}`">{{ t('settings.azimuth') }}</label>
              <input :id="`azimuth-${index}`" v-model.number="str.azimuth" class="form-control" type="number" min="0" max="359" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`pr-${index}`">{{ t('settings.performanceRatio') }}</label>
              <input :id="`pr-${index}`" v-model.number="str.performance_ratio" class="form-control" type="number" step="0.01" min="0" max="1.5" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`temp-${index}`">{{ t('settings.tempCoefficient') }}</label>
              <input :id="`temp-${index}`" v-model.number="str.temp_coefficient" class="form-control" type="number" step="0.001" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`max-${index}`">{{ t('settings.maxW') }}</label>
              <input :id="`max-${index}`" v-model.number="str.max_w" class="form-control" type="number" step="100" min="0" />
            </div>
          </div>
        </div>

        <button
          v-if="pvForm.strings.length < 3"
          class="btn btn-outline-secondary mt-3"
          type="button"
          @click="addString"
        >
          <i class="bi bi-plus-lg" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.addString') }}</span>
        </button>

        <div class="mt-4">
          <button class="btn btn-primary" type="submit" :disabled="saving">
            <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ saving ? t('common.saving') : t('settings.saveSolar') }}
          </button>
        </div>
      </form>
    </section>

    <section v-else-if="section === 'load'" class="aipt-card">
      <h2>{{ t('settings.sections.load') }}</h2>
      <p class="text-muted small">{{ t('settings.loadIntro') }}</p>
      <form novalidate @submit.prevent="saveLoad">
        <div class="row g-3">
          <div class="col-6 col-sm-4">
            <label class="form-label" for="load-kwh">{{ t('settings.dailyKwh') }}</label>
            <input id="load-kwh" v-model.number="loadForm.dailyKwh" class="form-control" type="number" min="0" step="0.5" />
          </div>
          <div class="col-12 col-sm-8 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="load-measured" v-model="loadForm.useMeasured" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="load-measured">{{ t('settings.useMeasured') }}</label>
            </div>
          </div>
        </div>

        <details class="mt-4">
          <summary class="fw-semibold">{{ t('settings.hourlyProfile') }}</summary>
          <p class="text-muted small mt-2">{{ t('settings.profileHelp') }}</p>
          <div class="row g-2">
            <div v-for="(value, hour) in loadForm.profile" :key="hour" class="col-4 col-sm-2">
              <label class="form-label" :for="`profile-${hour}`">{{ hour }}:00</label>
              <input
                :id="`profile-${hour}`"
                v-model.number="loadForm.profile[hour]"
                class="form-control"
                type="number"
                step="0.001"
                min="0"
              />
            </div>
          </div>
        </details>

        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveLoad') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'plan'" class="aipt-card">
      <h2>{{ t('settings.sections.plan') }}</h2>
      <p class="text-muted small">{{ t('settings.planIntro') }}</p>
      <form novalidate @submit.prevent="savePlan">
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-slots">{{ t('settings.maxSlots') }}</label>
            <input id="plan-slots" v-model.number="planForm.maxSlots" class="form-control" type="number" min="1" max="6" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-min">{{ t('settings.maxSlotMin') }}</label>
            <input id="plan-min" v-model.number="planForm.maxSlotMin" class="form-control" type="number" min="30" max="240" step="30" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-days">{{ t('settings.horizonDays') }}</label>
            <input id="plan-days" v-model.number="planForm.horizonDays" class="form-control" type="number" min="1" max="2" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-saving">{{ t('settings.minDailySaving') }}</label>
            <input id="plan-saving" v-model.number="planForm.minDailySaving" class="form-control" type="number" min="0" step="0.1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-charge">{{ t('settings.planChargeW') }}</label>
            <input id="plan-charge" v-model.number="planForm.chargeW" class="form-control" type="number" min="1" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-discharge">{{ t('settings.planDischargeW') }}</label>
            <input id="plan-discharge" v-model.number="planForm.dischargeW" class="form-control" type="number" min="1" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-end">{{ t('settings.endSoc') }}</label>
            <input id="plan-end" v-model.number="planForm.endSoc" class="form-control" type="number" min="0" max="100" step="1" />
          </div>
          <div class="col-12 col-sm-3 d-flex flex-column justify-content-end">
            <div class="form-check form-switch mb-2">
              <input id="plan-preserve" v-model="planForm.preserveSoc" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="plan-preserve">{{ t('settings.preserveSoc') }}</label>
            </div>
            <div class="form-check form-switch mb-2">
              <input id="plan-grid" v-model="planForm.allowGrid" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="plan-grid">{{ t('settings.allowGridDischarge') }}</label>
            </div>
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.savePlan') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'ev'" class="aipt-card">
      <h2>{{ t('settings.sections.ev') }}</h2>
      <p class="text-muted small">{{ t('settings.evIntro') }}</p>
      <form novalidate @submit.prevent="saveEV">
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-mode">{{ t('settings.evMode') }}</label>
            <select id="ev-mode" v-model="evForm.mode" class="form-select">
              <option value="off">{{ t('car.modes.off') }}</option>
              <option value="now">{{ t('car.modes.now') }}</option>
              <option value="smart">{{ t('car.modes.smart') }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-min">{{ t('settings.evMinCurrent') }}</label>
            <input id="ev-min" v-model.number="evForm.minCurrent" class="form-control" type="number" min="6" max="32" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-max">{{ t('settings.evMaxCurrent') }}</label>
            <input id="ev-max" v-model.number="evForm.maxCurrent" class="form-control" type="number" min="6" max="32" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-phases">{{ t('settings.evPhases') }}</label>
            <input id="ev-phases" v-model.number="evForm.phases" class="form-control" type="number" min="1" max="3" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-voltage">{{ t('settings.evVoltage') }}</label>
            <input id="ev-voltage" v-model.number="evForm.voltage" class="form-control" type="number" min="100" max="300" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-solar">{{ t('settings.evSolarShare') }}</label>
            <input id="ev-solar" v-model.number="evForm.solarShare" class="form-control" type="number" min="0" max="1" step="0.05" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-floor">{{ t('settings.evBatteryFloor') }}</label>
            <input id="ev-floor" v-model.number="evForm.batteryFloor" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-always">{{ t('settings.evAlwaysCharge') }}</label>
            <select id="ev-always" v-model="evForm.alwaysCharge" class="form-select">
              <option value="off">off</option>
              <option value="on">on</option>
              <option value="once">once</option>
            </select>
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-cost">{{ t('settings.evSmartCost') }}</label>
            <input id="ev-cost" v-model.number="evForm.smartCostLimit" class="form-control" type="number" step="0.01" placeholder="—" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-feed">{{ t('settings.evFeedIn') }}</label>
            <input id="ev-feed" v-model.number="evForm.feedInLimit" class="form-control" type="number" step="0.01" placeholder="—" />
          </div>
          <div class="col-12 col-sm-6 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="ev-grid" v-model="evForm.gridChargeAllowed" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="ev-grid">{{ t('settings.evGridAllowed') }}</label>
            </div>
          </div>
        </div>
        <p class="small text-muted mt-3">{{ t('settings.evGridWarning') }}</p>
        <button class="btn btn-primary mt-2" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveEV') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'safety'" class="aipt-card">
      <h2>{{ t('settings.sections.safety') }}</h2>
      <p class="text-muted small">{{ t('settings.safetyIntro') }}</p>
      <form novalidate @submit.prevent="saveSafety">
        <div class="form-check form-switch py-2">
          <input id="writes" v-model="safetyForm.writes" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="writes">{{ t('settings.writesArm') }}</label>
        </div>
        <div class="form-check form-switch py-2">
          <input id="ev" v-model="safetyForm.ev" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="ev">{{ t('settings.evArm') }}</label>
        </div>
        <div class="row g-3 mt-2">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="watchdog">{{ t('settings.watchdog') }}</label>
            <input id="watchdog" v-model.number="safetyForm.watchdog" class="form-control" type="number" min="60" step="60" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="horizon">{{ t('settings.horizon') }}</label>
            <input id="horizon" v-model.number="safetyForm.horizon" class="form-control" type="number" min="1" max="2" />
          </div>
        </div>
        <div class="mt-3 small text-muted">
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.writes') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.evControl') }}</dt>
            <dd>{{ onOff(session.meta?.gates.ev_control_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.effective') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_armed) }}</dd>
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSafety') }}
        </button>
      </form>
    </section>

    <section v-else class="aipt-card">
      <h2>{{ t('settings.sections.system') }}</h2>
      <p class="text-muted small">{{ t('settings.systemIntro') }}</p>
      <dl class="mb-0">
        <div class="key-value">
          <dt>{{ t('common.version') }}</dt>
          <dd class="mono">{{ session.meta?.version ?? '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.buildCommit') }}</dt>
          <dd class="mono">{{ session.meta?.commit || '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.buildDate') }}</dt>
          <dd class="mono">{{ session.meta?.build_date || '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.inverter') }}</dt>
          <dd>{{ settings.inverter.type }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.host') }}</dt>
          <dd class="mono">
            {{ settings.inverter.host || t('settings.notConfigured') }}:{{ settings.inverter.port }}
          </dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.batteryCapacity') }}</dt>
          <dd>{{ settings.inverter.capacity_kwh }} kWh</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxCharge') }}</dt>
          <dd>{{ settings.inverter.max_charge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxDischarge') }}</dt>
          <dd>{{ settings.inverter.max_discharge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.area') }}</dt>
          <dd>{{ settings.price.area }} · {{ settings.price.currency }}</dd>
        </div>
      </dl>
      <div class="d-flex flex-wrap gap-2 mt-4">
        <button class="btn btn-outline-secondary" type="button" @click="exportSettings">
          <i class="bi bi-download" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.export') }}</span>
        </button>
        <label class="btn btn-outline-secondary mb-0">
          <i class="bi bi-upload" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.import') }}</span>
          <input class="d-none" type="file" accept="application/json" @change="importSettings" />
        </label>
      </div>
    </section>
  </template>
</template>
