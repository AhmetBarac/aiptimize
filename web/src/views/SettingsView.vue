<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, ApiError } from '../api/client'
import type { SettingsDocument } from '../api/types'
import { loadSettings, patchSettings, session } from '../session'
import PageHead from '../components/PageHead.vue'

const { t } = useI18n()
const route = useRoute()

const sections = ['site', 'inverter', 'safety', 'system'] as const
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
    </section>
  </template>
</template>
