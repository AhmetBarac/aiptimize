<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type { DraftSlot, Plan, PlanApplyResult, PlanSlot, PlanValidation, PlanView, PriceBlock } from '../api/types'
import { loadMeta, session } from '../session'
import PlanChart from '../components/PlanChart.vue'
import PlanTimeline from '../components/PlanTimeline.vue'
import { addDays, formatPrice, localDateKey, localTimeKey } from '../lib/energy'
import { basePlanID, baseRevision } from '../lib/plan'

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
const clearError = ref('')
const clearMessage = ref('')
const durationOptions = [30, 60, 90, 120, 150, 180, 210, 240]
const startTimeOptions = Array.from({ length: 48 }, (_, index) => index * 30)

function formatDuration(minutes: number): string {
  return `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`
}

function slotTime(slot: EditSlot): string {
  return slot.start.slice(11, 16)
}

function setSlotTime(slot: EditSlot, event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  slot.start = `${view.value?.date ?? slot.start.slice(0, 10)}T${value}`
}

const powerEditorIndex = ref<number | null>(null)
const powerDraft = ref(100)

function openPowerPicker(index: number): void {
  const current = Number(editSlots.value[index]?.power ?? 100)
  powerDraft.value = Math.min(8000, Math.max(100, Math.round(current / 50) * 50))
  powerEditorIndex.value = index
}

function closePowerPicker(): void {
  powerEditorIndex.value = null
}

function applyPowerPicker(): void {
  const index = powerEditorIndex.value
  if (index === null) return
  const slot = editSlots.value[index]
  if (slot) slot.power = powerDraft.value
  closePowerPicker()
}
const editing = ref(false)
const editSlots = ref<EditSlot[]>([])
const validation = ref<PlanValidation>({})
const note = ref('')
const priceBlocks = ref<PriceBlock[]>([])
const selectedIndex = ref(-1)

