<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { Status } from '../api/types'
import { session } from '../session'
import PageHead from '../components/PageHead.vue'

const { t } = useI18n()
const status = ref<Status | null>(null)
const error = ref('')

onMounted(async () => {
  try {
    status.value = await api.get<Status>('/status')
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
})

const site = computed<{ name: string; area?: string; timezone?: string; currency?: string } | null>(
  () => status.value?.site ?? session.meta?.site ?? null,
)
const gates = computed(() => status.value?.gates ?? session.meta?.gates ?? null)
const armed = computed(() => gates.value?.writes_armed === true)
const version = computed(() => status.value?.version ?? session.meta?.version ?? '—')

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

const tiles = computed(() => [
  { key: 'solar', label: t('dashboard.solar'), icon: 'bi-sun', cls: 'tile-solar' },
  { key: 'grid', label: t('dashboard.grid'), icon: 'bi-plug', cls: 'tile-grid' },
  { key: 'battery', label: t('dashboard.battery'), icon: 'bi-battery-half', cls: 'tile-battery' },
  { key: 'house', label: t('dashboard.house'), icon: 'bi-house', cls: 'tile-house' },
  { key: 'car', label: t('dashboard.car'), icon: 'bi-ev-station', cls: 'tile-car' },
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
      <span class="badge-soft">{{ t('dashboard.waiting') }}</span>
    </div>
    <div class="energy-grid">
      <div v-for="tile in tiles" :key="tile.key" class="energy-tile">
        <span class="tile-label">
          <i class="bi" :class="tile.icon" aria-hidden="true"></i>
          {{ tile.label }}
        </span>
        <span class="tile-value tabular" :class="tile.cls">—</span>
      </div>
    </div>
    <p class="text-muted small mb-0 mt-3">{{ t('dashboard.waitingTelemetry') }}</p>
  </section>

  <div class="row g-3">
    <div class="col-12 col-lg-5">
      <section class="aipt-card h-100">
        <h2>{{ t('dashboard.soc') }}</h2>
        <div class="d-flex align-items-center gap-3">
          <div class="soc-ring" style="--soc: 0">
            <span>—</span>
          </div>
          <div>
            <div class="fw-semibold">{{ t('dashboard.noData') }}</div>
            <div class="text-muted small">{{ t('dashboard.waitingTelemetry') }}</div>
          </div>
        </div>
      </section>
    </div>
    <div class="col-12 col-lg-7">
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
        </dl>
        <router-link to="/settings/safety" class="small d-inline-block mt-2">
          {{ t('settings.sections.safety') }} →
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
