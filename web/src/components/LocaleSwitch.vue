<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { currentLocale, setLocale, SUPPORTED_LOCALES, type Locale } from '../i18n'

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
  <select
    class="form-select form-select-sm auth-locale"
    :value="locale"
    :aria-label="t('common.language')"
    @change="changeLocale"
  >
    <option v-for="item in SUPPORTED_LOCALES" :key="item" :value="item">
      {{ item.toUpperCase() }}
    </option>
  </select>
</template>
