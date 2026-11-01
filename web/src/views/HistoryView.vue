<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { HistorySnapshot } from '../api/types'
import { session } from '../session'
import HistoryChart from '../components/HistoryChart.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHead from '../components/PageHead.vue'
import SegmentedControl from '../components/SegmentedControl.vue'
import { addDays, formatKwh, formatMoney, localDateKey, selfPoweredPct } from '../lib/energy'

const { t, locale } = useI18n()
const snapshot = ref<HistorySnapshot | null>(null)
const loading = ref(true)
const error = ref('')
const range = ref<'7d' | '30d' | '12w'>('7d')

const timezone = computed(() => session.meta?.timezone ?? snapshot.value?.timezone ?? 'UTC')
const today = computed(() => localDateKey(new Date(), timezone.value))

const rangeOptions = computed(() => [
  { value: '7d', label: t('history.range7') },
  { value: '30d', label: t('history.range30') },
  { value: '12w', label: t('history.range12w') },
])

const totals = computed(() => snapshot.value?.totals)
const currency = computed(() => snapshot.value?.currency ?? session.meta?.site.currency ?? '')
const selfPowered = computed(() => selfPoweredPct(totals.value))
const consumption = computed(() => {
  if (!totals.value) return null
  return totals.value.load_kwh
})
const hasData = computed(() => {
  const value = totals.value
  if (!value) return false
  return value.pv_kwh > 0 || value.load_kwh > 0 || value.grid_import_kwh > 0 || value.cost !== 0
})
const rangeLabel = computed(() => {
  if (!snapshot.value) return ''
  const from = localDateKey(snapshot.value.from, timezone.value)
  const to = addDays(localDateKey(snapshot.value.to, timezone.value), -1)
  return `${from} → ${to}`
})

function rangeParams(value: '7d' | '30d' | '12w'): { from: string; to: string; resolution: 'day' | 'week' } {
  const days = value === '7d' ? 7 : value === '30d' ? 30 : 84
  return {
    from: addDays(today.value, -(days - 1)),
    to: addDays(today.value, 1),
    resolution: value === '12w' ? 'week' : 'day',
  }
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const params = rangeParams(range.value)
    snapshot.value = await api.get<HistorySnapshot>(
      `/history?from=${params.from}&to=${params.to}&resolution=${params.resolution}`,
    )
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

function kwh(value: number | null | undefined): string {
  return formatKwh(value, locale.value, 1)
}

function money(value: number | null | undefined): string {
  return formatMoney(value, locale.value, 2)
}

onMounted(() => {
  void load()
})

watch(range, () => {
  void load()
})
</script>

<template>
  <PageHead :title="t('history.title')" :subtitle="t('history.subtitle')">
    <SegmentedControl v-model="range" :options="rangeOptions" :group-label="t('history.title')" size="sm" />
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="snapshot && totals">
    <div class="metric-grid">
      <MetricCard
        :label="t('history.pv')"
        :value="kwh(totals.pv_kwh)"
        unit="kWh"
        icon="bi-sun"
        tone="solar"
        :points="snapshot.buckets.map((bucket) => bucket.pv_kwh)"
        :hint="t('home.soFar')"
      />
      <MetricCard
        :label="t('history.load')"
        :value="consumption === null ? '—' : kwh(consumption)"
        :unit="consumption === null ? '' : 'kWh'"
        icon="bi-house-door"
        tone="neutral"
        :points="snapshot.buckets.map((bucket) => bucket.load_kwh)"
        :hint="t('home.soFar')"
      />
      <MetricCard
        :label="t('history.gridImport')"
        :value="kwh(totals.grid_import_kwh)"
        unit="kWh"
        icon="bi-plug"
        tone="grid"
        :points="snapshot.buckets.map((bucket) => bucket.grid_import_kwh)"
        :hint="totals.grid_export_kwh > 0 ? t('home.exported', { value: kwh(totals.grid_export_kwh) }) : t('home.soFar')"
      />
      <MetricCard
        :label="t('history.savings')"
        :value="money(totals.savings)"
        :unit="currency"
        icon="bi-piggy-bank"
        tone="neutral"
        :points="snapshot.buckets.map((bucket) => bucket.savings)"
        :hint="t('home.vsBaseline')"
      />
    </div>

    <div class="history-summary">
      <span>{{ t('history.selfPowered') }}</span>
      <strong class="tabular">{{ selfPowered === null ? '—' : Math.round(selfPowered) }}<small v-if="selfPowered !== null">%</small></strong>
      <span>{{ t('history.selfPoweredHint') }}</span>
    </div>

    <section class="aipt-card mt-3">
      <div class="card-head">
        <div>
          <p class="card-kicker">{{ t('history.title') }}</p>
          <h2>{{ t('history.chartTitle') }}</h2>
        </div>
        <span class="small text-muted">{{ rangeLabel }}</span>
      </div>
      <HistoryChart
        :buckets="snapshot.buckets"
        :locale="locale"
        :labels="{
          pv: t('history.pv'),
          load: t('history.load'),
          gridImport: t('history.gridImport'),
          gridExport: t('history.gridExport'),
          cost: t('history.cost'),
          savings: t('history.savings'),
        }"
      />
      <p v-for="warning in snapshot.warnings" :key="warning" class="card-warnings">{{ warning }}</p>
    </section>

    <details class="history-table aipt-card mt-3">
      <summary>
        <span>{{ t('history.tableTitle') }}</span>
        <i class="bi bi-chevron-down" aria-hidden="true"></i>
      </summary>
      <div class="table-responsive">
        <table class="table table-sm plan-table">
          <thead>
            <tr>
              <th>{{ t('history.period') }}</th>
              <th class="text-end">{{ t('history.pv') }}</th>
              <th class="text-end">{{ t('history.load') }}</th>
              <th class="text-end d-none d-md-table-cell">{{ t('history.import') }}</th>
              <th class="text-end d-none d-md-table-cell">{{ t('history.export') }}</th>
              <th class="text-end">{{ t('history.cost') }}</th>
              <th class="text-end">{{ t('history.savings') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bucket in snapshot.buckets" :key="bucket.label">
              <td>{{ bucket.label }}</td>
              <td class="text-end">{{ kwh(bucket.pv_kwh) }}</td>
              <td class="text-end">{{ kwh(bucket.load_kwh) }}</td>
              <td class="text-end d-none d-md-table-cell">{{ kwh(bucket.grid_import_kwh) }}</td>
              <td class="text-end d-none d-md-table-cell">{{ kwh(bucket.grid_export_kwh) }}</td>
              <td class="text-end">{{ money(bucket.cost) }}</td>
              <td class="text-end">{{ money(bucket.savings) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!hasData" class="empty-state mb-0">{{ t('history.noData') }}</p>
    </details>
  </template>
</template>
