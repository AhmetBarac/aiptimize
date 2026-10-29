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
const busy = ref(false)
const error = ref('')

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
