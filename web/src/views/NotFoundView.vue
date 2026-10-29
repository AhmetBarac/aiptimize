<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>

<template>
  <div class="auth-shell">
    <div class="auth-wrap">
      <div class="auth-card text-center">
        <div class="phase-icon mx-auto mb-3" aria-hidden="true"><i class="bi bi-compass"></i></div>
        <h1>404</h1>
        <p class="text-muted">{{ t('common.error') }}</p>
        <router-link class="btn btn-primary" to="/">{{ t('common.backHome') }}</router-link>
      </div>
    </div>
  </div>
</template>
