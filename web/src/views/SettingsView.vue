<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ApiError } from '../api/client'
import type { SettingsDocument } from '../api/types'
import { loadSettings, patchSettings, session } from '../session'
import PageHead from '../components/PageHead.vue'

const { t } = useI18n()
const route = useRoute()

const sections = ['site', 'safety', 'system'] as const
const section = computed(() => {
  const value = String(route.params.section ?? 'site')
  return (sections as readonly string[]).includes(value) ? value : 'site'
})

const settings = ref<SettingsDocument | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref('')

const siteForm = reactive({
  name: '',
  timezone: '',
  latitude: 0,
  longitude: 0,
  maxImport: 0,
})

const safetyForm = reactive({
  writes: false,
  ev: false,
  watchdog: 1800,
  horizon: 2,
})

onMounted(load)

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const doc = await loadSettings()
    settings.value = doc
    siteForm.name = doc.site.name
    siteForm.timezone = doc.site.timezone
    siteForm.latitude = doc.site.latitude
    siteForm.longitude = doc.site.longitude
    siteForm.maxImport = doc.site.max_import_power_w
    safetyForm.writes = doc.safety.writes_armed_ui
    safetyForm.ev = doc.safety.ev_control_armed_ui
    safetyForm.watchdog = doc.safety.watchdog_clear_after_s
    safetyForm.horizon = doc.safety.horizon_limit_days
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.loadFailed')
  } finally {
    loading.value = false
  }
}

async function saveSite(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('site', {
      name: siteForm.name,
      timezone: siteForm.timezone,
      latitude: Number(siteForm.latitude),
      longitude: Number(siteForm.longitude),
      max_import_power_w: Number(siteForm.maxImport),
    })
    saved.value = t('settings.siteSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveSafety(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('safety', {
      writes_armed_ui: safetyForm.writes,
      ev_control_armed_ui: safetyForm.ev,
      watchdog_clear_after_s: Number(safetyForm.watchdog),
      horizon_limit_days: Number(safetyForm.horizon),
    })
    saved.value = t('settings.safetySaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

function onOff(value: boolean | undefined): string {
  return value ? t('settings.on') : t('settings.off')
}
</script>

<template>
  <PageHead :title="t('settings.title')" :subtitle="t('settings.subtitle')" />

  <nav class="settings-nav" aria-label="Settings sections">
    <router-link
      v-for="item in sections"
      :key="item"
      :to="`/settings/${item}`"
      :class="{ active: section === item }"
    >
      {{ t(`settings.sections.${item}`) }}
    </router-link>
  </nav>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="saved" class="alert alert-success" role="status">{{ saved }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="settings">
    <section v-if="section === 'site'" class="aipt-card">
      <h2>{{ t('settings.sections.site') }}</h2>
      <form novalidate @submit.prevent="saveSite">
        <div class="row g-3">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="name">{{ t('settings.siteName') }}</label>
            <input id="name" v-model="siteForm.name" class="form-control" type="text" required />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="tz">{{ t('settings.timezone') }}</label>
            <input id="tz" v-model="siteForm.timezone" class="form-control" type="text" required />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lat">{{ t('settings.latitude') }}</label>
            <input id="lat" v-model.number="siteForm.latitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lon">{{ t('settings.longitude') }}</label>
            <input id="lon" v-model.number="siteForm.longitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="max-import">{{ t('settings.maxImport') }}</label>
            <input
              id="max-import"
              v-model.number="siteForm.maxImport"
              class="form-control"
              type="number"
              min="1"
              step="100"
            />
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSite') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'safety'" class="aipt-card">
      <h2>{{ t('settings.sections.safety') }}</h2>
      <p class="text-muted small">{{ t('settings.safetyIntro') }}</p>
      <form novalidate @submit.prevent="saveSafety">
        <div class="form-check form-switch py-2">
          <input id="writes" v-model="safetyForm.writes" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="writes">{{ t('settings.writesArm') }}</label>
        </div>
        <div class="form-check form-switch py-2">
          <input id="ev" v-model="safetyForm.ev" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="ev">{{ t('settings.evArm') }}</label>
        </div>
        <div class="row g-3 mt-2">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="watchdog">{{ t('settings.watchdog') }}</label>
            <input id="watchdog" v-model.number="safetyForm.watchdog" class="form-control" type="number" min="60" step="60" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="horizon">{{ t('settings.horizon') }}</label>
            <input id="horizon" v-model.number="safetyForm.horizon" class="form-control" type="number" min="1" max="2" />
          </div>
        </div>
        <div class="mt-3 small text-muted">
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.writes') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.evControl') }}</dt>
            <dd>{{ onOff(session.meta?.gates.ev_control_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.effective') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_armed) }}</dd>
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSafety') }}
        </button>
      </form>
    </section>

    <section v-else class="aipt-card">
      <h2>{{ t('settings.sections.system') }}</h2>
      <p class="text-muted small">{{ t('settings.systemIntro') }}</p>
      <dl class="mb-0">
        <div class="key-value">
          <dt>{{ t('common.version') }}</dt>
          <dd class="mono">{{ session.meta?.version ?? '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.inverter') }}</dt>
          <dd>{{ settings.inverter.type }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.host') }}</dt>
          <dd class="mono">
            {{ settings.inverter.host || t('settings.notConfigured') }}:{{ settings.inverter.port }}
          </dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.batteryCapacity') }}</dt>
          <dd>{{ settings.inverter.capacity_kwh }} kWh</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxCharge') }}</dt>
          <dd>{{ settings.inverter.max_charge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxDischarge') }}</dt>
          <dd>{{ settings.inverter.max_discharge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.area') }}</dt>
          <dd>{{ settings.price.area }} · {{ settings.price.currency }}</dd>
        </div>
      </dl>
    </section>
  </template>
</template>
