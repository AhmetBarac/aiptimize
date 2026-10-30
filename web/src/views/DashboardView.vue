<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import { openStatusStream } from '../api/live'
import type {
  ForecastSnapshot,
  HistoryBucket,
  HistorySnapshot,
  Plan,
  PlanSlot,
  PlanView,
  PriceSnapshot,
  Status,
  TelemetrySample,
} from '../api/types'
import { session } from '../session'
import EnergyFlow from '../components/EnergyFlow.vue'
import ForecastChart from '../components/ForecastChart.vue'
import HouseSupplySheet from '../components/HouseSupplySheet.vue'
import LiveBadge from '../components/LiveBadge.vue'
import PlanTimeline from '../components/PlanTimeline.vue'
import PriceStrip from '../components/PriceStrip.vue'
import SegmentedControl from '../components/SegmentedControl.vue'
import {
  addDays,
  formatKwh,
  formatMoney,
  localDateKey,
  selfPoweredPct,
} from '../lib/energy'

const { t, locale } = useI18n()

const status = ref<Status | null>(null)
const statusError = ref('')
const forecast = ref<ForecastSnapshot | null>(null)
const forecastError = ref('')
const forecastRefreshing = ref(false)
const history = ref<HistorySnapshot | null>(null)
const historyLoading = ref(true)
const prices = ref<PriceSnapshot | null>(null)
const planView = ref<PlanView | null>(null)
const houseOpen = ref(false)
const horizon = ref<'24' | '48'>('24')
const streamConnected = ref(false)
let stopStream: (() => void) | undefined
let timers: Array<ReturnType<typeof setInterval>> = []

const site = computed(() => status.value?.site ?? session.meta?.site ?? null)
const siteName = computed(() => site.value?.name ?? t('app.name'))
const timezone = computed(() => status.value?.site.timezone ?? session.meta?.timezone ?? 'UTC')
const nowIso = computed(() => status.value?.now ?? new Date().toISOString())
const today = computed(() => localDateKey(nowIso.value, timezone.value))
const currency = computed(
  () => prices.value?.currency ?? status.value?.site.currency ?? site.value?.currency ?? '',
)
const siteLine = computed(() => {
  const parts = [site.value?.area, timezone.value].filter(Boolean)
  return parts.join(' · ')
})
const dateLabel = computed(() =>
  new Intl.DateTimeFormat(locale.value, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    timeZone: timezone.value,
  }).format(new Date(nowIso.value)),
)

const telemetry = computed(() => status.value?.telemetry ?? null)
const sample = computed<TelemetrySample | null>(() => telemetry.value?.sample ?? null)
const hasData = computed(() => telemetry.value?.available === true && sample.value !== null)
const stale = computed(() => telemetry.value?.stale === true)
const age = computed(() => telemetry.value?.age_s ?? null)
const ev = computed(() => status.value?.ev ?? null)
const evStatus = computed(() => ev.value?.status ?? null)
const evAvailable = computed(() => ev.value?.available === true)
const evConnected = computed(() => evStatus.value?.connected === true)
const evCharging = computed(() => evStatus.value?.charging === true)

const liveTone = computed<'live' | 'stale' | 'waiting' | 'offline'>(() => {
  if (statusError.value) return 'offline'
  if (!hasData.value) return 'waiting'
  return stale.value ? 'stale' : 'live'
})
const liveLabel = computed(() => {
  if (liveTone.value === 'offline') return t('home.offline')
  if (liveTone.value === 'waiting') return t('dashboard.waiting')
  return stale.value ? t('dashboard.staleTelemetry') : t('dashboard.live')
})
const liveDetail = computed(() => {
  if (liveTone.value !== 'live' || age.value === null) return ''
  return t('dashboard.updatedAgo', { seconds: Math.max(0, Math.round(age.value)) })
})

const todayBucket = computed<HistoryBucket | null>(
  () => history.value?.buckets.find((bucket) => bucket.label === today.value) ?? null,
)
const todayHasData = computed(() => {
  const bucket = todayBucket.value
  if (!bucket) return false
  return (
    bucket.pv_kwh > 0 ||
    bucket.load_kwh > 0 ||
    bucket.ev_kwh > 0 ||
    bucket.grid_import_kwh > 0 ||
    bucket.grid_export_kwh > 0 ||
    bucket.battery_charge_kwh > 0 ||
    bucket.battery_discharge_kwh > 0
  )
})
const selfPowered = computed(() => (todayHasData.value ? selfPoweredPct(todayBucket.value) : null))
const consumptionToday = computed(() => {
  if (!todayBucket.value || !todayHasData.value) return null
  return todayBucket.value.load_kwh + todayBucket.value.ev_kwh
})

