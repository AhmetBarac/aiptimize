<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type { SchedulePayload, SettingsDocument, Status } from '../api/types'
import { patchSettings } from '../session'
import MetricCard from '../components/MetricCard.vue'
import PageHead from '../components/PageHead.vue'

const { t } = useI18n()
const status = ref<Status | null>(null)
const schedule = ref<SchedulePayload | null>(null)
const settings = ref<SettingsDocument | null>(null)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const message = ref('')
const forceDirection = ref<'charge' | 'discharge' | ''>('')
const limits = reactive({
  minSoc: 10,
  maxSoc: 100,
  reserveSoc: 10,
  maxChargeW: 6000,
  maxDischargeW: 3000,
  endSoc: 50,
  preserveSoc: true,
})
const force = reactive({
  powerW: 3000,
  durationMin: 60,
})

const sample = computed(() => status.value?.telemetry.sample ?? null)
const inverterMode = computed(() => sample.value?.inverter_mode ?? 0)
const canWrite = computed(() => status.value?.gates.writes_armed === true)
const batteryPower = computed(() => sample.value?.battery_w ?? 0)
const batteryHint = computed(() => {
  if (batteryPower.value > 50) return t('battery.charging')
  if (batteryPower.value < -50) return t('battery.discharging')
  return t('battery.idle')
})
const capacity = computed(() => settings.value?.inverter.capacity_kwh ?? 0)
const modeLabel = computed(() => {
  if (inverterMode.value === 1) return t('battery.modeOffGrid')
  if (inverterMode.value === 2) return t('battery.modeSelfConsumption')
  if (inverterMode.value === 3) return t('battery.modeReserve')
  if (inverterMode.value === 4) return t('battery.modeCustom')
  return t('battery.modeUnknown')
})
const dayOrder = ['Mon', 'Tus', 'Wen', 'Thu', 'Fri', 'Sat', 'Sun']
const scheduleDays = computed(() =>
  dayOrder.map((key) => ({ key, slots: schedule.value?.schedule.days[key] ?? [] })),
)

function fmtTime(hour: number, minute: number): string {
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}

function dayLabel(key: string): string {
  return t(`battery.days.${key}`)
}

function directionLabel(discharge: boolean): string {
  return discharge ? t('battery.discharge') : t('battery.charge')
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const [nextStatus, nextSchedule, nextSettings] = await Promise.all([
      api.get<Status>('/status'),
      api.get<SchedulePayload>('/schedule').catch(() => null),
      api.get<SettingsDocument>('/settings'),
    ])
    status.value = nextStatus
    schedule.value = nextSchedule
    settings.value = nextSettings
    limits.minSoc = nextSettings.inverter.min_soc
    limits.maxSoc = nextSettings.inverter.max_soc
    limits.reserveSoc = nextSettings.inverter.reserve_soc
    limits.maxChargeW = nextSettings.inverter.max_charge_w
    limits.maxDischargeW = nextSettings.inverter.max_discharge_w
    limits.endSoc = nextSettings.plan.end_soc_target
    limits.preserveSoc = nextSettings.plan.preserve_soc
    force.powerW = Math.round(nextSettings.inverter.max_charge_w || 3000)
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('battery.loadFailed')
  } finally {
    loading.value = false
  }
}

async function setMode(mode: 'self_consumption' | 'reserve' | 'custom' | 'off_grid'): Promise<void> {
  if (!canWrite.value) return
  if (mode === 'off_grid' && !window.confirm(t('battery.confirmOffGrid'))) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api.post('/battery/mode', { mode })
    message.value = t(`battery.${mode}Applied`)
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('battery.modeFailed')
  } finally {
    busy.value = false
  }
}

async function saveLimits(): Promise<void> {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await patchSettings('inverter', {
      min_soc: Number(limits.minSoc),
      max_soc: Number(limits.maxSoc),
      reserve_soc: Number(limits.reserveSoc),
      max_charge_w: Math.round(Number(limits.maxChargeW)),
      max_discharge_w: Math.round(Number(limits.maxDischargeW)),
    })
    await patchSettings('plan', {
      end_soc_target: Number(limits.endSoc),
      preserve_soc: limits.preserveSoc,
    })
    message.value = t('battery.limitsSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('battery.saveFailed')
  } finally {
    busy.value = false
  }
}

