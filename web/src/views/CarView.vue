<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type { ChargerPayload, ChargerPlanPayload, ChargerStatus, EVConfig, EVPlan, EVPreview } from '../api/types'
import { session } from '../session'
import MetricCard from '../components/MetricCard.vue'
import PageHead from '../components/PageHead.vue'
import PlanPreview from '../components/PlanPreview.vue'
import Sheet from '../components/Sheet.vue'

const { t } = useI18n()
const charger = ref<ChargerPayload | null>(null)
const evConfig = ref<EVConfig | null>(null)
const plans = ref<EVPlan[]>([])
const preview = ref<EVPreview | null>(null)
const loading = ref(true)
const busy = ref(false)
const pendingCommand = ref(false)
const commandPhase = ref<'sending' | 'waiting'>('sending')
const error = ref('')
const message = ref('')
const currentA = ref(6)
const targetKwh = ref(20)
const deadline = ref(defaultDeadline())
const strategy = ref('cheapest')
const precondition = ref(0)
// Two-step plan: "Create plan" renders a preview, "Save plan" persists exactly
// the settings that were previewed.
const draft = ref<EVPreview | null>(null)
// Saved-plan viewer: the eye button opens one of the stored plans and shows its
// preview, from where it can be scheduled.
const viewing = ref<EVPlan | null>(null)
const viewOpen = ref(false)
const viewPreview = ref<EVPreview | null>(null)
const draftKey = ref('')
let timer: ReturnType<typeof setInterval> | undefined

const canControl = computed(() => Boolean(session.meta?.gates?.ev_control_armed))
const evRestartRequired = computed(() => session.meta?.ev?.restart_required === true)
const evProvider = computed(() => session.meta?.ev?.active_provider || session.meta?.ev?.provider || '')
const configuredProvider = computed(
  () => evConfig.value?.provider || session.meta?.ev?.provider || 'zaptec',
)
const activeProvider = computed(
  () => evConfig.value?.active_provider || session.meta?.ev?.active_provider || '',
)
const hasZaptecCredentials = computed(() => evConfig.value?.credentials.zaptec === true)
const status = computed(() => charger.value?.status ?? null)
const mode = computed(() => status.value?.mode ?? 'off')

function planInput(): Record<string, unknown> {
  return {
    target_energy_kwh: Number(targetKwh.value),
    deadline: deadline.value,
    precondition_min: Number(precondition.value),
    strategy: strategy.value,
  }
}

const planKey = computed(() => JSON.stringify(planInput()))
// Saving is only allowed for settings the user has just previewed.
const canSavePlan = computed(() => Boolean(draft.value) && draftKey.value === planKey.value)

