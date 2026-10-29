<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type { DraftSlot, Plan, PlanApplyResult, PlanValidation, PlanView } from '../api/types'
import { session } from '../session'
import PageHead from '../components/PageHead.vue'
import PlanChart from '../components/PlanChart.vue'

interface EditSlot {
  start: string
  duration: number
  direction: 'charge' | 'discharge'
  power: number
}

const { t, locale } = useI18n()
const view = ref<PlanView | null>(null)
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const message = ref('')
const editing = ref(false)
const editSlots = ref<EditSlot[]>([])
const validation = ref<PlanValidation>({})
const note = ref('')

const plan = computed<Plan | null>(() => view.value?.draft ?? view.value?.effective ?? view.value?.auto ?? null)
const canWrite = computed(() => Boolean(session.meta?.gates?.writes_armed))
const currency = computed(() => plan.value?.summary.currency ?? '')
const hasSlots = computed(() => (plan.value?.slots.length ?? 0) > 0)
const draft = computed(() => view.value?.draft ?? null)
const todayDate = ref('')
const tomorrowDate = computed(() => {
  const base = todayDate.value || view.value?.date || new Date().toISOString().slice(0, 10)
  const d = new Date(`${base}T12:00:00`)
  d.setDate(d.getDate() + 1)
  return d.toISOString().slice(0, 10)
})

async function load(day?: string): Promise<void> {
  try {
    view.value = await api.get<PlanView>(`/plan${day ? `?date=${day}` : ''}`)
    if (!todayDate.value) todayDate.value = view.value.date
    error.value = ''
    validation.value = view.value.validation ?? {}
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      view.value = null
      error.value = ''
    } else {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    loading.value = false
  }
}

async function selectDay(day: string): Promise<void> {
  if (view.value?.date === day) return
  busy.value = true
  editing.value = false
  await load(day)
  busy.value = false
}

async function recompute(): Promise<void> {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api.post<Plan>('/plan/recompute', {})
    editing.value = false
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

function toLocalInput(iso: string, timezone: string): string {
  return new Intl.DateTimeFormat('sv-SE', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: timezone,
  }).format(new Date(iso)).replace(' ', 'T')
}

function startEdit(): void {
  if (!plan.value || !view.value) return
  const source = draft.value ?? plan.value
  editSlots.value = source.slots.map((slot) => ({
    start: toLocalInput(slot.start, plan.value?.timezone ?? 'UTC'),
    duration: Math.max(30, Math.round((Date.parse(slot.end) - Date.parse(slot.start)) / 60000)),
    direction: slot.direction,
    power: slot.power_w,
  }))
  validation.value = {}
  note.value = ''
  message.value = ''
  editing.value = true
}

function cancelEdit(): void {
  editing.value = false
  validation.value = {}
  note.value = ''
}

function addSlot(): void {
  if (!plan.value) return
  const last = editSlots.value[editSlots.value.length - 1]
  let start: Date
  if (last) {
    start = new Date(new Date(last.start).getTime() + last.duration * 60_000)
  } else {
    start = new Date(`${view.value?.date ?? new Date().toISOString().slice(0, 10)}T12:00:00`)
  }
  editSlots.value.push({
    start: toLocalInput(start.toISOString(), plan.value.timezone),
    duration: 30,
    direction: 'charge',
    power: Math.min(3000, Number(plan.value.inputs?.charge_w ?? 3000) || 3000),
  })
}

function removeSlot(index: number): void {
  editSlots.value.splice(index, 1)
}

function slotPayload(): DraftSlot[] {
  return editSlots.value.map((slot) => {
    const start = new Date(slot.start)
    const end = new Date(start.getTime() + Number(slot.duration) * 60_000)
    return {
      start: start.toISOString(),
      end: end.toISOString(),
      direction: slot.direction,
      power_w: Number(slot.power),
    }
  })
}

function validationFromApi(err: ApiError): PlanValidation {
  const details = err.details as { validation?: PlanValidation } | undefined
  return details?.validation ?? { errors: [err.message], warnings: [] }
}

