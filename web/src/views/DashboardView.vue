<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { Status, TelemetrySample } from '../api/types'
import { session } from '../session'
import PageHead from '../components/PageHead.vue'

const { t, locale } = useI18n()
const status = ref<Status | null>(null)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined

async function load(): Promise<void> {
  try {
    status.value = await api.get<Status>('/status')
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void load(), 10_000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})

const site = computed<{ name: string; area?: string; timezone?: string; currency?: string } | null>(
  () => status.value?.site ?? session.meta?.site ?? null,
)
const gates = computed(() => status.value?.gates ?? session.meta?.gates ?? null)
const armed = computed(() => gates.value?.writes_armed === true)
const version = computed(() => status.value?.version ?? session.meta?.version ?? '—')

const telemetry = computed(() => status.value?.telemetry ?? null)
const sample = computed<TelemetrySample | null>(() => telemetry.value?.sample ?? null)
const hasData = computed(() => telemetry.value?.available === true && sample.value !== null)
const stale = computed(() => telemetry.value?.stale === true)
const age = computed(() => telemetry.value?.age_s ?? null)

const siteLine = computed(() => {
  if (!site.value) return ''
  const parts = [site.value.name]
  if (site.value.area) parts.push(site.value.area)
  if (site.value.timezone) parts.push(site.value.timezone)
  return parts.filter(Boolean).join(' · ')
})

