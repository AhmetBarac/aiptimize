<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { SolarSnapshot } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
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

const accuracy = computed(() => {
  if (!snapshot.value || snapshot.value.expected_kwh <= 0 || snapshot.value.actual_kwh <= 0) return null
  return (snapshot.value.actual_kwh / snapshot.value.expected_kwh) * 100
})

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
    <RouterLink class="btn btn-sm btn-outline-secondary" to="/">
      <i class="bi bi-arrow-left" aria-hidden="true"></i>
      <span class="ms-1">{{ t('solar.back') }}</span>
    </RouterLink>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="snapshot">
    <div class="metric-grid">
      <MetricCard :label="t('solar.current')" :value="formatPower(snapshot.current_w)" icon="bi-brightness-high" tone="solar" />
      <MetricCard :label="t('solar.actual')" :value="kWh(snapshot.actual_kwh)" unit="kWh" icon="bi-graph-up-arrow" tone="solar" />
      <MetricCard :label="t('solar.expected')" :value="kWh(snapshot.expected_kwh)" unit="kWh" icon="bi-cloud-sun" tone="neutral" :hint="`${kWh(snapshot.min_kwh)}–${kWh(snapshot.max_kwh)} kWh`" />
      <MetricCard :label="t('solar.accuracy')" :value="accuracy === null ? '—' : String(Math.round(accuracy))" :unit="accuracy === null ? '' : '%'" icon="bi-bullseye" tone="neutral" :hint="t('solar.accuracyHint')" />
      <MetricCard :label="t('solar.load')" :value="kWh(snapshot.load_kwh)" unit="kWh" icon="bi-house-door" tone="neutral" />
    </div>

    <section class="aipt-card mt-3">
      <div class="card-head">
        <div>
          <p class="card-kicker">{{ t('solar.title') }}</p>
          <h2>{{ t('solar.chartTitle') }}</h2>
        </div>
        <div class="card-tools">
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
      <p v-for="warning in snapshot.warnings" :key="warning" class="card-warnings">{{ warning }}</p>
    </section>
  </template>
</template>
