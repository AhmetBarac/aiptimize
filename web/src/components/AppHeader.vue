<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { currentLocale, setLocale, SUPPORTED_LOCALES, type Locale } from '../i18n'
import { session, signOut } from '../session'
import { theme, toggleTheme } from '../theme'

const { t } = useI18n()
const router = useRouter()
const locale = computed(() => currentLocale())
const armed = computed(() => session.meta?.gates.writes_armed === true)

function changeLocale(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  if (value === 'en' || value === 'sv') {
    setLocale(value as Locale)
  }
}

async function logout(): Promise<void> {
  await signOut()
  await router.push({ name: 'login' })
}
</script>

<template>
  <header class="app-header">
    <div class="header-inner">
      <router-link to="/" class="brand">
        <span class="brand-mark" aria-hidden="true"><i class="bi bi-lightning-charge-fill"></i></span>
        <span class="brand-text">
          <strong>{{ t('app.name') }}</strong>
          <small>{{ t('app.tagline') }}</small>
        </span>
      </router-link>
      <div class="header-actions">
        <span class="gate-pill header-gate" :class="armed ? 'gate-on' : 'gate-off'">
          <i class="bi" :class="armed ? 'bi-shield-check' : 'bi-shield-lock'"></i>
          <span class="d-none d-sm-inline">
            {{ armed ? t('dashboard.armed') : t('dashboard.disarmed') }}
          </span>
        </span>
        <button
          class="icon-button"
          type="button"
          :title="t('common.switchTheme')"
          :aria-label="t('common.switchTheme')"
          @click="toggleTheme"
        >
          <i class="bi" :class="theme === 'dark' ? 'bi-sun' : 'bi-moon-stars'"></i>
        </button>
        <select
          class="form-select form-select-sm locale-select"
          :value="locale"
          :aria-label="t('common.language')"
          @change="changeLocale"
        >
          <option v-for="item in SUPPORTED_LOCALES" :key="item" :value="item">
            {{ item.toUpperCase() }}
          </option>
        </select>
        <button
          class="icon-button"
          type="button"
          :title="t('common.logout')"
          :aria-label="t('common.logout')"
          @click="logout"
        >
          <i class="bi bi-box-arrow-right"></i>
        </button>
      </div>
    </div>
  </header>
</template>
