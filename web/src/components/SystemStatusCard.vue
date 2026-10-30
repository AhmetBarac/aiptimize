<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Status } from '../api/types'

const props = withDefaults(defineProps<{ status: Status | null; showHead?: boolean }>(), {
  showHead: true,
})
const { t, locale } = useI18n()

function ago(seconds: number | undefined): string {
  if (seconds === undefined || seconds === null) return ''
  return t('dashboard.updatedAgo', { seconds: Math.max(0, Math.round(seconds)) })
}

function time(iso: string | undefined): string {
  if (!iso) return ''
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: props.status?.site.timezone ?? 'UTC',
  }).format(new Date(iso))
}

const rows = computed(() => {
  const status = props.status
  const telemetry = status?.telemetry
  const forecast = status?.forecast
  const inverter = status?.checks.inverter ?? 'unknown'
  return [
    {
      key: 'inverter',
      label: t('settings.inverter'),
      value: inverter,
      tone: inverter === 'ok' ? 'on' : 'warn',
      icon: 'bi-cpu',
    },
    {
      key: 'telemetry',
      label: t('dashboard.telemetryAvailable'),
      value: telemetry?.stale ? t('dashboard.staleTelemetry') : telemetry?.available ? ago(telemetry.age_s) : t('dashboard.waiting'),
      tone: telemetry?.stale ? 'warn' : telemetry?.available ? 'on' : 'off',
      icon: 'bi-broadcast',
    },
    {
      key: 'forecast',
      label: t('dashboard.forecastAvailable'),
      value: forecast?.stale ? t('forecast.stale') : forecast?.available ? time(forecast.generated_at) : t('forecast.notReady'),
      tone: forecast?.stale ? 'warn' : forecast?.available ? 'on' : 'off',
      icon: 'bi-cloud-sun',
    },
    {
      key: 'writes',
      label: t('dashboard.writes'),
      value: status?.gates.writes_armed ? t('dashboard.armed') : t('dashboard.disarmed'),
      tone: status?.gates.writes_armed ? 'on' : 'off',
      icon: status?.gates.writes_armed ? 'bi-shield-check' : 'bi-shield-lock',
    },
    {
      key: 'ev',
      label: t('dashboard.evControl'),
      value: status?.ev.available
        ? status.gates.ev_control_armed
          ? t('dashboard.armed')
          : t('dashboard.disarmed')
        : t('dashboard.evPhase'),
      tone: status?.ev.available ? (status.gates.ev_control_armed ? 'on' : 'off') : 'off',
      icon: 'bi-ev-station',
    },
  ]
})
</script>

<template>
  <section class="aipt-card system-card">
    <div v-if="showHead" class="system-head">
      <div>
        <h2>{{ t('system.title') }}</h2>
        <p>{{ t('system.subtitle') }}</p>
      </div>
      <RouterLink to="/settings/system" class="icon-button" :aria-label="t('system.open')">
        <i class="bi bi-arrow-up-right" aria-hidden="true"></i>
      </RouterLink>
    </div>
    <ul class="system-list">
      <li v-for="row in rows" :key="row.key" class="system-row">
        <span class="system-icon"><i class="bi" :class="row.icon" aria-hidden="true"></i></span>
        <span class="system-label">{{ row.label }}</span>
        <span class="badge-soft" :class="`badge-${row.tone}`">{{ row.value }}</span>
      </li>
    </ul>
    <div class="system-links">
      <RouterLink to="/settings/safety">{{ t('settings.sections.safety') }} →</RouterLink>
      <RouterLink to="/settings/system">{{ t('system.open') }} →</RouterLink>
    </div>
  </section>
</template>
