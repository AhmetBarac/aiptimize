<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ApiError } from '../api/client'
import { completeSetup } from '../session'
import LocaleSwitch from '../components/LocaleSwitch.vue'

const { t } = useI18n()
const router = useRouter()

const timezones = [
  'Europe/Stockholm',
  'Europe/Oslo',
  'Europe/Copenhagen',
  'Europe/Helsinki',
  'Europe/Berlin',
  'Europe/London',
  'Europe/Amsterdam',
  'UTC',
]
const detected = Intl.DateTimeFormat().resolvedOptions().timeZone
if (detected && !timezones.includes(detected)) {
  timezones.unshift(detected)
}

const username = ref('admin')
const password = ref('')
const siteName = ref('Home')
const timezone = ref(detected && detected !== 'UTC' ? detected : 'Europe/Stockholm')
const latitude = ref(59.3293)
const longitude = ref(18.0686)
// Inverter connection: the Modbus port belongs in first-run setup, because a
// wrong port is the most common reason a new installation reads nothing.
const inverterType = ref('solplanet_ai_dongle')
const inverterHost = ref('')
const inverterPort = ref(8484)
const inverterUnit = ref(0)
const busy = ref(false)
const error = ref('')

// The two families listen on very different ports; move to the new default when
// the type changes and the port is still on the old one.
function onInverterTypeChange(): void {
  if (inverterType.value === 'solplanet_ai_dongle') {
    if (inverterPort.value === 502 || inverterPort.value === 6607) inverterPort.value = 8484
    return
  }
  if (inverterPort.value === 8484) inverterPort.value = 502
}

async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await completeSetup({
      username: username.value.trim(),
      password: password.value,
      site: {
        name: siteName.value.trim(),
        timezone: timezone.value,
        latitude: Number(latitude.value),
        longitude: Number(longitude.value),
      },
      ...(inverterHost.value.trim()
        ? {
            inverter: {
              type: inverterType.value,
              host: inverterHost.value.trim(),
              port: Number(inverterPort.value),
              unit_id: Number(inverterUnit.value) || 0,
              scheme: inverterType.value === 'solplanet_ai_dongle' ? 'http' : 'http',
            },
          }
        : {}),
    })
    await router.replace('/')
  } catch (err) {
    if (err instanceof ApiError) {
      error.value = err.code === 'network_error' ? t('errors.network') : err.message
    } else {
      error.value = t('auth.setupError')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-shell">
    <LocaleSwitch />
    <div class="auth-wrap" style="max-width: 560px">
      <div class="auth-brand">
        <span class="brand-mark" aria-hidden="true"><i class="bi bi-lightning-charge-fill"></i></span>
        <div>
          <strong>{{ t('app.name') }}</strong>
          <div class="text-muted small">{{ t('app.tagline') }}</div>
        </div>
      </div>

      <div class="auth-card">
        <span class="badge-soft mb-2 d-inline-block">{{ t('auth.firstRun') }}</span>
        <h1>{{ t('auth.setupTitle') }}</h1>
        <p class="lead">{{ t('auth.setupSubtitle') }}</p>

        <form novalidate @submit.prevent="submit">
          <h2 class="section-title mt-0">{{ t('auth.adminSection') }}</h2>
          <div class="row g-3">
            <div class="col-12 col-sm-6">
              <label class="form-label" for="username">{{ t('auth.username') }}</label>
              <input
                id="username"
                v-model="username"
                class="form-control"
                type="text"
                autocomplete="username"
                autocapitalize="none"
                spellcheck="false"
                required
              />
              <div class="form-text">{{ t('auth.usernameHint') }}</div>
            </div>
            <div class="col-12 col-sm-6">
              <label class="form-label" for="password">{{ t('auth.password') }}</label>
              <input
                id="password"
                v-model="password"
                class="form-control"
                type="password"
                autocomplete="new-password"
                minlength="8"
                required
              />
              <div class="form-text">{{ t('auth.passwordHint') }}</div>
            </div>
          </div>

          <h2 class="section-title">{{ t('auth.siteSection') }}</h2>
          <div class="row g-3">
            <div class="col-12 col-sm-6">
              <label class="form-label" for="site-name">{{ t('auth.siteName') }}</label>
              <input id="site-name" v-model="siteName" class="form-control" type="text" required />
            </div>
            <div class="col-12 col-sm-6">
              <label class="form-label" for="timezone">{{ t('auth.timezone') }}</label>
              <select id="timezone" v-model="timezone" class="form-select">
                <option v-for="zone in timezones" :key="zone" :value="zone">{{ zone }}</option>
              </select>
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="latitude">{{ t('auth.latitude') }}</label>
              <input
                id="latitude"
                v-model.number="latitude"
                class="form-control"
                type="number"
                step="0.0001"
                min="-90"
                max="90"
                required
              />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="longitude">{{ t('auth.longitude') }}</label>
              <input
                id="longitude"
                v-model.number="longitude"
                class="form-control"
                type="number"
                step="0.0001"
                min="-180"
                max="180"
                required
              />
            </div>
          </div>

          <h2 class="section-title">{{ t('auth.inverterSection') }}</h2>
          <p class="text-muted small">{{ t('auth.inverterHint') }}</p>
          <div class="row g-3">
            <div class="col-12 col-sm-5">
              <label class="form-label" for="setup-inverter-type">{{ t('settings.inverterType') }}</label>
              <select
                id="setup-inverter-type"
                v-model="inverterType"
                class="form-select"
                @change="onInverterTypeChange"
              >
                <option value="solplanet_ai_dongle">{{ t('settings.inverterTypeSolplanet') }}</option>
                <option value="saj">{{ t('settings.inverterTypeSaj') }}</option>
                <option value="huawei">{{ t('settings.inverterTypeHuawei') }}</option>
              </select>
            </div>
            <div class="col-12 col-sm-4">
              <label class="form-label" for="setup-inverter-host">{{ t('settings.host') }}</label>
              <input
                id="setup-inverter-host"
                v-model="inverterHost"
                class="form-control mono"
                type="text"
                placeholder="192.0.2.10"
              />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="setup-inverter-port">{{ t('settings.port') }}</label>
              <input
                id="setup-inverter-port"
                v-model.number="inverterPort"
                class="form-control"
                type="number"
                min="1"
                max="65535"
              />
            </div>
            <div v-if="inverterType !== 'solplanet_ai_dongle'" class="col-6 col-sm-3">
              <label class="form-label" for="setup-inverter-unit">{{ t('settings.unitId') }}</label>
              <input
                id="setup-inverter-unit"
                v-model.number="inverterUnit"
                class="form-control"
                type="number"
                min="0"
                max="255"
              />
            </div>
            <p v-if="inverterType === 'huawei'" class="small text-muted mb-0">
              {{ t('auth.inverterPortHintHuawei') }}
            </p>
            <p v-else-if="inverterType === 'saj'" class="small text-muted mb-0">
              {{ t('auth.inverterPortHintSaj') }}
            </p>
          </div>

          <div v-if="error" class="alert alert-danger py-2 small mt-3" role="alert">{{ error }}</div>

          <button class="btn btn-primary w-100 mt-4" type="submit" :disabled="busy">
            <span v-if="busy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ busy ? t('auth.creating') : t('auth.createAdmin') }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>