function defaultDeadline(): string {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  d.setHours(7, 0, 0, 0)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function load(): Promise<void> {
  try {
    evConfig.value = await api.get<EVConfig>('/ev/config').catch(() => null)
    const list = await api.get<{ chargers: ChargerPayload[] }>('/chargers')
    charger.value = list.chargers[0] ?? null
    if (charger.value) {
      currentA.value = Math.round(charger.value.status.current_a || charger.value.config.min_current_a)
      const plan = await api.get<ChargerPlanPayload>(`/chargers/${charger.value.id}/plan`)
      plans.value = plan.plans ?? []
      preview.value = plan.preview ?? null
    }
    error.value = ''
  } catch (err) {
    if (!hasZaptecCredentials.value || (err instanceof ApiError && err.status === 503)) {
      charger.value = null
      error.value = ''
    } else {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } finally {
    loading.value = false
  }
}

async function refreshStatus(): Promise<void> {
  if (!charger.value) return
  try {
    charger.value.status = await api.get<ChargerStatus>(`/chargers/${charger.value.id}/status`)
  } catch {
    // keep the last status
  }
}

async function setMode(next: string): Promise<void> {
  if (!charger.value) return
  busy.value = true
  error.value = ''
  try {
    charger.value.status = await api.post(`/chargers/${charger.value.id}/mode`, { mode: next })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function setCurrent(): Promise<void> {
  if (!charger.value) return
  busy.value = true
  error.value = ''
  try {
    charger.value.status = await api.post(`/chargers/${charger.value.id}/current`, { amps: Number(currentA.value) })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

const COMMAND_TIMEOUT_MS = 10_000
const COMMAND_POLL_MS = 1_000

// Zaptec accepts the command before the charger acts on it, so the indicator
// has to follow the reported state rather than the HTTP response.
async function waitForCharger(paused: boolean): Promise<boolean> {
  const id = charger.value?.id
  if (!id) return false
  const deadline = Date.now() + COMMAND_TIMEOUT_MS
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, COMMAND_POLL_MS))
    if (!charger.value) return false
    const next = await api.get<ChargerStatus>(`/chargers/${id}/status`).catch(() => null)
    if (!next) continue
    charger.value.status = next
    if (next.paused === paused) return true
  }
  return false
}

async function pauseOrResume(): Promise<void> {
  if (!charger.value) return
  const expectPaused = !status.value?.paused
  busy.value = true
  pendingCommand.value = true
  commandPhase.value = 'sending'
  error.value = ''
  message.value = ''
  try {
    charger.value.status = await api.post(
      `/chargers/${charger.value.id}/${expectPaused ? 'pause' : 'resume'}`,
      {},
    )
    commandPhase.value = 'waiting'
    if (!(await waitForCharger(expectPaused))) {
      message.value = t('car.commandUnconfirmed')
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
    pendingCommand.value = false
    commandPhase.value = 'sending'
  }
}

async function createPlan(): Promise<void> {
  if (!charger.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const input = planInput()
    draft.value = await api.post<EVPreview>(`/chargers/${charger.value.id}/plan/preview`, input)
    draftKey.value = JSON.stringify(input)
  } catch (err) {
    draft.value = null
    draftKey.value = ''
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

function discardDraft(): void {
  draft.value = null
  draftKey.value = ''
  message.value = ''
}

async function savePlan(): Promise<void> {
  if (!charger.value || !canSavePlan.value) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await api.post<{ plan: EVPlan; preview: EVPreview }>(`/chargers/${charger.value.id}/plan/static`, {
      ...planInput(),
      active: true,
    })
    preview.value = result.preview
    draft.value = null
    draftKey.value = ''
    message.value = t('car.planSaved')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function openPlan(plan: EVPlan): Promise<void> {
  if (!charger.value) return
  viewing.value = plan
  viewPreview.value = null
  viewOpen.value = true
  error.value = ''
  try {
    viewPreview.value = await api.post<EVPreview>(`/chargers/${charger.value.id}/plan/preview`, {
      target_energy_kwh: plan.target_energy_kwh,
      deadline: plan.deadline_local,
      precondition_min: plan.precondition_min,
      strategy: plan.strategy,
    })
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function scheduleViewed(): Promise<void> {
  const plan = viewing.value
  if (!charger.value || !plan) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await api.post(`/chargers/${charger.value.id}/plan/static/activate?id=${plan.id}`, {})
    viewOpen.value = false
    viewing.value = null
    message.value = t('car.planActivated')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function deletePlan(plan: EVPlan): Promise<void> {
  if (!charger.value || !window.confirm(t('car.confirmDelete'))) return
  busy.value = true
  try {
    await api.del(`/chargers/${charger.value.id}/plan/static?id=${plan.id}`)
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void refreshStatus(), 15_000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <PageHead :title="t('car.title')" :subtitle="t('car.subtitle')">
    <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="load">
      <i class="bi bi-arrow-clockwise" aria-hidden="true"></i>
      <span class="ms-1">{{ t('common.retry') }}</span>
    </button>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="evRestartRequired" class="alert alert-warning py-2 small" role="status">
    {{ t('car.restartRequired') }}
  </div>
  <div v-if="message" class="alert alert-success" role="status">{{ message }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="charger && status">
    <div class="metric-grid mb-3">
      <MetricCard :label="t('car.state')" :value="status.connected ? (status.charging ? t('car.charging') : t('car.connected')) : t('car.unplugged')" icon="bi-ev-station" tone="car" :hint="canControl ? t('home.liveNow') : t('car.controlDisarmed')" />
      <MetricCard :label="t('car.power')" :value="String(Math.round(status.power_w))" unit="W" icon="bi-lightning-charge" tone="battery" />
      <MetricCard :label="t('car.current')" :value="status.current_a.toFixed(0)" unit="A" icon="bi-sliders" tone="neutral" />
      <MetricCard :label="t('car.session')" :value="status.session_kwh.toFixed(1)" unit="kWh" icon="bi-battery-charging" tone="neutral" />
      <MetricCard :label="t('car.mode')" :value="mode.toUpperCase()" icon="bi-stars" tone="neutral" />
    </div>

    <section v-if="status.connected" class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2">
        <h2 class="mb-0">{{ t('car.sessionTitle') }}</h2>
        <span class="badge-soft" :class="status.paused ? 'badge-warn' : 'badge-on'">
          {{ status.paused ? t('car.sessionPaused') : t('car.sessionActive') }}
        </span>
      </div>
      <p class="small text-muted mt-2">{{ t('car.sessionHint') }}</p>
      <button
        class="btn btn-outline-secondary"
        type="button"
        :disabled="!canControl || busy"
        :aria-busy="pendingCommand"
        @click="pauseOrResume"
      >
        <i v-if="pendingCommand" class="bi bi-arrow-repeat spin" aria-hidden="true"></i>
        <i v-else class="bi" :class="status.paused ? 'bi-play-fill' : 'bi-pause-fill'" aria-hidden="true"></i>
        <span class="ms-1">
          {{
            pendingCommand
              ? commandPhase === 'waiting'
                ? t('car.awaitingCommand')
                : t('car.sendingCommand')
              : status.paused
                ? t('car.resume')
                : t('car.pause')
          }}
        </span>
      </button>
    </section>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-3">
        <h2 class="mb-0">{{ t('car.modeTitle') }}</h2>
        <span v-if="evProvider" class="badge-soft">{{ evProvider }}</span>
        <span v-if="!canControl" class="badge-soft badge-warn">{{ t('car.controlDisarmed') }}</span>
      </div>
      <div class="mode-grid">
        <button
          v-for="option in ['off', 'now', 'smart']"
          :key="option"
          class="mode-card"
          :class="{ active: mode === option }"
          type="button"
          :aria-pressed="mode === option"
          :disabled="!canControl || busy"
          @click="setMode(option)"
        >
          <span class="mode-icon">
            <i class="bi" :class="option === 'off' ? 'bi-power' : option === 'now' ? 'bi-lightning-charge' : 'bi-stars'" aria-hidden="true"></i>
          </span>
          <strong>{{ t(`car.modes.${option}`) }}</strong>
          <span>{{ t(`car.modeHints.${option}`) }}</span>
        </button>
      </div>
      <div class="row g-3 mt-2 align-items-end">
        <div class="col-auto">
          <label class="form-label" for="ev-current">{{ t('car.current') }} (A)</label>
          <input
            id="ev-current"
            v-model.number="currentA"
            class="form-control ev-current-input"
            type="number"
            inputmode="numeric"
            :min="charger.config.min_current_a"
            :max="charger.config.max_current_a"
            step="1"
          />
        </div>
        <div class="col">
          <button class="btn btn-outline-secondary w-100" type="button" :disabled="!canControl || busy" @click="setCurrent">
            {{ t('car.applyCurrent') }}
          </button>
        </div>
      </div>
      <p class="small text-muted mt-3 mb-0">{{ t('car.cadence') }}</p>
    </section>

    <section class="aipt-card mb-3">
      <div class="d-flex flex-wrap justify-content-between align-items-center gap-2">
        <h2 class="mb-0">{{ t('car.planTitle') }}</h2>
        <span v-if="preview" class="badge-soft" :class="preview.state === 'unreachable' ? 'badge-warn' : 'badge-on'">{{ preview.state }}</span>
      </div>
      <div v-if="preview" class="mt-3">
        <PlanPreview :preview="preview" />
      </div>
      <div v-else class="empty-state">{{ t('car.noPlan') }}</div>
    </section>

    <section class="aipt-card mb-3">
      <h2>{{ t('car.newPlan') }}</h2>
      <form novalidate @submit.prevent="savePlan">
        <div class="row g-3">
          <div class="col-5 col-sm-3">
            <label class="form-label" for="ev-target">{{ t('car.target') }} (kWh)</label>
            <input id="ev-target" v-model.number="targetKwh" class="form-control" type="number" min="0.5" step="0.5" />
          </div>
          <div class="col-7 col-sm-3">
            <label class="form-label" for="ev-deadline">{{ t('car.deadline') }}</label>
            <input id="ev-deadline" v-model="deadline" class="form-control" type="datetime-local" />
          </div>
          <div class="col-6 col-sm-2">
            <label class="form-label" for="ev-strategy">{{ t('car.strategy') }}</label>
            <select id="ev-strategy" v-model="strategy" class="form-select">
              <option value="cheapest">{{ t('car.cheapest') }}</option>
              <option value="continuous">{{ t('car.continuous') }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-2">
            <label class="form-label" for="ev-precondition">{{ t('car.precondition') }}</label>
            <input id="ev-precondition" v-model.number="precondition" class="form-control" type="number" min="0" max="240" step="5" />
          </div>
          <div class="col-12 col-sm-2 d-flex align-items-end gap-2">
            <button class="btn btn-outline-primary flex-fill" type="button" :disabled="busy" @click="createPlan">
              {{ t('car.createPlan') }}
            </button>
            <button class="btn btn-primary flex-fill" type="submit" :disabled="busy || !canSavePlan">
              {{ t('car.savePlan') }}
            </button>
          </div>
        </div>
      </form>

      <div v-if="draft" class="mt-4 pt-3 border-top">
        <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
          <h3 class="h6 mb-0">{{ t('car.draftTitle') }}</h3>
          <button class="btn btn-sm btn-outline-secondary" type="button" :disabled="busy" @click="discardDraft">
            {{ t('car.discard') }}
          </button>
        </div>
        <PlanPreview :preview="draft" />
        <p class="small text-muted mb-0">{{ canSavePlan ? t('car.draftHint') : t('car.draftStale') }}</p>
      </div>
      <div v-if="plans.length" class="mt-3">
        <div v-for="plan in plans" :key="plan.id" class="key-value plan-row">
          <dt>
            {{ plan.target_energy_kwh }} kWh · {{ plan.deadline_local }} · {{ plan.strategy }}
            <span v-if="plan.active" class="badge-soft badge-on ms-1">{{ t('car.planActive') }}</span>
          </dt>
          <dd class="d-flex gap-1">
            <button
              class="btn btn-sm btn-outline-secondary"
              type="button"
              :disabled="busy"
              :aria-label="t('car.viewPlan')"
              :title="t('car.viewPlan')"
              @click="openPlan(plan)"
            >
              <i class="bi bi-eye" aria-hidden="true"></i>
            </button>
            <button
              class="btn btn-sm btn-outline-secondary"
              type="button"
              :disabled="busy"
              :aria-label="t('car.delete')"
              :title="t('car.delete')"
              @click="deletePlan(plan)"
            >
              <i class="bi bi-trash" aria-hidden="true"></i>
            </button>
          </dd>
        </div>
      </div>
    </section>

    <p class="small text-muted">{{ t('car.limitation') }}</p>

    <Sheet
      v-model:open="viewOpen"
      :title="t('car.viewTitle')"
      :subtitle="viewing ? `${viewing.target_energy_kwh} kWh · ${viewing.deadline_local} · ${viewing.strategy}` : ''"
      size="sm"
    >
      <PlanPreview v-if="viewPreview" :preview="viewPreview" />
      <p v-else class="small text-muted mb-0">{{ t('common.loading') }}</p>
      <template #footer>
        <button
          class="btn btn-primary w-100"
          type="button"
          :disabled="busy || viewing?.active"
          @click="scheduleViewed"
        >
          {{ viewing?.active ? t('car.planActive') : t('car.scheduleViewed') }}
        </button>
      </template>
    </Sheet>
  </template>

  <section v-else class="aipt-card">
    <h2>{{ t('car.evSetupTitle') }}</h2>
    <p class="text-muted small">{{ !hasZaptecCredentials ? t('car.evSetupHint') : t('car.unavailable') }}</p>
    <div class="key-value">
      <dt>{{ t('settings.evProvider') }}</dt>
      <dd>{{ configuredProvider }}</dd>
    </div>
    <div class="key-value">
      <dt>{{ t('car.activeProvider') }}</dt>
      <dd>{{ activeProvider || t('car.notActive') }}</dd>
    </div>
    <div class="key-value">
      <dt>{{ t('settings.evZaptecCredentials') }}</dt>
      <dd>{{ hasZaptecCredentials ? t('settings.evConfigured') : t('settings.evNotConfigured') }}</dd>
    </div>
    <RouterLink class="btn btn-primary mt-3" to="/settings/ev">{{ t('car.configureEV') }}</RouterLink>
  </section>
</template>
