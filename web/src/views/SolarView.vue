<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { SolarSnapshot } from '../api/types'
import PageHead from '../components/PageHead.vue'
import SolarChart from '../components/SolarChart.vue'

const { t, locale } = useI18n()
const snapshot = ref<SolarSnapshot | null>(null)
const error = ref('')
const loading = ref(true)
let timer: ReturnType<typeof setInterval> | undefined

async function load(): Promise<void> {
  try {
    snapshot.value = await api.get<SolarSnapshot>('/solar')
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void load(), 60_000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})

function formatPower(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  if (Math.abs(value) >= 1000) {
    return `${new Intl.NumberFormat(locale.value, {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(value / 1000)} kW`
  }
  return `${Math.round(value)} W`
}

function kWh(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—'
  return new Intl.NumberFormat(locale.value, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  }).format(value)
}

const dateLabel = computed(() => {
  const start = snapshot.value?.points[0]?.start
  if (!start) return ''
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    timeZone: snapshot.value?.timezone ?? 'UTC',
  }).format(new Date(start))
})

const updated = computed(() => {
  const at = snapshot.value?.generated_at
  if (!at) return ''
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: snapshot.value?.timezone ?? 'UTC',
  }).format(new Date(at))
})
</script>

<template>
  <PageHead :title="t('solar.title')" :subtitle="dateLabel || t('solar.subtitle')">
    <router-link class="btn btn-sm btn-outline-secondary" to="/">
      <i class="bi bi-arrow-left" aria-hidden="true"></i>
      <span class="ms-1">{{ t('solar.back') }}</span>
    </router-link>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="snapshot">
    <div class="solar-stats mb-3">
      <section class="aipt-card">
        <span class="tile-label">{{ t('solar.current') }}</span>
        <strong class="solar-value">{{ formatPower(snapshot.current_w) }}</strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('solar.actual') }}</span>
        <strong class="solar-value">{{ kWh(snapshot.actual_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('solar.expected') }}</span>
        <strong class="solar-value">{{ kWh(snapshot.expected_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('solar.estimateRange') }}</span>
        <strong class="solar-value">{{ kWh(snapshot.min_kwh) }}–{{ kWh(snapshot.max_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('solar.load') }}</span>
        <strong class="solar-value">{{ kWh(snapshot.load_kwh) }}<span class="unit">kWh</span></strong>
      </section>
    </div>

    <section class="aipt-card">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h2 class="mb-0">{{ t('solar.chartTitle') }}</h2>
        <div class="d-flex align-items-center gap-2">
          <span v-if="snapshot.stale" class="badge-soft badge-warn">{{ t('solar.stale') }}</span>
          <span v-if="updated" class="small text-muted">{{ t('solar.updated', { time: updated }) }}</span>
        </div>
      </div>
      <SolarChart
        :points="snapshot.points"
        :timezone="snapshot.timezone"
        :locale="locale"
        :labels="{
          measured: t('solar.measured'),
          expected: t('solar.expectedCurve'),
          minimum: t('solar.minEstimate'),
          maximum: t('solar.maxEstimate'),
          load: t('solar.loadCurve'),
        }"
      />
      <p v-for="warning in snapshot.warnings" :key="warning" class="small text-muted mb-1 mt-2">
        {{ warning }}
      </p>
    </section>
  </template>
</template>
