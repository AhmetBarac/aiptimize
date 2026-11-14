<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { EVPreview } from '../api/types'
import { session } from '../session'

defineProps<{ preview: EVPreview }>()

const { t, locale } = useI18n()

function fmtTime(iso: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: session.meta?.timezone ?? 'UTC',
  }).format(new Date(iso))
}
</script>

<template>
  <div class="plan-home-stats mb-3">
    <div>
      <span class="tile-label">{{ t('car.target') }}</span>
      <strong>{{ preview.target_kwh.toFixed(1) }} kWh</strong>
    </div>
    <div>
      <span class="tile-label">{{ t('car.remaining') }}</span>
      <strong>{{ preview.remaining_kwh.toFixed(1) }} kWh</strong>
    </div>
    <div>
      <span class="tile-label">{{ t('car.duration') }}</span>
      <strong>{{ preview.required_min }} min</strong>
    </div>
  </div>
  <div v-if="preview.windows.length" class="table-responsive">
    <table class="table table-sm plan-table">
      <thead>
        <tr>
          <th>{{ t('car.window') }}</th>
          <th>{{ t('car.source') }}</th>
          <th class="text-end">{{ t('car.energy') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="window in preview.windows" :key="window.start">
          <td>{{ fmtTime(window.start) }}–{{ fmtTime(window.end) }}</td>
          <td>
            <span class="badge-soft" :class="window.source === 'pv' ? 'badge-charge' : 'badge-discharge'">
              {{ window.source }}
            </span>
          </td>
          <td class="text-end">{{ window.energy_kwh.toFixed(1) }} kWh</td>
        </tr>
      </tbody>
    </table>
  </div>
  <p v-for="warning in preview.warnings ?? []" :key="warning" class="small text-muted mb-1">{{ warning }}</p>
</template>
