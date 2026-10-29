<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '../api/client'
import { session, signIn } from '../session'
import LocaleSwitch from '../components/LocaleSwitch.vue'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

onMounted(() => {
  if (session.error) {
    error.value = t('errors.network')
  }
})

async function submit(): Promise<void> {
  busy.value = true
  error.value = ''
  try {
    await signIn(username.value, password.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.replace(next)
  } catch (err) {
    if (err instanceof ApiError) {
      error.value = err.code === 'network_error' ? t('errors.network') : t('auth.invalid')
    } else {
      error.value = t('common.error')
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-shell">
    <LocaleSwitch />
    <div class="auth-wrap">
      <div class="auth-brand">
        <span class="brand-mark" aria-hidden="true"><i class="bi bi-lightning-charge-fill"></i></span>
        <div>
          <strong>{{ t('app.name') }}</strong>
          <div class="text-muted small">{{ t('app.tagline') }}</div>
        </div>
      </div>

      <div class="auth-card">
        <h1>{{ t('auth.loginTitle') }}</h1>
        <p class="lead">{{ t('auth.loginSubtitle') }}</p>

        <form novalidate @submit.prevent="submit">
          <div class="mb-3">
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
          </div>
          <div class="mb-3">
            <label class="form-label" for="password">{{ t('auth.password') }}</label>
            <input
              id="password"
              v-model="password"
              class="form-control"
              type="password"
              autocomplete="current-password"
              required
            />
          </div>

          <div v-if="error" class="alert alert-danger py-2 small" role="alert">{{ error }}</div>

          <button class="btn btn-primary w-100" type="submit" :disabled="busy">
            <span v-if="busy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ busy ? t('auth.signingIn') : t('auth.signIn') }}
          </button>
        </form>
      </div>
    </div>
  </div>
</template>
