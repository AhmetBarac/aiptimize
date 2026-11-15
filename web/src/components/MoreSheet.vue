<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Sheet from './Sheet.vue'
import { currentLocale, setLocale, SUPPORTED_LOCALES, type Locale } from '../i18n'
import { isAdmin, session, signOut } from '../session'
import { theme, toggleTheme } from '../theme'
import { ui } from '../ui'

const { t } = useI18n()
const router = useRouter()
const locale = computed(() => currentLocale())
const version = computed(() => session.meta?.version ?? '—')

const open = computed({
  get: () => ui.moreOpen,
  set: (value: boolean) => {
    ui.moreOpen = value
  },
})

const links = [
  { to: '/history', icon: 'bi-bar-chart', key: 'nav.history', hint: 'more.historyHint' },
  { to: '/prices', icon: 'bi-graph-up', key: 'nav.prices', hint: 'more.pricesHint' },
  { to: '/system', icon: 'bi-activity', key: 'system.title', hint: 'more.systemHint' },
  ...(isAdmin()
    ? [{ to: '/settings', icon: 'bi-gear', key: 'nav.settings', hint: 'more.settingsHint' }]
    : []),
]

// The companion app injects a native bridge so the web UI can hand control back
// to it (switch to the server list, or reload the page). Absent in a browser.
const companion = (window as unknown as { Aiptimize?: { servers: () => void; reload: () => void } }).Aiptimize
const companionAvailable = computed(() => typeof companion !== 'undefined')

function companionServers(): void {
  companion?.servers()
  ui.moreOpen = false
}

function companionReload(): void {
  companion?.reload()
  ui.moreOpen = false
}

function changeLocale(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  if (value === 'en' || value === 'sv') setLocale(value as Locale)
}

async function logout(): Promise<void> {
  ui.moreOpen = false
  await signOut()
  await router.push({ name: 'login' })
}
</script>

<template>
  <Sheet v-model:open="open" :title="t('more.title')" :subtitle="t('more.subtitle')" size="sm">
    <nav class="more-links" :aria-label="t('more.title')">
      <RouterLink v-for="link in links" :key="link.to" :to="link.to" class="more-link" @click="ui.moreOpen = false">
        <span class="more-link-icon"><i class="bi" :class="link.icon" aria-hidden="true"></i></span>
        <span class="more-link-text">
          <strong>{{ t(link.key) }}</strong>
          <small>{{ t(link.hint) }}</small>
        </span>
        <i class="bi bi-chevron-right more-chevron" aria-hidden="true"></i>
      </RouterLink>
    </nav>

    <div v-if="companionAvailable" class="more-section">
      <h3>{{ t('more.companion') }}</h3>
      <button class="more-row" type="button" @click="companionServers">
        <span class="more-row-label">
          <i class="bi bi-collection" aria-hidden="true"></i>
          {{ t('more.servers') }}
        </span>
      </button>
      <button class="more-row" type="button" @click="companionReload">
        <span class="more-row-label">
          <i class="bi bi-arrow-clockwise" aria-hidden="true"></i>
          {{ t('more.reload') }}
        </span>
      </button>
    </div>

    <div class="more-section">
      <h3>{{ t('more.preferences') }}</h3>
      <button class="more-row" type="button" @click="toggleTheme">
        <span class="more-row-label">
          <i class="bi" :class="theme === 'dark' ? 'bi-sun' : 'bi-moon-stars'" aria-hidden="true"></i>
          {{ t('common.switchTheme') }}
        </span>
        <span class="more-row-value">{{ theme === 'dark' ? t('more.dark') : t('more.light') }}</span>
      </button>
      <div class="more-row">
        <label class="more-row-label" for="more-locale">
          <i class="bi bi-translate" aria-hidden="true"></i>
          {{ t('common.language') }}
        </label>
        <select id="more-locale" class="form-select form-select-sm locale-select" :value="locale" @change="changeLocale">
          <option v-for="item in SUPPORTED_LOCALES" :key="item" :value="item">{{ item.toUpperCase() }}</option>
        </select>
      </div>
    </div>

    <div class="more-section">
      <button class="more-row danger" type="button" @click="logout">
        <span class="more-row-label">
          <i class="bi bi-box-arrow-right" aria-hidden="true"></i>
          {{ t('common.logout') }}
        </span>
      </button>
    </div>

    <p class="more-version">{{ t('more.version', { version }) }}</p>
  </Sheet>
</template>
