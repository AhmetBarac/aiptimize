<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { session } from '../session'
import { openMore } from '../ui'

const { t } = useI18n()
const armed = computed(() => session.meta?.gates.writes_armed === true)
const initial = computed(() => (session.user?.username ?? 'A').slice(0, 1).toUpperCase())
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
          <i class="bi" :class="armed ? 'bi-shield-check' : 'bi-shield-lock'" aria-hidden="true"></i>
          <span class="d-none d-sm-inline">
            {{ armed ? t('dashboard.armed') : t('dashboard.disarmed') }}
          </span>
        </span>
        <button class="avatar-button" type="button" :aria-label="t('more.openMenu')" @click="openMore">
          {{ initial }}
        </button>
      </div>
    </div>
  </header>
</template>