const plan = computed<Plan | null>(() => planView.value?.inverter_plan ?? null)
const planSlots = computed<PlanSlot[]>(() =>
  (plan.value?.slots ?? []).filter(
    (slot) => (slot.day_local || localDateKey(slot.start, timezone.value)) === today.value,
  ),
)
const nextSlot = computed<PlanSlot | null>(() => {
  const now = Date.parse(nowIso.value)
  return planSlots.value.find((slot) => Date.parse(slot.end) > now) ?? null
})
const planStatusLabel = computed(() => {
  if (!plan.value) return t('home.noPlan')
  if (planView.value?.applied) return t('plan.statusApplied')
  if (planView.value?.draft) return t('plan.statusDraft')
  return t('plan.statusAuto')
})
const planBadgeClass = computed(() => {
  if (!plan.value) return 'badge-off'
  if (planView.value?.applied) return 'badge-on'
  if (planView.value?.draft) return 'badge-draft'
  return ''
})

const forecastPoints = computed(() => {
  const hours = Number(horizon.value)
  const cutoff = Date.now() + hours * 3600 * 1000
  return (forecast.value?.points ?? []).filter((point) => Date.parse(point.start) < cutoff)
})
const forecastPvKWh = computed(
  () => forecastPoints.value.reduce((sum, point) => sum + point.pv_w * 0.5, 0) / 1000,
)
const forecastLoadKWh = computed(
  () => forecastPoints.value.reduce((sum, point) => sum + point.load_w * 0.5, 0) / 1000,
)
const forecastUpdated = computed(() => {
  const at = forecast.value?.generated_at
  if (!at) return ''
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: forecast.value?.timezone ?? timezone.value,
  }).format(new Date(at))
})
const forecastWarnings = computed(() => forecast.value?.warnings ?? [])

const priceNow = computed(() => status.value?.prices.now_import ?? null)
const priceNext = computed(() => status.value?.prices.next_import ?? null)
const cheapestWindow = computed(() => status.value?.prices.cheapest_window ?? null)
const priceBlocks = computed(() => prices.value?.blocks ?? [])

const horizonOptions = computed(() => [
  { value: '24', label: t('forecast.h24') },
  { value: '48', label: t('forecast.h48') },
])

const flowLabels = computed(() => ({
  flowTitle: t('home.energyFlow'),
  flowSubtitle: t('home.liveNow'),
  solar: t('dashboard.solar'),
  grid: t('dashboard.grid'),
  battery: t('dashboard.battery'),
  house: t('dashboard.house'),
  car: t('dashboard.car'),
  charging: t('dashboard.batteryCharging'),
  discharging: t('dashboard.batteryDischarging'),
  producing: t('home.producing'),
  idle: t('home.idle'),
  importing: t('dashboard.gridImport'),
  exporting: t('dashboard.gridExport'),
  connected: t('car.connected'),
  unplugged: t('car.unplugged'),
  liveLoad: t('home.liveLoad'),
  inspectHouse: t('home.viewSupply'),
  noData: t('dashboard.noData'),
}))

const houseLabels = computed(() => ({
  title: t('home.supplyTitle'),
  subtitle: t('home.supplySubtitle'),
  solar: t('dashboard.solar'),
  battery: t('dashboard.battery'),
  grid: t('dashboard.grid'),
  surplus: t('home.surplus'),
  estimate: t('home.supplyEstimate'),
  noData: t('home.noSupplyData'),
}))

const timelineLabels = computed(() => ({
  charge: t('plan.charge'),
  discharge: t('plan.discharge'),
  now: t('home.now'),
  empty: t('home.noSlotsShort'),
}))

const nextSlotLabel = computed(() => {
  if (!nextSlot.value) return t('plan.noUpcoming')
  const direction = nextSlot.value.direction === 'charge' ? t('plan.charge') : t('plan.discharge')
  return `${direction} ${planTime(nextSlot.value.start)}`
})

