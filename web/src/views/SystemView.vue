<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../api/client'
import type { Status } from '../api/types'
import PageHead from '../components/PageHead.vue'
import SystemStatusCard from '../components/SystemStatusCard.vue'

const { t } = useI18n()
const status = ref<Status | null>(null)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined

async function load(): Promise<void> {
  try {
    status.value = await api.get<Status>('/status')
    error.value = ''
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

onMounted(() => {
  void load()
  timer = setInterval(() => void load(), 10_000)
})

onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <PageHead :title="t('system.title')" :subtitle="t('system.subtitle')">
    <RouterLink to="/settings/system" class="btn btn-outline-secondary btn-sm">
      <i class="bi bi-sliders" aria-hidden="true"></i>
      <span class="ms-1">{{ t('system.open') }}</span>
    </RouterLink>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="!status && !error" class="empty-state">{{ t('common.loading') }}</div>
  <SystemStatusCard v-if="status" :status="status" :show-head="false" />
</template>
