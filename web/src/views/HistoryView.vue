<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { HistorySnapshot } from '../api/types'
import HistoryChart from '../components/HistoryChart.vue'
import PageHead from '../components/PageHead.vue'

const { t, locale } = useI18n()
const snapshot = ref<HistorySnapshot | null>(null)
const loading = ref(true)
const error = ref('')
const resolution = ref<'day' | 'week'>('day')

async function load(): Promise<void> {
  loading.value = true
  try {
    snapshot.value = await api.get<HistorySnapshot>(`/history?resolution=${resolution.value}`)
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    loading.value = false
  }
}

function setResolution(value: 'day' | 'week'): void {
  resolution.value = value
  void load()
}

onMounted(() => {
  void load()
})

const totals = computed(() => snapshot.value?.totals)
const currency = computed(() => snapshot.value?.currency ?? '')

function kwh(value: number | undefined): string {
  return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(value ?? 0)
}

function money(value: number | undefined): string {
  return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value ?? 0)
}
</script>

<template>
  <PageHead :title="t('history.title')" :subtitle="t('history.subtitle')">
    <div class="day-tabs">
      <button class="btn btn-sm" :class="resolution === 'day' ? 'btn-primary' : 'btn-outline-secondary'" type="button" @click="setResolution('day')">{{ t('history.daily') }}</button>
      <button class="btn btn-sm" :class="resolution === 'week' ? 'btn-primary' : 'btn-outline-secondary'" type="button" @click="setResolution('week')">{{ t('history.weekly') }}</button>
    </div>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="snapshot && totals">
    <div class="solar-stats mb-3">
      <section class="aipt-card">
        <span class="tile-label">{{ t('history.pv') }}</span>
        <strong class="solar-value">{{ kwh(totals.pv_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('history.load') }}</span>
        <strong class="solar-value">{{ kwh(totals.load_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('history.gridImport') }}</span>
        <strong class="solar-value">{{ kwh(totals.grid_import_kwh) }}<span class="unit">kWh</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('history.cost') }}</span>
        <strong class="solar-value">{{ money(totals.cost) }}<span class="unit">{{ currency }}</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('history.savings') }}</span>
        <strong class="solar-value" :class="{ 'text-success': totals.savings > 0 }">{{ money(totals.savings) }}<span class="unit">{{ currency }}</span></strong>
      </section>
    </div>

    <section class="aipt-card mb-3">
      <h2>{{ t('history.chartTitle') }}</h2>
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
      <p v-for="warning in snapshot.warnings" :key="warning" class="small text-muted mb-1">{{ warning }}</p>
    </section>

    <section class="aipt-card">
      <h2>{{ t('history.tableTitle') }}</h2>
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
    </section>
  </template>
</template>
