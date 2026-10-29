<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()

const items = computed(() => [
  { name: 'home', to: '/', icon: 'bi-house-door' },
  { name: 'plan', to: '/plan', icon: 'bi-calendar3-week' },
  { name: 'prices', to: '/prices', icon: 'bi-graph-up' },
  { name: 'history', to: '/history', icon: 'bi-bar-chart' },
  { name: 'car', to: '/car', icon: 'bi-ev-station' },
  { name: 'settings', to: '/settings', icon: 'bi-gear' },
])

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(`${path}/`)
}
</script>

<template>
  <nav class="app-nav" :aria-label="t('app.name')">
    <div class="nav-brand">
      <i class="bi bi-lightning-charge-fill" aria-hidden="true"></i>
      <span>{{ t('app.name') }}</span>
    </div>
    <router-link
      v-for="item in items"
      :key="item.name"
      :to="item.to"
      :class="{ active: isActive(item.to) }"
      :aria-current="isActive(item.to) ? 'page' : undefined"
    >
      <i class="bi" :class="item.icon" aria-hidden="true"></i>
      <span>{{ t(`nav.${item.name}`) }}</span>
    </router-link>
  </nav>
</template>