const plan = computed<Plan | null>(() => view.value?.draft ?? view.value?.effective ?? view.value?.auto ?? null)
const canWrite = computed(() => Boolean(session.meta?.gates?.writes_armed))
const currency = computed(() => plan.value?.summary.currency ?? '')
const draft = computed(() => view.value?.draft ?? null)
const sync = computed(() => view.value?.sync ?? null)
const todayDate = ref('')
const daySlots = computed(() => {
  const day = view.value?.date
  if (!day) return plan.value?.slots ?? []
  return (plan.value?.slots ?? []).filter((slot) => slot.day_local === day)
})
const hasSlots = computed(() => daySlots.value.length > 0)
const timezone = computed(() => plan.value?.timezone ?? session.meta?.timezone ?? 'UTC')
const timelineSlots = computed<PlanSlot[]>(() => {
  if (!editing.value) return daySlots.value
  const day = view.value?.date ?? ''
  return editSlots.value.map((slot) => {
    const start = new Date(slot.start)
    const end = new Date(start.getTime() + Number(slot.duration) * 60_000)
    return {
      day_local: day,
      start: start.toISOString(),
      end: end.toISOString(),
      direction: slot.direction,
      power_w: Math.round(Number(slot.power)),
      energy_kwh: (Number(slot.power) * Number(slot.duration)) / 60_000,
      avg_import_price: 0,
      expected_saving: 0,
    }
  })
})
const localErrors = computed<string[]>(() => {
  if (!editing.value || !view.value) return []
  const errors: string[] = []
  const tz = timezone.value
  const day = view.value.date
  const slots = editSlots.value.map((slot, index) => {
    const start = new Date(slot.start)
    const duration = Number(slot.duration)
    return {
      index,
      start,
      end: new Date(start.getTime() + duration * 60_000),
      duration,
      power: Number(slot.power),
    }
  })
  for (const slot of slots) {
    const label = `${t('plan.slots')} ${slot.index + 1}`
    if (Number.isNaN(slot.start.getTime())) {
      errors.push(`${label}: ${t('plan.invalidStart')}`)
      continue
    }
    if (localDateKey(slot.start.toISOString(), tz) !== day) {
      errors.push(`${label}: ${t('plan.outsideDay')}`)
    }
    if (slot.start.getMinutes() % 30 !== 0 || slot.start.getSeconds() !== 0) {
      errors.push(`${label}: ${t('plan.invalidSlotStart')}`)
    }
    if (localDateKey(slot.end.toISOString(), tz) !== day && localTimeKey(slot.end.toISOString(), tz) !== '00:00') {
      errors.push(`${label}: ${t('plan.crossesMidnight')}`)
    }
    if (slot.duration < 30 || slot.duration > 240 || slot.duration % 30 !== 0) {
      errors.push(`${label}: ${t('plan.invalidDuration')}`)
    }
    if (slot.power <= 0) {
      errors.push(`${label}: ${t('plan.invalidPower')}`)
    } else if (!Number.isInteger(slot.power)) {
      errors.push(`${label}: ${t('plan.invalidPowerWhole')}`)
    } else if (slot.power < 100 || slot.power > 8000) {
      errors.push(`${label}: ${t('plan.invalidPowerRange')}`)
    }
  }
  const sorted = [...slots].sort((a, b) => a.start.getTime() - b.start.getTime())
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i].start < sorted[i - 1].end) {
      errors.push(`${t('plan.overlap', { a: sorted[i - 1].index + 1, b: sorted[i].index + 1 })}`)
    }
  }
  return errors
})
const allErrors = computed(() => [...(validation.value.errors ?? []), ...localErrors.value])
const timelineLabels = computed(() => ({
  charge: t('plan.charge'),
  discharge: t('plan.discharge'),
  now: t('home.now'),
  empty: t('home.noSlotsShort'),
}))
const nowIso = new Date().toISOString()
const selectedDayLabel = computed(() => {
  if (!view.value) return ''
  if (view.value.date === todayDate.value) return t('plan.today')
  if (view.value.date === tomorrowDate.value) return t('plan.tomorrow')
  return view.value.date
})
const tomorrowDate = computed(() => {
  const base =
    todayDate.value ||
    view.value?.date ||
    localDateKey(new Date(), session.meta?.timezone ?? 'UTC')
  return addDays(base, 1)
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

async function loadPrices(): Promise<void> {
  try {
    const snapshot = await api.get<{ blocks: PriceBlock[] }>('/prices')
    priceBlocks.value = snapshot.blocks
  } catch {
    // The timeline still works without the price background.
  }
}

function displaySlotPrice(slot: PlanSlot): number {
  if (session.meta?.site.show_full_price === true) return slot.avg_import_price
  const at = Date.parse(slot.start)
  const block = priceBlocks.value.find(
    (item) => Date.parse(item.start) <= at && Date.parse(item.end) > at,
  )
  return block?.price_kwh ?? slot.avg_import_price
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
  editSlots.value = source.slots
    .filter((slot) => !view.value?.date || slot.day_local === view.value.date)
    .map((slot) => ({
    start: toLocalInput(slot.start, plan.value?.timezone ?? 'UTC'),
    duration: Math.max(30, Math.round((Date.parse(slot.end) - Date.parse(slot.start)) / 60000)),
    direction: slot.direction,
    power: Math.round(slot.power_w),
  }))
  validation.value = {}
  note.value = ''
  message.value = ''
  selectedIndex.value = -1
  editing.value = true
}

function cancelEdit(): void {
  editing.value = false
  validation.value = {}
  note.value = ''
  selectedIndex.value = -1
}

function addSlotAt(payload: { start: string }): void {
  if (!plan.value || !view.value) return
  const start = new Date(payload.start)
  editSlots.value.push({
    start: toLocalInput(start.toISOString(), timezone.value),
    duration: 30,
    direction: 'charge',
    power: Math.min(3000, Math.round(Number(plan.value.inputs?.charge_w ?? 3000) || 3000)),
  })
  selectedIndex.value = editSlots.value.length - 1
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
  addSlotAt({ start: start.toISOString() })
}

function updateTimelineSlot(payload: { index: number; start: string; duration: number }): void {
  const slot = editSlots.value[payload.index]
  if (!slot) return
  slot.start = toLocalInput(payload.start, timezone.value)
  slot.duration = Math.max(30, Math.min(240, Math.round(payload.duration)))
}

function selectTimelineSlot(index: number): void {
  selectedIndex.value = index
}

function removeSlot(index: number): void {
  editSlots.value.splice(index, 1)
  if (selectedIndex.value === index) selectedIndex.value = -1
  if (selectedIndex.value > index) selectedIndex.value -= 1
}

function slotPayload(): DraftSlot[] {
  return editSlots.value.map((slot) => {
    const start = new Date(slot.start)
    const end = new Date(start.getTime() + Number(slot.duration) * 60_000)
    return {
      start: start.toISOString(),
      end: end.toISOString(),
      direction: slot.direction,
      power_w: Math.round(Number(slot.power)),
    }
  })
}

function validationFromApi(err: ApiError): PlanValidation {
  const details = err.details as { validation?: PlanValidation } | undefined
  return details?.validation ?? { errors: [err.message], warnings: [] }
}

async function saveDraft(): Promise<void> {
  if (!view.value) return
  if (localErrors.value.length) {
    error.value = t('plan.draftInvalid')
    return
  }
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const revision = baseRevision(view.value)
    const planID = basePlanID(view.value)
    const response = await api.post<{ plan: Plan; validation: PlanValidation }>('/plan/drafts', {
      date: view.value.date,
      revision,
      base_plan_id: planID,
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
    } else if (err instanceof ApiError && err.code === 'revision_conflict') {
      // Reload so the next save carries the current revision; the edited
      // slots stay in place.
      await load(view.value.date)
      error.value = t('plan.revisionConflict')
    } else {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    busy.value = false
  }
}

async function checkSync(): Promise<void> {
  busy.value = true
  try {
    await load(view.value?.date)
  } finally {
    busy.value = false
  }
}

async function downloadPlan(): Promise<void> {
  if (!view.value) return
  if (!window.confirm(t('plan.confirmDownload'))) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const result = draft.value
      ? await api.post<PlanApplyResult>(`/plan/drafts/${draft.value.id}/apply`, {
          confirm: true,
          revision: draft.value.revision,
        })
      : await api.post<PlanApplyResult>(`/plan/download?date=${view.value.date}`, { confirm: true })
    message.value = result.message
    await load(view.value.date)
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

const manualControlBusy = ref(false)
const manualControlError = ref('')

async function toggleManualControl(): Promise<void> {
  if (!view.value || manualControlBusy.value) return
  manualControlBusy.value = true
  manualControlError.value = ''
  try {
    view.value = await api.post<PlanView>('/plan/control', {
      locked: !view.value.locked,
      date: view.value.date,
    })
    await load(view.value.date)
  } catch (err) {
    manualControlError.value = err instanceof Error ? err.message : String(err)
  } finally {
    manualControlBusy.value = false
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
  clearError.value = ''
  clearMessage.value = ''
  message.value = ''
  try {
    const result = await api.post<PlanApplyResult>('/schedule/clear', { confirm: true })
    message.value = result.message
    clearMessage.value = t('plan.scheduleCleared')
    await load(view.value?.date)
  } catch (err) {
    clearError.value = err instanceof Error ? err.message : String(err)
    error.value = clearError.value
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  void loadMeta()
  void load()
  void loadPrices()
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
  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="message" class="alert alert-success" role="status">{{ message }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="view && plan">
    <section class="plan-hero plan-hero-card mb-3">
      <header class="plan-hero-top">
        <div>
          <p class="hero-kicker">{{ t('plan.subtitle') }}</p>
          <h1>{{ t('plan.title') }}</h1>
        </div>
        <div class="plan-hero-actions">
          <button v-if="!editing" class="btn btn-outline-secondary btn-sm" type="button" :disabled="busy" @click="recompute">
            <i class="bi bi-arrow-repeat" aria-hidden="true"></i>
            <span class="ms-1">{{ t('plan.recompute') }}</span>
          </button>
          <button v-if="editing" class="btn btn-outline-secondary btn-sm" type="button" @click="cancelEdit">
            {{ t('common.cancel') }}
          </button>
        </div>
      </header>

      <div class="plan-hero-controls">
        <div class="plan-hero-day">
          <div class="day-tabs">
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
          <span class="plan-hero-date">{{ selectedDayLabel }} · {{ view.date }}</span>
        </div>
      </div>

      <div class="plan-hero-body">
        <div class="plan-hero-main">
          <span class="hero-kicker">{{ t('plan.savings') }}</span>
          <strong class="plan-hero-value tabular" :class="{ 'text-success': plan.summary.savings > 0 }">
            {{ fmtMoney(plan.summary.savings) }}<small>{{ currency }}</small>
          </strong>
          <span class="plan-hero-note">{{ t('plan.cost') }} {{ fmtMoney(plan.summary.cost) }} {{ currency }} · {{ t('plan.baseline') }} {{ fmtMoney(plan.summary.baseline_cost) }} {{ currency }}</span>
        </div>
        <div class="plan-hero-side">
          <div><span>{{ t('plan.slots') }}</span><strong>{{ daySlots.length }}</strong></div>
          <div><span>{{ t('plan.cycles') }}</span><strong>{{ plan.summary.cycles.toFixed(2) }}</strong></div>
          <div><span>{{ t('plan.charge') }}</span><strong>{{ fmtEnergy(plan.summary.charge_kwh) }} <small>kWh</small></strong></div>
        </div>
      </div>

      <div class="plan-status-chips plan-hero-status">
        <span class="badge-soft" :class="view.applied ? 'badge-on' : view.draft ? 'badge-draft' : ''">
          {{ view.applied ? t('plan.statusApplied') : view.draft ? t('plan.statusDraft') : t('plan.statusAuto') }}
        </span>
        <span v-if="view.locked" class="badge-soft badge-warn">{{ t('plan.manualLock') }}</span>
        <span v-if="view.foreign_schedule" class="badge-soft badge-warn">{{ t('plan.foreign') }}</span>
        <span v-else-if="!view.inverter_match" class="badge-soft badge-warn">{{ t('plan.inverterDiffers') }}</span>
        <span v-if="!canWrite" class="badge-soft">{{ t('plan.writesDisarmed') }}</span>
      </div>
    </section>

    <section class="aipt-card plan-slots-card mb-3">
      <div class="card-head">
        <div>
          <p class="card-kicker">{{ t('plan.slotsTitle') }}</p>
          <h2>{{ selectedDayLabel }}</h2>
        </div>
        <span class="small text-muted">{{ view.date }}</span>
      </div>

      <PlanTimeline
        :slots="timelineSlots"
        :day="view.date"
        :timezone="plan.timezone"
        :locale="locale"
        :now="nowIso"
        :prices="priceBlocks"
        :full-price="session.meta?.site.show_full_price === true"
        :editable="editing"
        :selected-index="selectedIndex"
        :labels="timelineLabels"
        @select-slot="selectTimelineSlot"
        @update-slot="updateTimelineSlot"
        @add-slot="addSlotAt"
      />
      <p v-if="editing" class="timeline-edit-hint">{{ t('plan.timelineEditHint') }}</p>

      <div v-if="editing">
        <div v-if="allErrors.length" class="alert alert-danger mt-3" role="alert">
          <div v-for="issue in allErrors" :key="issue">{{ issue }}</div>
        </div>
        <div v-if="validation.warnings?.length" class="alert alert-warning mt-3" role="alert">
          <div v-for="issue in validation.warnings" :key="issue">{{ issue }}</div>
        </div>
        <div
          v-for="(slot, index) in editSlots"
          :key="index"
          class="string-card"
          :class="{ selected: selectedIndex === index }"
          @focusin="selectedIndex = index"
        >
          <div class="row g-2 align-items-end">
            <div class="col-12 col-md-4">
              <label class="form-label" :for="`slot-start-${index}`">
                {{ t('plan.start') }} <span class="text-muted">· {{ view.date }}</span>
              </label>
              <select
                :id="`slot-start-${index}`"
                class="form-select"
                :value="slotTime(slot)"
                @change="setSlotTime(slot, $event)"
              >
                <option v-for="minutes in startTimeOptions" :key="minutes" :value="formatDuration(minutes)">
                  {{ formatDuration(minutes) }}
                </option>
              </select>
            </div>
            <div class="col-6 col-md-2">
              <label class="form-label" :for="`slot-duration-${index}`">{{ t('plan.duration') }}</label>
              <select :id="`slot-duration-${index}`" v-model.number="slot.duration" class="form-select">
                <option v-for="minutes in durationOptions" :key="minutes" :value="minutes">{{ formatDuration(minutes) }}</option>
              </select>
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
              <button
                :id="`slot-power-${index}`"
                type="button"
                class="form-control power-picker-button"
                aria-haspopup="dialog"
                @click="openPowerPicker(index)"
              >
                <span>{{ Math.round(slot.power) }} W</span>
                <i class="bi bi-sliders" aria-hidden="true"></i>
              </button>
            </div>
            <div class="col-6 col-md-2">
              <button class="btn btn-outline-secondary w-100" type="button" @click="removeSlot(index)">
                <i class="bi bi-trash" aria-hidden="true"></i>
                <span class="d-none d-md-inline ms-1">{{ t('plan.remove') }}</span>
              </button>
            </div>
          </div>
        </div>
        <div class="mt-3">
          <label class="form-label" for="draft-note">{{ t('plan.note') }}</label>
          <input id="draft-note" v-model="note" class="form-control" type="text" maxlength="200" />
        </div>
        <div class="plan-card-actions">
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
            <tr v-for="slot in daySlots" :key="slot.start">
              <td>{{ fmtTime(slot.start) }}–{{ fmtTime(slot.end) }}</td>
              <td>
                <span class="badge-soft" :class="slot.direction === 'charge' ? 'badge-charge' : 'badge-discharge'">
                  {{ slot.direction === 'charge' ? t('plan.charge') : t('plan.discharge') }}
                </span>
              </td>
              <td class="text-end d-none d-md-table-cell">{{ Math.round(slot.power_w) }} W</td>
              <td class="text-end">{{ fmtEnergy(slot.energy_kwh) }} kWh</td>
              <td class="text-end d-none d-md-table-cell">{{ formatPrice(displaySlotPrice(slot), locale) }}</td>
              <td class="text-end d-none d-md-table-cell">{{ fmtMoney(slot.expected_saving) }} {{ currency }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="!editing" class="plan-card-actions">
        <div class="plan-primary-actions">
          <button v-if="plan" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="startEdit">
            <i class="bi bi-pencil" aria-hidden="true"></i>
            <span class="ms-1">{{ t('plan.edit') }}</span>
          </button>
          <button v-if="plan && canWrite" class="btn btn-sm btn-primary" type="button" :disabled="busy" @click="downloadPlan">
            <i class="bi bi-download" aria-hidden="true"></i>
            <span class="ms-1">{{ t('plan.downloadPlan') }}</span>
          </button>
        </div>
        <button v-if="draft" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="discardDraft">
          {{ t('plan.discardDraft') }}
        </button>
        <button v-if="view.foreign_schedule && canWrite" class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="takeControl">
          {{ t('plan.takeControl') }}
        </button>
      </div>
    </section>

    <section class="aipt-card plan-inverter-card mb-3">
      <div class="card-head">
        <div>
          <p class="card-kicker">{{ t('settings.inverter') }}</p>
          <h2>{{ t('plan.inverterControl') }}</h2>
        </div>
      </div>
      <div class="manual-control-row plan-manual-control">
        <div class="manual-control-label">
          <strong>{{ t('plan.manualControl') }}</strong>
          <small>{{ view.locked ? t('plan.manualControlOn') : t('plan.manualControlOff') }}</small>
        </div>
        <button
          type="button"
          class="ios-switch"
          :class="{ on: view.locked }"
          role="switch"
          :aria-checked="view.locked"
          :aria-label="t('plan.manualControl')"
          :disabled="manualControlBusy"
          @click="toggleManualControl"
        >
          <span class="ios-switch-knob"></span>
        </button>
      </div>
      <p v-if="manualControlError" class="text-danger small mb-0" role="alert">{{ manualControlError }}</p>

      <div class="inverter-control-list">
        <div v-if="view.locked && canWrite" class="inverter-control-row">
          <p class="plan-inverter-hint">{{ t('plan.useAutomaticHint') }}</p>
          <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="useAutomatic">
            {{ t('plan.useAutomatic') }}
          </button>
        </div>
        <div class="inverter-control-row">
          <p class="plan-inverter-hint">{{ t('plan.clearScheduleHint') }}</p>
          <button class="btn btn-sm btn-outline-danger" type="button" :disabled="busy" @click="clearSchedule">
            {{ t('plan.clearSchedule') }}
          </button>
        </div>
        <p v-if="clearError" class="text-danger small mb-2" role="alert">{{ clearError }}</p>
        <p v-else-if="clearMessage" class="text-success small mb-2" role="status">{{ clearMessage }}</p>
      </div>
    </section>

    <section class="aipt-card plan-forecast-card mb-3">
      <div class="card-head">
        <div>
          <p class="card-kicker">{{ t('plan.chartTitle') }}</p>
          <h2>{{ t('forecast.title') }}</h2>
        </div>
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
        <h2 class="mb-0">{{ t('plan.syncTitle') }}</h2>
        <span class="badge-soft" :class="sync ? (sync.match ? 'badge-on' : 'badge-warn') : ''">
          {{ sync ? (sync.match ? t('plan.syncInSync') : t('plan.syncOutOfSync')) : t('plan.syncUnknown') }}
        </span>
      </div>
      <template v-if="sync">
        <p class="small text-muted mt-2 mb-1">{{ t('plan.syncChecked', { time: fmtTime(sync.ts) }) }}</p>
        <p v-if="sync.plan_id" class="small text-muted mb-1">
          {{ t('plan.syncCompared', { id: sync.plan_id, revision: sync.plan_revision }) }}
        </p>
        <ul v-if="sync.diff.length" class="small text-muted mb-1">
          <li v-for="diff in sync.diff" :key="diff">{{ diff }}</li>
        </ul>
        <p v-else class="small text-muted mb-1">{{ t('plan.syncMatches') }}</p>
        <p v-if="draft" class="small text-muted mb-1">{{ t('plan.syncDraftHint') }}</p>
      </template>
      <p v-else class="small text-muted mt-2 mb-1">{{ t('plan.syncNever') }}</p>
      <p v-if="view.last_write" class="small text-muted mb-1">
        {{ t('plan.lastWrite', { time: fmtTime(view.last_write.ts), kind: view.last_write.kind }) }}
      </p>
      <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="checkSync">
        <i class="bi bi-arrow-repeat" aria-hidden="true"></i>
        <span class="ms-1">{{ t('plan.syncCheckNow') }}</span>
      </button>
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

  <Teleport to="body">
    <div
      v-if="powerEditorIndex !== null"
      class="power-picker-backdrop"
      tabindex="-1"
      @click.self="closePowerPicker"
      @keydown.esc="closePowerPicker"
    >
      <div class="power-picker-modal" role="dialog" aria-modal="true" :aria-label="t('plan.powerPickerTitle')">
        <p class="card-kicker">{{ t('plan.power') }}</p>
        <h2>{{ t('plan.powerPickerTitle') }}</h2>
        <div class="power-picker-value">{{ powerDraft }} <small>W</small></div>
        <input
          v-model.number="powerDraft"
          class="power-picker-range"
          type="range"
          min="100"
          max="8000"
          step="50"
          autofocus
        />
        <div class="power-picker-scale">
          <span>100 W</span>
          <span>{{ t('plan.powerPickerRange') }}</span>
          <span>8000 W</span>
        </div>
        <div class="power-picker-actions">
          <button class="btn btn-outline-secondary" type="button" @click="closePowerPicker">
            {{ t('common.cancel') }}
          </button>
          <button class="btn btn-primary" type="button" @click="applyPowerPicker">
            {{ t('common.save') }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
