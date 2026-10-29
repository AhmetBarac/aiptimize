<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { currentLocale, setLocale, SUPPORTED_LOCALES, type Locale } from '../i18n'
import { theme, toggleTheme } from '../theme'

const { t } = useI18n()
const locale = computed(() => currentLocale())

function changeLocale(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  if (value === 'en' || value === 'sv') {
    setLocale(value as Locale)
  }
}
</script>

<template>
  <div class="auth-locale">
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
  </div>
</template>