const uptime = computed(() => {
  const seconds = status.value?.uptime_s ?? 0
  if (!seconds) return '—'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`
})

const database = computed(() => status.value?.checks.database ?? 'ok')
const inverterCheck = computed(() => status.value?.checks.inverter ?? 'unknown')
const prices = computed(() => status.value?.prices ?? null)
const currency = computed(() => prices.value?.currency ?? site.value?.currency ?? '')
const priceNow = computed(() => prices.value?.now_import ?? null)
const priceNext = computed(() => prices.value?.next_import ?? null)
const cheapestWindow = computed(() => prices.value?.cheapest_window ?? null)

function money(value: number | null | undefined, digits = 2): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return new Intl.NumberFormat(locale.value, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value)
}

function shortTime(iso: string | undefined): string {
  if (!iso) return ''
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: site.value?.timezone ?? 'UTC',
  }).format(new Date(iso))
}

const cheapestLabel = computed(() => {
  if (!cheapestWindow.value) return ''
  return `${shortTime(cheapestWindow.value.start)}–${shortTime(cheapestWindow.value.end)}`
})

const socStyle = computed<Record<string, string>>(() => ({
  '--soc': String(hasData.value && sample.value ? Math.round(sample.value.soc) : 0),
}))

interface PowerParts {
  value: string
  unit: string
}

function powerParts(value: number | null | undefined): PowerParts {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return { value: '—', unit: '' }
  }
  if (Math.abs(value) >= 1000) {
    return {
      value: new Intl.NumberFormat(locale.value, {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2,
      }).format(value / 1000),
      unit: 'kW',
    }
  }
  return { value: new Intl.NumberFormat(locale.value, { maximumFractionDigits: 0 }).format(value), unit: 'W' }
}

function formatPower(value: number | null | undefined): string {
  const parts = powerParts(value)
  return parts.unit ? `${parts.value} ${parts.unit}` : parts.value
}

function batteryState(value: number | undefined): string {
  if (value === undefined) return t('dashboard.noData')
  if (value > 50) return t('dashboard.batteryCharging')
  if (value < -50) return t('dashboard.batteryDischarging')
  return t('dashboard.batteryIdle')
}

function gridState(value: number | undefined): string {
  if (value === undefined) return ''
  if (value > 50) return t('dashboard.gridImport')
  if (value < -50) return t('dashboard.gridExport')
  return ''
}

const pvStringHint = computed(() => {
  const strings = sample.value?.pv_strings ?? []
  if (!strings.length || strings.every((watts) => watts <= 0)) return ''
  return strings
    .map((watts, index) => `${t('dashboard.mppt', { n: index + 1 })} ${formatPower(watts)}`)
    .join(' · ')
})

const telemetryHint = computed(() => {
  if (!hasData.value) return t('dashboard.waitingTelemetry')
  if (stale.value) return t('dashboard.staleTelemetry')
  if (age.value !== null) return t('dashboard.updatedAgo', { seconds: Math.max(0, age.value) })
  return ''
})

const tiles = computed(() => [
  {
    key: 'solar',
    label: t('dashboard.solar'),
    icon: 'bi-sun',
    cls: 'tile-solar',
    value: powerParts(sample.value?.pv_w),
    hint: pvStringHint.value,
  },
  {
    key: 'grid',
    label: t('dashboard.grid'),
    icon: 'bi-plug',
    cls: sample.value && sample.value.grid_w < 0 ? 'tile-solar' : 'tile-grid',
    value: powerParts(sample.value?.grid_w),
    hint: gridState(sample.value?.grid_w),
  },
  {
    key: 'battery',
    label: t('dashboard.battery'),
    icon: 'bi-battery-half',
    cls:
      sample.value && sample.value.battery_w > 50
        ? 'tile-solar'
        : sample.value && sample.value.battery_w < -50
          ? 'tile-grid'
          : 'tile-battery',
    value: powerParts(sample.value?.battery_w),
    hint: batteryState(sample.value?.battery_w),
  },
  {
    key: 'house',
    label: t('dashboard.house'),
    icon: 'bi-house',
    cls: 'tile-house',
    value: powerParts(sample.value?.load_w),
    hint: '',
  },
  {
    key: 'car',
    label: t('dashboard.car'),
    icon: 'bi-ev-station',
    cls: 'tile-car',
    value: powerParts(null),
    hint: t('dashboard.evPhase'),
  },
])

const sources = computed(() => {
  const features = (session.meta?.features ?? {}) as Record<string, unknown>
  return [
    { key: 'prices', label: t('dashboard.pricesAvailable'), phase: t('dashboard.pricesPhase'), done: Boolean(features.prices) },
    { key: 'telemetry', label: t('dashboard.telemetryAvailable'), phase: t('dashboard.telemetryPhase'), done: Boolean(features.telemetry) },
    { key: 'forecast', label: t('dashboard.forecastAvailable'), phase: t('dashboard.forecastPhase'), done: Boolean(features.forecast) },
    { key: 'planner', label: t('dashboard.plannerAvailable'), phase: t('dashboard.plannerPhase'), done: Boolean(features.battery_plan) },
    { key: 'actuation', label: t('dashboard.actuationAvailable'), phase: t('dashboard.actuationPhase'), done: Boolean(features.actuation) },
    { key: 'ev', label: t('dashboard.evAvailable'), phase: t('dashboard.evPhase'), done: Boolean(features.ev_charging) },
  ]
})
</script>

<template>
  <PageHead :title="t('dashboard.title')" :subtitle="siteLine">
    <span class="gate-pill" :class="armed ? 'gate-on' : 'gate-off'">
      <i class="bi" :class="armed ? 'bi-shield-check' : 'bi-shield-lock'"></i>
      {{ armed ? t('dashboard.armed') : t('dashboard.disarmed') }}
    </span>
  </PageHead>

  <div v-if="error" class="alert alert-warning" role="alert">{{ error }}</div>

  <section class="aipt-card mb-3">
    <div class="d-flex justify-content-between align-items-center mb-3">
      <h2 class="mb-0">{{ t('dashboard.energyFlow') }}</h2>
      <span class="badge-soft" :class="stale ? 'badge-warn' : hasData ? 'badge-on' : ''">
        {{ stale ? t('dashboard.staleTelemetry') : hasData ? t('dashboard.live') : t('dashboard.waiting') }}
      </span>
    </div>
    <div class="energy-grid">
      <div v-for="tile in tiles" :key="tile.key" class="energy-tile">
        <span class="tile-label">
          <i class="bi" :class="tile.icon" aria-hidden="true"></i>
          {{ tile.label }}
        </span>
        <span class="tile-value tabular" :class="tile.cls">
          {{ tile.value.value }}<span v-if="tile.value.unit" class="unit">{{ tile.value.unit }}</span>
        </span>
        <span v-if="tile.hint" class="tile-hint">{{ tile.hint }}</span>
      </div>
    </div>
    <p v-if="!hasData" class="text-muted small mb-0 mt-3">{{ t('dashboard.waitingTelemetry') }}</p>
  </section>

  <div class="row g-3">
    <div class="col-12 col-lg-4">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.soc') }}</h2>
        <div class="d-flex align-items-center gap-3">
          <div class="soc-ring" :style="socStyle">
            <span>{{ hasData && sample ? `${Math.round(sample.soc)} %` : '—' }}</span>
          </div>
          <div>
            <div class="fw-semibold">
              {{ hasData ? batteryState(sample?.battery_w) : t('dashboard.noData') }}
            </div>
            <div class="text-muted small">
              {{ telemetryHint }}
            </div>
          </div>
        </div>
      </section>
    </div>
    <div class="col-12 col-lg-4">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.priceCard') }}</h2>
        <template v-if="priceNow !== null">
          <div class="price-now">
            <strong>{{ money(priceNow, 2) }}</strong>
            <span>{{ currency }}/kWh</span>
          </div>
          <div class="key-value mt-3">
            <dt>{{ t('dashboard.nextPrice') }}</dt>
            <dd>{{ priceNext !== null ? `${money(priceNext, 2)} ${currency}` : '—' }}</dd>
          </div>
          <div v-if="cheapestWindow" class="key-value">
            <dt>{{ t('dashboard.cheapestWindow') }}</dt>
            <dd>{{ cheapestLabel }} · {{ money(cheapestWindow.avg_import_price, 2) }} {{ currency }}</dd>
          </div>
          <router-link to="/prices" class="small d-inline-block mt-2">
            {{ t('nav.prices') }} →
          </router-link>
        </template>
        <p v-else class="empty-state mb-0">{{ t('dashboard.noPrices') }}</p>
      </section>
    </div>
    <div class="col-12 col-lg-4">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.gates') }}</h2>
        <dl class="mb-0">
          <div class="key-value">
            <dt>{{ t('dashboard.writes') }}</dt>
            <dd>
              <span class="badge-soft" :class="gates?.writes_armed ? 'badge-on' : 'badge-off'">
                {{ gates?.writes_armed ? t('dashboard.armed') : t('dashboard.disarmed') }}
              </span>
            </dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.evControl') }}</dt>
            <dd>
              <span class="badge-soft" :class="gates?.ev_control_armed ? 'badge-on' : 'badge-off'">
                {{ gates?.ev_control_armed ? t('dashboard.armed') : t('dashboard.disarmed') }}
              </span>
            </dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.systemCheck') }} · {{ t('settings.inverter') }}</dt>
            <dd>
              <span class="badge-soft" :class="inverterCheck === 'ok' ? 'badge-on' : 'badge-off'">
                {{ inverterCheck }}
              </span>
            </dd>
          </div>
        </dl>
        <router-link to="/settings/inverter" class="small d-inline-block mt-2">
          {{ t('settings.inverter') }} →
        </router-link>
      </section>
    </div>
  </div>

  <div class="row g-3 mt-1">
    <div class="col-12 col-lg-7">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.sources') }}</h2>
        <ul class="source-list">
          <li v-for="source in sources" :key="source.key" class="source-item">
            <span class="source-check" :class="source.done ? 'done' : 'pending'">
              <i class="bi" :class="source.done ? 'bi-check-lg' : 'bi-hourglass'" aria-hidden="true"></i>
            </span>
            <span>{{ source.label }}</span>
            <span class="source-phase">{{ source.done ? t('dashboard.live') : source.phase }}</span>
          </li>
        </ul>
      </section>
    </div>
    <div class="col-12 col-lg-5">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.buildInfo') }}</h2>
        <dl class="mb-0">
          <div class="key-value">
            <dt>{{ t('common.version') }}</dt>
            <dd class="mono">{{ version }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.uptime') }}</dt>
            <dd class="tabular">{{ uptime }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.database') }}</dt>
            <dd>
              <span class="badge-soft" :class="database === 'ok' ? 'badge-on' : 'badge-warn'">
                {{ database }}
              </span>
            </dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.timezone') }}</dt>
            <dd>{{ site?.timezone ?? '—' }}</dd>
          </div>
        </dl>
      </section>
    </div>
  </div>
</template>
