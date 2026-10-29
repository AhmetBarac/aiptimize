<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type { Plan } from '../api/types'
import PageHead from '../components/PageHead.vue'
import PlanChart from '../components/PlanChart.vue'

const { t, locale } = useI18n()
const plan = ref<Plan | null>(null)
const loading = ref(true)
const recomputing = ref(false)
const error = ref('')

async function load(): Promise<void> {
  try {
    plan.value = await api.get<Plan>('/plan')
    error.value = ''
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      plan.value = null
      error.value = ''
    } else {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    loading.value = false
  }
}

async function recompute(): Promise<void> {
  recomputing.value = true
  error.value = ''
  try {
    plan.value = await api.post<Plan>('/plan/recompute', {})
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    recomputing.value = false
  }
}

onMounted(() => {
  void load()
})

function fmtMoney(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '—'
  return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)
}

function fmtEnergy(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '—'
  return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(value)
}

function fmtTime(iso: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: plan.value?.timezone ?? 'UTC',
  }).format(new Date(iso))
}

const slotCount = computed(() => plan.value?.slots.length ?? 0)
const currency = computed(() => plan.value?.summary.currency ?? '')
const hasSlots = computed(() => slotCount.value > 0)
</script>

<template>
  <PageHead :title="t('plan.title')" :subtitle="t('plan.subtitle')">
    <button class="btn btn-primary" type="button" :disabled="recomputing" @click="recompute">
      <span v-if="recomputing" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
      <i v-else class="bi bi-arrow-repeat" aria-hidden="true"></i>
      <span class="ms-1">{{ t('plan.recompute') }}</span>
    </button>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="plan">
    <div class="plan-stats mb-3">
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.savings') }}</span>
        <strong class="plan-value" :class="{ 'text-success': plan.summary.savings > 0 }">
          {{ fmtMoney(plan.summary.savings) }}<span class="unit">{{ currency }}</span>
        </strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.cost') }}</span>
        <strong class="plan-value">{{ fmtMoney(plan.summary.cost) }}<span class="unit">{{ currency }}</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.baseline') }}</span>
        <strong class="plan-value">{{ fmtMoney(plan.summary.baseline_cost) }}<span class="unit">{{ currency }}</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.slots') }}</span>
        <strong class="plan-value">{{ slotCount }}<span class="unit">{{ t('plan.perDay') }}</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.cycles') }}</span>
        <strong class="plan-value">{{ plan.summary.cycles.toFixed(2) }}</strong>
      </section>
    </div>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h2 class="mb-0">{{ t('plan.chartTitle') }}</h2>
        <span class="small text-muted">
          {{ t('plan.horizon', { from: fmtTime(plan.horizon_start), to: fmtTime(plan.horizon_end) }) }}
        </span>
      </div>
      <PlanChart
        :plan="plan"
        :locale="locale"
        :labels="{
          price: t('plan.price'),
          pv: t('plan.pv'),
          load: t('plan.load'),
          battery: t('plan.battery'),
          soc: t('plan.soc'),
          charge: t('plan.charge'),
          discharge: t('plan.discharge'),
        }"
      />
      <div class="d-flex flex-wrap gap-3 small text-muted mt-2">
        <span>{{ t('plan.charge') }}: {{ fmtEnergy(plan.summary.charge_kwh) }} kWh</span>
        <span>{{ t('plan.discharge') }}: {{ fmtEnergy(plan.summary.discharge_kwh) }} kWh</span>
        <span v-if="plan.summary.slot_count === 0">{{ t('plan.noSlots') }}</span>
      </div>
    </section>

    <section class="aipt-card mb-3">
      <h2>{{ t('plan.slotsTitle') }}</h2>
      <div v-if="!hasSlots" class="empty-state">
        {{ plan.no_plan_reason || t('plan.noSlots') }}
      </div>
      <div v-else class="table-responsive">
        <table class="table table-sm align-middle plan-table">
          <thead>
            <tr>
              <th scope="col">{{ t('plan.time') }}</th>
              <th scope="col">{{ t('plan.direction') }}</th>
              <th scope="col" class="text-end d-none d-md-table-cell">{{ t('plan.power') }}</th>
              <th scope="col" class="text-end">{{ t('plan.energy') }}</th>
              <th scope="col" class="text-end d-none d-md-table-cell">{{ t('plan.avgPrice') }}</th>
              <th scope="col" class="text-end d-none d-md-table-cell">{{ t('plan.expectedSaving') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="slot in plan.slots" :key="slot.start">
              <td>{{ fmtTime(slot.start) }}–{{ fmtTime(slot.end) }}</td>
              <td>
                <span class="badge-soft" :class="slot.direction === 'charge' ? 'badge-charge' : 'badge-discharge'">
                  {{ slot.direction === 'charge' ? t('plan.charge') : t('plan.discharge') }}
                </span>
              </td>
              <td class="text-end d-none d-md-table-cell">{{ fmtEnergy(slot.power_w) }} W</td>
              <td class="text-end">{{ fmtEnergy(slot.energy_kwh) }} kWh</td>
              <td class="text-end d-none d-md-table-cell">{{ slot.avg_import_price.toFixed(3) }}</td>
              <td class="text-end d-none d-md-table-cell">{{ fmtMoney(slot.expected_saving) }} {{ currency }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="plan.warnings?.length" class="aipt-card">
      <h2>{{ t('plan.warnings') }}</h2>
      <p v-for="warning in plan.warnings ?? []" :key="warning" class="small text-muted mb-1">{{ warning }}</p>
    </section>
    <p class="small text-muted mt-3">{{ t('plan.dryRun') }}</p>
  </template>

  <section v-else class="aipt-card">
    <div class="empty-state">
      <p class="mb-2">{{ t('plan.notReady') }}</p>
      <button class="btn btn-primary" type="button" :disabled="recomputing" @click="recompute">
        {{ t('plan.recompute') }}
      </button>
    </div>
  </section>
</template>
