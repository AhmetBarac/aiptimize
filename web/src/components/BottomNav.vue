<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { currentLocale, setLocale, SUPPORTED_LOCALES, type Locale } from '../i18n'
import { session, signOut } from '../session'
import { theme, toggleTheme } from '../theme'
import { openMore, ui } from '../ui'
import SolarIcon from './SolarIcon.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const primary = [
  { name: 'plan', to: '/plan', icon: 'bi-calendar3-week' },
  { name: 'solar', to: '/solar', icon: 'solar-panel' },
  { name: 'home', to: '/', icon: 'bi-house-door' },
  { name: 'car', to: '/car', icon: 'bi-ev-station' },
]

const secondary = [
  { name: 'history', to: '/history', icon: 'bi-bar-chart' },
  { name: 'settings', to: '/settings', icon: 'bi-gear' },
]

const locale = computed(() => currentLocale())
const moreActive = computed(() =>
  ['/history', '/prices', '/settings'].some((path) => route.path === path || route.path.startsWith(`${path}/`)),
)

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(`${path}/`)
}

function changeLocale(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  if (value === 'en' || value === 'sv') setLocale(value as Locale)
}

async function logout(): Promise<void> {
  await signOut()
  await router.push({ name: 'login' })
}
</script>

<template>
  <nav class="app-nav" :aria-label="t('app.name')">
    <div class="nav-brand desktop-only">
      <i class="bi bi-lightning-charge-fill" aria-hidden="true"></i>
      <span>{{ t('app.name') }}</span>
    </div>

    <div class="nav-links">
      <router-link
        v-for="item in primary"
        :key="item.name"
        :to="item.to"
        class="nav-link"
        :class="[`nav-${item.name}`, { active: isActive(item.to) }]"
        :aria-current="isActive(item.to) ? 'page' : undefined"
      >
        <span class="nav-icon">
          <SolarIcon v-if="item.icon === 'solar-panel'" />
          <i v-else class="bi" :class="item.icon" aria-hidden="true"></i>
        </span>
        <span class="nav-label">{{ t(`nav.${item.name}`) }}</span>
      </router-link>

      <router-link
        v-for="item in secondary"
        :key="item.name"
        :to="item.to"
        class="nav-link nav-extra desktop-only"
        :class="{ active: isActive(item.to) }"
        :aria-current="isActive(item.to) ? 'page' : undefined"
      >
        <span class="nav-icon">
          <SolarIcon v-if="item.icon === 'solar-panel'" />
          <i v-else class="bi" :class="item.icon" aria-hidden="true"></i>
        </span>
        <span class="nav-label">{{ t(`nav.${item.name}`) }}</span>
      </router-link>

      <button
        class="nav-more"
        type="button"
        :class="{ active: moreActive || ui.moreOpen }"
        :aria-label="t('nav.more')"
        :aria-expanded="ui.moreOpen"
        @click="openMore"
      >
        <span class="nav-icon"><i class="bi bi-grid-3x3-gap" aria-hidden="true"></i></span>
        <span class="nav-label">{{ t('nav.more') }}</span>
      </button>
    </div>

    <div class="nav-footer desktop-only">
      <button class="nav-utility" type="button" @click="toggleTheme">
        <i class="bi" :class="theme === 'dark' ? 'bi-sun' : 'bi-moon-stars'" aria-hidden="true"></i>
        <span>{{ t('common.switchTheme') }}</span>
      </button>
      <div class="nav-locale">
        <i class="bi bi-translate" aria-hidden="true"></i>
        <select class="form-select form-select-sm locale-select" :value="locale" :aria-label="t('common.language')" @change="changeLocale">
          <option v-for="item in SUPPORTED_LOCALES" :key="item" :value="item">{{ item.toUpperCase() }}</option>
        </select>
      </div>
      <button class="nav-utility" type="button" @click="logout">
        <i class="bi bi-box-arrow-right" aria-hidden="true"></i>
        <span>{{ t('common.logout') }}</span>
      </button>
      <span class="nav-version">{{ t('more.versionShort', { version: session.meta?.version ?? '—' }) }}</span>
    </div>
  </nav>
</template>