async function saveDraft(): Promise<void> {
  if (!view.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const revision = draft.value?.revision ?? view.value.auto?.revision ?? 0
    const response = await api.post<{ plan: Plan; validation: PlanValidation }>('/plan/drafts', {
      date: view.value.date,
      revision,
      slots: slotPayload(),
      note: note.value,
    })
    validation.value = response.validation
    if (response.validation.errors?.length) {
      error.value = t('plan.draftInvalid')
      return
    }
    message.value = t('plan.draftSaved')
    editing.value = false
    await load()
  } catch (err) {
    if (err instanceof ApiError && err.code === 'draft_invalid') {
      validation.value = validationFromApi(err)
      error.value = t('plan.draftInvalid')
    } else {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    busy.value = false
  }
}

async function resubmit(): Promise<void> {
  if (!draft.value) return
  if (!window.confirm(t('plan.confirmResubmit'))) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await api.post<PlanApplyResult>(`/plan/drafts/${draft.value.id}/apply`, {
      confirm: true,
      revision: draft.value.revision,
    })
    message.value = result.message
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function discardDraft(): Promise<void> {
  if (!draft.value) return
  busy.value = true
  try {
    await api.del(`/plan/drafts/${draft.value.id}`)
    editing.value = false
    message.value = t('plan.draftDiscarded')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function useAutomatic(): Promise<void> {
  if (!window.confirm(t('plan.confirmAuto'))) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await api.post<PlanApplyResult>('/plan/auto/apply', { confirm: true })
    message.value = result.message
    await load(view.value?.date)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function takeControl(): Promise<void> {
  if (!window.confirm(t('plan.confirmTakeover'))) return
  busy.value = true
  error.value = ''
  try {
    const result = await api.post<PlanApplyResult>(`/schedule/takeover?date=${view.value?.date ?? ''}`, { confirm: true })
    message.value = result.message
    await load(view.value?.date)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function clearSchedule(): Promise<void> {
  if (!window.confirm(t('plan.confirmClear'))) return
  busy.value = true
  error.value = ''
  try {
    const result = await api.post<PlanApplyResult>('/schedule/clear', { confirm: true })
    message.value = result.message
    await load(view.value?.date)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
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
</script>

<template>
  <PageHead :title="t('plan.title')" :subtitle="t('plan.subtitle')">
    <button v-if="!editing" class="btn btn-outline-secondary me-2" type="button" :disabled="busy" @click="recompute">
      <i class="bi bi-arrow-repeat" aria-hidden="true"></i>
      <span class="ms-1">{{ t('plan.recompute') }}</span>
    </button>
    <button v-if="!editing && plan" class="btn btn-primary" type="button" @click="startEdit">
      <i class="bi bi-pencil" aria-hidden="true"></i>
      <span class="ms-1">{{ t('plan.edit') }}</span>
    </button>
    <button v-if="editing" class="btn btn-outline-secondary" type="button" @click="cancelEdit">
      {{ t('common.cancel') }}
    </button>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="message" class="alert alert-success" role="status">{{ message }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="view && plan">
    <div class="d-flex flex-wrap gap-2 align-items-center mb-3">
      <div class="day-tabs me-2">
        <button
          class="btn btn-sm"
          :class="view.date === todayDate ? 'btn-primary' : 'btn-outline-secondary'"
          type="button"
          @click="selectDay(todayDate)"
        >
          {{ t('plan.today') }}
        </button>
        <button
          class="btn btn-sm"
          :class="view.date === tomorrowDate ? 'btn-primary' : 'btn-outline-secondary'"
          type="button"
          @click="selectDay(tomorrowDate)"
        >
          {{ t('plan.tomorrow') }}
        </button>
      </div>
      <span class="badge-soft" :class="view.applied ? 'badge-on' : view.draft ? 'badge-warn' : ''">
        {{ view.applied ? t('plan.statusApplied') : view.draft ? t('plan.statusDraft') : t('plan.statusAuto') }}
      </span>
      <span v-if="view.locked" class="badge-soft badge-warn">{{ t('plan.manualLock') }}</span>
      <span v-if="view.foreign_schedule" class="badge-soft badge-warn">{{ t('plan.foreign') }}</span>
      <span v-else-if="!view.inverter_match" class="badge-soft badge-warn">{{ t('plan.inverterDiffers') }}</span>
      <span v-if="!canWrite" class="badge-soft">{{ t('plan.writesDisarmed') }}</span>
    </div>

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
        <strong class="plan-value">{{ plan.slots.length }}<span class="unit">{{ t('plan.perDay') }}</span></strong>
      </section>
      <section class="aipt-card">
        <span class="tile-label">{{ t('plan.cycles') }}</span>
        <strong class="plan-value">{{ plan.summary.cycles.toFixed(2) }}</strong>
      </section>
    </div>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
        <h2 class="mb-0">{{ t('plan.chartTitle') }}</h2>
        <span class="small text-muted">{{ view.date }}</span>
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
      </div>
    </section>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2">
        <h2>{{ t('plan.slotsTitle') }}</h2>
        <div v-if="!editing" class="d-flex flex-wrap gap-2">
          <button v-if="draft && canWrite" class="btn btn-sm btn-primary" type="button" :disabled="busy" @click="resubmit">
            <i class="bi bi-upload" aria-hidden="true"></i>
            <span class="ms-1">{{ t('plan.resubmit') }}</span>
          </button>
          <button v-if="draft" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="discardDraft">
            {{ t('plan.discardDraft') }}
          </button>
          <button v-if="view.locked && canWrite" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="useAutomatic">
            {{ t('plan.useAutomatic') }}
          </button>
          <button v-if="view.foreign_schedule && canWrite" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="takeControl">
            {{ t('plan.takeControl') }}
          </button>
          <button v-if="canWrite" class="btn btn-sm btn-outline-danger" type="button" :disabled="busy" @click="clearSchedule">
            {{ t('plan.clearSchedule') }}
          </button>
        </div>
      </div>

      <div v-if="editing">
        <div v-if="validation.errors?.length" class="alert alert-danger mt-3" role="alert">
          <div v-for="issue in validation.errors" :key="issue">{{ issue }}</div>
        </div>
        <div v-if="validation.warnings?.length" class="alert alert-warning mt-3" role="alert">
          <div v-for="issue in validation.warnings" :key="issue">{{ issue }}</div>
        </div>
        <div v-for="(slot, index) in editSlots" :key="index" class="string-card">
          <div class="row g-2 align-items-end">
            <div class="col-12 col-md-4">
              <label class="form-label" :for="`slot-start-${index}`">{{ t('plan.start') }}</label>
              <input :id="`slot-start-${index}`" v-model="slot.start" class="form-control" type="datetime-local" />
            </div>
            <div class="col-6 col-md-2">
              <label class="form-label" :for="`slot-duration-${index}`">{{ t('plan.duration') }}</label>
              <input :id="`slot-duration-${index}`" v-model.number="slot.duration" class="form-control" type="number" min="30" max="240" step="30" />
            </div>
            <div class="col-6 col-md-2">
              <label class="form-label" :for="`slot-direction-${index}`">{{ t('plan.direction') }}</label>
              <select :id="`slot-direction-${index}`" v-model="slot.direction" class="form-select">
                <option value="charge">{{ t('plan.charge') }}</option>
                <option value="discharge">{{ t('plan.discharge') }}</option>
              </select>
            </div>
            <div class="col-6 col-md-2">
              <label class="form-label" :for="`slot-power-${index}`">{{ t('plan.power') }} (W)</label>
              <input :id="`slot-power-${index}`" v-model.number="slot.power" class="form-control" type="number" min="1" step="100" />
            </div>
            <div class="col-6 col-md-2">
              <button class="btn btn-outline-secondary w-100" type="button" @click="removeSlot(index)">
                <i class="bi bi-trash" aria-hidden="true"></i>
                <span class="d-none d-md-inline ms-1">{{ t('plan.remove') }}</span>
              </button>
            </div>
          </div>
        </div>
        <div class="d-flex flex-wrap gap-2 mt-3">
          <button class="btn btn-outline-secondary" type="button" @click="addSlot">
            <i class="bi bi-plus-lg" aria-hidden="true"></i>
            <span class="ms-1">{{ t('plan.addSlot') }}</span>
          </button>
          <button class="btn btn-primary" type="button" :disabled="busy" @click="saveDraft">
            <span v-if="busy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ t('plan.saveDraft') }}
          </button>
          <button class="btn btn-outline-secondary" type="button" @click="cancelEdit">{{ t('common.cancel') }}</button>
        </div>
        <div class="mt-3">
          <label class="form-label" for="draft-note">{{ t('plan.note') }}</label>
          <input id="draft-note" v-model="note" class="form-control" type="text" maxlength="200" />
        </div>
      </div>

      <div v-else-if="!hasSlots" class="empty-state">
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

    <section v-if="view.schedule_diff.length || view.last_write" class="aipt-card mb-3">
      <h2>{{ t('plan.scheduleState') }}</h2>
      <p v-if="!view.inverter_match" class="small text-muted">{{ t('plan.inverterDiffers') }}</p>
      <ul v-if="view.schedule_diff.length" class="small text-muted">
        <li v-for="diff in view.schedule_diff" :key="diff">{{ diff }}</li>
      </ul>
      <p v-if="view.last_write" class="small text-muted mb-0">
        {{ t('plan.lastWrite', { time: fmtTime(view.last_write.ts), kind: view.last_write.kind }) }}
      </p>
    </section>

    <section v-if="view.warnings.length" class="aipt-card">
      <h2>{{ t('plan.warnings') }}</h2>
      <p v-for="warning in view.warnings" :key="warning" class="small text-muted mb-1">{{ warning }}</p>
    </section>
    <p class="small text-muted mt-3">{{ t('plan.dryRun') }}</p>
  </template>

  <section v-else class="aipt-card">
    <div class="empty-state">
      <p class="mb-2">{{ t('plan.notReady') }}</p>
      <button class="btn btn-primary" type="button" :disabled="busy" @click="recompute">
        {{ t('plan.recompute') }}
      </button>
    </div>
  </section>
</template>