const cheapestLabel = computed(() => {
  if (!cheapestWindow.value) return ''
  return `${planTime(cheapestWindow.value.start)}–${planTime(cheapestWindow.value.end)}`
})

function planTime(iso: string | undefined): string {
  if (!iso) return '—'
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: timezone.value,
  }).format(new Date(iso))
}

function planClock(iso: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: timezone.value,
  }).format(new Date(iso))
}

function money(value: number | null | undefined, digits = 2): string {
  return formatMoney(value, locale.value, digits)
}

function kwh(value: number | null | undefined): string {
  return formatKwh(value, locale.value, 1)
}

async function loadStatus(): Promise<void> {
  if (streamConnected.value) return
  try {
    status.value = await api.get<Status>('/status')
    statusError.value = ''
  } catch (err) {
    statusError.value = err instanceof Error ? err.message : String(err)
  }
}

async function loadForecast(): Promise<void> {
  try {
    forecast.value = await api.get<ForecastSnapshot>('/forecast')
    forecastError.value = ''
  } catch (err) {
    forecastError.value = err instanceof Error ? err.message : String(err)
  }
}

async function refreshForecast(): Promise<void> {
  forecastRefreshing.value = true
  forecastError.value = ''
  try {
    forecast.value = await api.post<ForecastSnapshot>('/forecast/refresh', {})
  } catch (err) {
    forecastError.value = err instanceof Error ? err.message : String(err)
  } finally {
    forecastRefreshing.value = false
  }
}

async function loadHistory(): Promise<void> {
  if (!today.value) return
  historyLoading.value = history.value === null
  try {
    history.value = await api.get<HistorySnapshot>(
      `/history?from=${addDays(today.value, -6)}&to=${addDays(today.value, 1)}&resolution=day`,
    )
  } catch {
    // The live card still works; metric values fall back to an em dash.
  } finally {
    historyLoading.value = false
  }
}

async function loadPlan(): Promise<void> {
  if (!today.value) return
  try {
    planView.value = await api.get<PlanView>(`/plan?date=${today.value}`)
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      planView.value = null
    }
  }
}

const syncChecking = ref(false)
const syncMatch = computed(
  () => planView.value?.inverter_read_ok ?? planView.value?.sync?.match ?? false,
)

async function recheckPlan(): Promise<void> {
  if (syncChecking.value) return
  syncChecking.value = true
  try {
    await loadPlan()
    await loadStatus()
  } finally {
    syncChecking.value = false
  }
}

async function loadPrices(): Promise<void> {
  try {
    prices.value = await api.get<PriceSnapshot>('/prices')
  } catch {
    // The compact price card uses the status summary when this fails.
  }
}

onMounted(() => {
  void loadStatus()
  void loadForecast()
  void loadHistory()
  void loadPlan()
  void loadPrices()
  stopStream = openStatusStream(
    (next) => {
      status.value = next
      statusError.value = ''
    },
    (connected) => {
      streamConnected.value = connected
      if (!connected) void loadStatus()
    },
  )
  timers = [
    setInterval(() => void loadStatus(), 10_000),
    setInterval(() => void loadForecast(), 300_000),
    setInterval(() => void loadHistory(), 300_000),
    setInterval(() => void loadPlan(), 900_000),
    setInterval(() => void loadPrices(), 900_000),
  ]
})

onBeforeUnmount(() => {
  stopStream?.()
  stopStream = undefined
  timers.forEach((timer) => clearInterval(timer))
  timers = []
})

watch(today, (value, previous) => {
  if (value && previous && value !== previous) {
    void loadHistory()
    void loadPlan()
  }
})
</script>