async function applyForce(): Promise<void> {
  if (!forceDirection.value || !canWrite.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api.post('/battery/force', {
      direction: forceDirection.value,
      power_w: Number(force.powerW),
      duration_min: Number(force.durationMin),
    })
    message.value = t('battery.forceApplied')
    forceDirection.value = ''
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('battery.forceFailed')
  } finally {
    busy.value = false
  }
}

async function clearSchedule(): Promise<void> {
  if (!canWrite.value || !window.confirm(t('battery.confirmClear'))) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api.post('/schedule/clear', { confirm: true })
    await api.post('/battery/mode', { mode: 'self_consumption' })
    message.value = t('battery.scheduleCleared')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('battery.clearFailed')
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <PageHead :title="t('battery.title')" :subtitle="t('battery.subtitle')">
    <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="load">
      <i class="bi bi-arrow-clockwise" aria-hidden="true"></i>
      <span class="ms-1">{{ t('common.retry') }}</span>
    </button>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="message" class="alert alert-success" role="status">{{ message }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="status">
    <div class="metric-grid mb-3">
      <MetricCard :label="t('battery.soc')" :value="sample ? Math.round(sample.soc).toString() : '—'" unit="%" icon="bi-battery-half" tone="battery" />
      <MetricCard :label="t('battery.power')" :value="Math.round(Math.abs(batteryPower)).toString()" unit="W" icon="bi-lightning-charge" :tone="batteryPower >= 0 ? 'battery' : 'neutral'" :hint="batteryHint" />
      <MetricCard :label="t('battery.workMode')" :value="modeLabel" icon="bi-sliders" tone="neutral" />
      <MetricCard :label="t('battery.capacity')" :value="capacity.toFixed(1)" unit="kWh" icon="bi-battery-full" tone="neutral" />
    </div>

    <div v-if="!canWrite" class="alert alert-warning py-2 small" role="status">{{ t('battery.writesDisarmed') }}</div>

    <section class="aipt-card mb-3">
      <h2>{{ t('battery.workModeTitle') }}</h2>
      <p class="small text-muted">{{ t('battery.workModeHint') }}</p>
      <div class="mode-grid">
        <button class="mode-card" :class="{ active: inverterMode === 2 }" type="button" :disabled="!canWrite || busy" @click="setMode('self_consumption')">
          <span class="mode-icon"><i class="bi bi-house-door" aria-hidden="true"></i></span>
          <strong>{{ t('battery.modeSelfConsumption') }}</strong>
          <span>{{ t('battery.modeSelfConsumptionHint') }}</span>
        </button>
        <button class="mode-card" :class="{ active: inverterMode === 3 }" type="button" :disabled="!canWrite || busy" @click="setMode('reserve')">
          <span class="mode-icon"><i class="bi bi-shield-check" aria-hidden="true"></i></span>
          <strong>{{ t('battery.modeReserve') }}</strong>
          <span>{{ t('battery.modeReserveHint') }}</span>
        </button>
        <button class="mode-card" :class="{ active: inverterMode === 4 }" type="button" :disabled="!canWrite || busy" @click="setMode('custom')">
          <span class="mode-icon"><i class="bi bi-calendar3-week" aria-hidden="true"></i></span>
          <strong>{{ t('battery.modeCustom') }}</strong>
          <span>{{ t('battery.modeCustomHint') }}</span>
        </button>
        <button class="mode-card" :class="{ active: inverterMode === 1 }" type="button" :disabled="!canWrite || busy" @click="setMode('off_grid')">
          <span class="mode-icon"><i class="bi bi-plug" aria-hidden="true"></i></span>
          <strong>{{ t('battery.modeOffGrid') }}</strong>
          <span>{{ t('battery.modeOffGridHint') }}</span>
        </button>
        <button class="mode-card" :class="{ active: forceDirection === 'charge' }" type="button" :disabled="!canWrite || busy" @click="forceDirection = 'charge'">
          <span class="mode-icon"><i class="bi bi-battery-charging" aria-hidden="true"></i></span>
          <strong>{{ t('battery.forceCharge') }}</strong>
          <span>{{ t('battery.forceChargeHint') }}</span>
        </button>
        <button class="mode-card" :class="{ active: forceDirection === 'discharge' }" type="button" :disabled="!canWrite || busy" @click="forceDirection = 'discharge'">
          <span class="mode-icon"><i class="bi bi-battery" aria-hidden="true"></i></span>
          <strong>{{ t('battery.forceDischarge') }}</strong>
          <span>{{ t('battery.forceDischargeHint') }}</span>
        </button>
      </div>

      <form v-if="forceDirection" class="row g-3 mt-3" novalidate @submit.prevent="applyForce">
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-force-power">{{ t('battery.forcePower') }}</label>
          <input id="battery-force-power" v-model.number="force.powerW" class="form-control" type="number" min="100" step="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-force-duration">{{ t('battery.forceDuration') }}</label>
          <select id="battery-force-duration" v-model.number="force.durationMin" class="form-select">
            <option v-for="minutes in [30, 60, 90, 120, 180, 240]" :key="minutes" :value="minutes">{{ minutes }} min</option>
          </select>
        </div>
        <div class="col-12 col-sm-3 d-flex align-items-end">
          <button class="btn btn-primary" type="submit" :disabled="busy">
            {{ forceDirection === 'charge' ? t('battery.startCharge') : t('battery.startDischarge') }}
          </button>
        </div>
      </form>
    </section>

    <section class="aipt-card mb-3">
      <h2>{{ t('battery.limitsTitle') }}</h2>
      <p class="small text-muted">{{ t('battery.limitsHint') }}</p>
      <form class="row g-3" novalidate @submit.prevent="saveLimits">
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-min-soc">{{ t('battery.minSoc') }}</label>
          <input id="battery-min-soc" v-model.number="limits.minSoc" class="form-control" type="number" min="0" max="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-max-soc">{{ t('battery.maxSoc') }}</label>
          <input id="battery-max-soc" v-model.number="limits.maxSoc" class="form-control" type="number" min="0" max="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-reserve-soc">{{ t('battery.reserveSoc') }}</label>
          <input id="battery-reserve-soc" v-model.number="limits.reserveSoc" class="form-control" type="number" min="0" max="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-end-soc">{{ t('battery.endSoc') }}</label>
          <input id="battery-end-soc" v-model.number="limits.endSoc" class="form-control" type="number" min="0" max="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-max-charge">{{ t('battery.maxCharge') }}</label>
          <input id="battery-max-charge" v-model.number="limits.maxChargeW" class="form-control" type="number" min="0" step="100" />
        </div>
        <div class="col-6 col-sm-3">
          <label class="form-label" for="battery-max-discharge">{{ t('battery.maxDischarge') }}</label>
          <input id="battery-max-discharge" v-model.number="limits.maxDischargeW" class="form-control" type="number" min="0" step="100" />
        </div>
        <div class="col-12 col-sm-6 d-flex align-items-end">
          <div class="form-check form-switch mb-2">
            <input id="battery-preserve-soc" v-model="limits.preserveSoc" class="form-check-input" type="checkbox" role="switch" />
            <label class="form-check-label" for="battery-preserve-soc">{{ t('battery.preserveSoc') }}</label>
          </div>
        </div>
        <div class="col-12">
          <button class="btn btn-primary" type="submit" :disabled="busy">{{ t('battery.saveLimits') }}</button>
        </div>
      </form>
    </section>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2">
        <div>
          <h2 class="mb-0">{{ t('battery.scheduleTitle') }}</h2>
          <p class="small text-muted mb-0">{{ t('battery.scheduleHint') }}</p>
        </div>
        <div class="d-flex gap-2">
          <RouterLink to="/plan" class="btn btn-sm btn-outline-secondary">{{ t('battery.editSchedule') }}</RouterLink>
          <button class="btn btn-sm btn-outline-danger" type="button" :disabled="!canWrite || busy" @click="clearSchedule">{{ t('battery.clearSchedule') }}</button>
        </div>
      </div>
      <div class="table-responsive mt-3">
        <table class="table table-sm plan-table mb-0">
          <thead>
            <tr>
              <th>{{ t('battery.day') }}</th>
              <th>{{ t('battery.windows') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="day in scheduleDays" :key="day.key">
              <td>{{ dayLabel(day.key) }}</td>
              <td>
                <span v-if="!day.slots.length" class="text-muted">—</span>
                <div
                  v-for="slot in day.slots"
                  :key="`${day.key}-${slot.hour}-${slot.minute}-${slot.discharge}`"
                  class="badge-soft d-inline-block me-1 mb-1"
                >
                  {{ fmtTime(slot.hour, slot.minute) }}–{{ slot.duration_min }} min · {{ directionLabel(slot.discharge) }}
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </template>
</template>