<template>
  <div class="home">
    <div v-if="statusError" class="alert alert-warning" role="alert">{{ statusError }}</div>

    <section class="home-hero">
      <div class="hero-top">
        <div>
          <p class="eyebrow">{{ siteLine }}</p>
          <h1>{{ siteName }}</h1>
          <p class="hero-date">{{ dateLabel }}</p>
        </div>
        <LiveBadge :tone="liveTone" :label="liveLabel" :detail="liveDetail" />
      </div>

      <div class="hero-grid">
        <div class="hero-primary">
          <span class="hero-kicker">{{ t('home.selfPowered') }}</span>
          <div class="hero-ring" :style="{ '--value': String(selfPowered ?? 0) }">
            <span class="hero-ring-value tabular">
              {{ selfPowered === null ? '—' : Math.round(selfPowered) }}<small v-if="selfPowered !== null">%</small>
            </span>
          </div>
          <p class="hero-note">{{ todayHasData ? t('home.selfPoweredHint') : t('home.collecting') }}</p>
        </div>

        <div class="hero-stats">
          <div class="hero-stat">
            <span>{{ t('home.solarToday') }}</span>
            <strong class="tabular">{{ todayHasData ? kwh(todayBucket?.pv_kwh) : '—' }}<small v-if="todayHasData">kWh</small></strong>
            <em>{{ t('home.soFar') }}</em>
          </div>
          <div class="hero-stat">
            <span>{{ t('home.consumptionToday') }}</span>
            <strong class="tabular">{{ consumptionToday === null ? '—' : kwh(consumptionToday) }}<small v-if="consumptionToday !== null">kWh</small></strong>
            <em>{{ t('home.soFar') }}</em>
          </div>
          <div class="hero-stat">
            <span>{{ t('home.savedToday') }}</span>
            <strong class="tabular">{{ todayHasData ? money(todayBucket?.savings) : '—' }}<small v-if="todayHasData">{{ currency }}</small></strong>
            <em>{{ t('home.vsBaseline') }}</em>
          </div>
        </div>
      </div>
    </section>

    <div class="home-grid">
      <EnergyFlow
        class="home-flow"
        :sample="sample"
        :soc="sample?.soc ?? null"
        :ev-power-w="evStatus?.power_w ?? null"
        :ev-available="evAvailable"
        :ev-connected="evConnected"
        :ev-charging="evCharging"
        :stale="stale"
        :locale="locale"
        :labels="flowLabels"
        @open-house="houseOpen = true"
      />

      <section class="aipt-card home-plan">
        <header class="card-head">
          <div>
            <p class="card-kicker">{{ t('plan.title') }}</p>
            <h2>{{ t('home.today') }}</h2>
          </div>
          <div class="plan-head-badges">
            <span class="badge-soft" :class="planBadgeClass">{{ planStatusLabel }}</span>
            <button
              v-if="planView && (planView.sync || planView.inverter_read_ok)"
              type="button"
              class="badge-soft plan-sync-chip"
              :class="syncMatch ? 'badge-on' : 'badge-warn'"
              :disabled="syncChecking"
              :title="t('plan.recheckSync')"
              :aria-label="t('plan.recheckSync')"
              @click="recheckPlan"
            >
              <i v-if="syncChecking" class="bi bi-arrow-clockwise spin" aria-hidden="true"></i>
              {{ syncMatch ? t('plan.syncInSync') : t('plan.syncOutOfSync') }}
            </button>
          </div>
        </header>

        <RouterLink v-if="plan" to="/plan" class="plan-timeline-link" :aria-label="t('plan.openTimeline')">
          <PlanTimeline
            :slots="planSlots"
            :day="today"
            :timezone="timezone"
            :locale="locale"
            :now="nowIso"
            compact
            :labels="timelineLabels"
          />
        </RouterLink>
        <div v-else class="plan-empty">
          <p>{{ t('plan.notReady') }}</p>
        </div>

        <ul v-if="plan && planSlots.length" class="plan-mini-list">
          <li v-for="slot in planSlots" :key="slot.start">
            <span class="plan-mini-dot" :class="slot.direction" aria-hidden="true"></span>
            <span class="plan-mini-time">{{ planClock(slot.start) }}–{{ planClock(slot.end) }}</span>
            <strong>{{ slot.direction === 'charge' ? t('plan.charge') : t('plan.discharge') }}</strong>
            <span class="plan-mini-energy">{{ kwh(slot.energy_kwh) }} kWh</span>
          </li>
        </ul>

        <div class="plan-home-footer">
          <div class="plan-stat">
            <span>{{ t('plan.savings') }}</span>
            <strong class="tabular">{{ plan ? money(plan.summary.savings) : '—' }}<small v-if="plan">{{ plan.summary.currency || currency }}</small></strong>
          </div>
          <div class="plan-stat">
            <span>{{ t('plan.nextSlot') }}</span>
            <strong class="tabular plan-next">{{ nextSlotLabel }}</strong>
          </div>
        </div>
      </section>

      <section class="aipt-card home-forecast">
        <header class="card-head">
          <div>
            <p class="card-kicker">{{ t('forecast.title') }}</p>
            <h2>{{ t('home.nextHours', { hours: horizon }) }}</h2>
          </div>
          <div class="card-tools">
            <SegmentedControl v-model="horizon" :options="horizonOptions" :group-label="t('forecast.title')" size="sm" />
            <button class="icon-button" type="button" :title="t('forecast.refresh')" :aria-label="t('forecast.refresh')" :disabled="forecastRefreshing" @click="refreshForecast">
              <i class="bi bi-arrow-clockwise" :class="{ spin: forecastRefreshing }" aria-hidden="true"></i>
            </button>
          </div>
        </header>

        <template v-if="forecast">
          <div class="forecast-stats">
            <div>
              <span class="tile-label">{{ t('forecast.pvTotal', { hours: horizon }) }}</span>
              <strong class="tabular">{{ forecastPvKWh.toFixed(1) }}<span class="unit">kWh</span></strong>
            </div>
            <div>
              <span class="tile-label">{{ t('forecast.loadTotal', { hours: horizon }) }}</span>
              <strong class="tabular">{{ forecastLoadKWh.toFixed(1) }}<span class="unit">kWh</span></strong>
            </div>
            <div>
              <span class="tile-label">{{ t('forecast.confidence') }}</span>
              <strong class="tabular">{{ Math.round(forecast.confidence * 100) }}<span class="unit">%</span></strong>
            </div>
          </div>
          <ForecastChart
            :points="forecastPoints"
            :timezone="forecast.timezone"
            :locale="locale"
            :label-pv="t('forecast.pv')"
            :label-load="t('forecast.load')"
            :height="220"
          />
          <div class="card-meta">
            <span>{{ forecast.source }} · {{ t('forecast.strings', { count: forecast.strings?.length ?? 0 }) }}<span v-if="forecast.calibration_factor !== 1"> · {{ forecast.calibration_factor.toFixed(2) }}×</span></span>
            <span>{{ t('forecast.updated', { time: forecastUpdated }) }}</span>
          </div>
          <div v-if="forecast.stale" class="mt-2">
            <span class="badge-soft badge-warn">{{ t('forecast.stale') }}</span>
          </div>
          <p v-if="forecastWarnings.length" class="card-warnings">{{ forecastWarnings.join(' · ') }}</p>
        </template>
        <p v-else class="empty-state mb-0">{{ forecastError || t('forecast.notReady') }}</p>
      </section>

      <section class="aipt-card home-prices">
        <header class="card-head">
          <div>
            <p class="card-kicker">{{ t('dashboard.priceCard') }}</p>
            <h2>{{ t('home.pricesNext') }}</h2>
          </div>
          <RouterLink to="/prices" class="card-link">{{ t('home.allPrices') }} →</RouterLink>
        </header>

        <div class="price-now">
          <strong class="tabular">{{ money(priceNow) }}</strong>
          <span>{{ currency }}/kWh</span>
        </div>
        <div class="key-value">
          <dt>{{ t('dashboard.nextPrice') }}</dt>
          <dd>{{ priceNext !== null ? `${money(priceNext)} ${currency}` : '—' }}</dd>
        </div>
        <div v-if="cheapestWindow" class="key-value">
          <dt>{{ t('dashboard.cheapestWindow') }}</dt>
          <dd>{{ cheapestLabel }} · {{ money(cheapestWindow.avg_import_price) }} {{ currency }}</dd>
        </div>

        <PriceStrip
          v-if="prices && priceBlocks.length"
          :blocks="priceBlocks"
          :now="nowIso"
          :timezone="timezone"
          :locale="locale"
          :currency="currency"
          :cheapest="cheapestWindow"
        />
        <p v-else class="empty-state mb-0">{{ t('dashboard.noPrices') }}</p>
      </section>
    </div>

    <HouseSupplySheet
      v-model:open="houseOpen"
      :sample="sample"
      :locale="locale"
      :labels="houseLabels"
    />
  </div>
</template>
