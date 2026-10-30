<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { TelemetrySample } from '../api/types'
import { powerParts } from '../lib/energy'

interface FlowLabels {
  flowTitle: string
  flowSubtitle: string
  solar: string
  grid: string
  battery: string
  house: string
  car: string
  charging: string
  discharging: string
  producing: string
  idle: string
  importing: string
  exporting: string
  connected: string
  unplugged: string
  liveLoad: string
  inspectHouse: string
  noData: string
}

const props = defineProps<{
  sample: TelemetrySample | null
  soc: number | null
  evPowerW: number | null
  evAvailable: boolean
  evConnected: boolean
  evCharging: boolean
  stale: boolean
  locale: string
  labels: FlowLabels
}>()

const emit = defineEmits<{ (event: 'open-house'): void }>()

const solarW = computed(() => props.sample?.pv_w ?? null)
const gridW = computed(() => props.sample?.grid_w ?? null)
const batteryW = computed(() => props.sample?.battery_w ?? null)
const houseW = computed(() => props.sample?.load_w ?? null)

function value(watts: number | null, signed = false): { value: string; unit: string } {
  const shown = watts === null ? null : signed ? watts : Math.abs(watts)
  return powerParts(shown, props.locale)
}

function hint(watts: number | null, positive: string, negative: string): string {
  if (watts === null) return props.labels.noData
  if (watts > 50) return positive
  if (watts < -50) return negative
  return props.labels.idle
}

const gridHint = computed(() => hint(gridW.value, props.labels.importing, props.labels.exporting))
const batteryHint = computed(() => hint(batteryW.value, props.labels.charging, props.labels.discharging))
const solarHint = computed(() => {
  if (solarW.value === null) return props.labels.noData
  return solarW.value > 50 ? props.labels.producing : props.labels.idle
})
const carHint = computed(() => {
  if (!props.evAvailable) return props.labels.unplugged
  if (props.evCharging) return props.labels.charging
  return props.evConnected ? props.labels.connected : props.labels.unplugged
})

interface FlowLine {
  key: string
  d: string
  color: string
  active: boolean
  reverse: boolean
  width: number
}

function line(key: string, d: string, watts: number | null, color: string, reverse = false): FlowLine {
  const magnitude = Math.abs(watts ?? 0)
  return {
    key,
    d,
    color,
    active: magnitude > 50,
    reverse,
    width: 1.4 + Math.min(2.6, (magnitude / 2500) * 2.6),
  }
}

const lines = computed<FlowLine[]>(() => [
  line('solar', 'M18,21 C30,26 34,38 43,47', solarW.value, 'var(--aipt-solar-warm)'),
  line(
    'grid',
    'M82,21 C70,26 66,38 57,47',
    gridW.value,
    (gridW.value ?? 0) >= 0 ? 'var(--aipt-grid-import)' : 'var(--aipt-green)',
    (gridW.value ?? 0) < 0,
  ),
  line(
    'battery',
    'M18,79 C34.5,79.5 41,70.5 43,58',
    batteryW.value,
    (batteryW.value ?? 0) >= 0 ? 'var(--aipt-green)' : 'var(--aipt-blue)',
    (batteryW.value ?? 0) > 0,
  ),
  line(
    'car',
    'M82,79 C70,76 66,66 57,58',
    props.evAvailable ? props.evPowerW : null,
    'var(--aipt-purple)',
    props.evCharging,
  ),
].filter((item) => item.key !== 'car' || props.evAvailable))

const solarValue = computed(() => value(solarW.value))
const gridValue = computed(() => value(gridW.value))
const batteryValue = computed(() => value(batteryW.value))
const houseValue = computed(() => value(houseW.value))
const carValue = computed(() => value(props.evPowerW))
</script>

<template>
  <section class="flow-card aipt-card" :class="{ 'flow-stale': stale, 'flow-no-ev': !evAvailable }">
    <header class="flow-head">
      <div>
        <h2 class="flow-title">{{ labels.flowTitle }}</h2>
      </div>
      <slot name="status" />
    </header>

    <div class="flow-stage" role="group" :aria-label="labels.liveLoad">
      <svg class="flow-lines" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        <path
          v-for="item in lines"
          :key="item.key"
          :d="item.d"
          class="flow-line"
          :class="{ active: item.active, reverse: item.reverse, solar: item.key === 'solar' }"
          :style="{ stroke: item.color, strokeWidth: item.width }"
        />
      </svg>

      <RouterLink
        to="/solar"
        class="flow-node flow-solar"
        :aria-label="`${labels.solar}: ${solarValue.value} ${solarValue.unit}, ${solarHint}`"
      >
        <span class="flow-node-label"><i class="bi bi-sun" aria-hidden="true"></i>{{ labels.solar }}</span>
        <strong class="flow-value tabular">
          {{ solarValue.value }}<span v-if="solarValue.unit" class="unit">{{ solarValue.unit }}</span>
        </strong>
        <span class="flow-hint">{{ solarHint }}</span>
      </RouterLink>

      <RouterLink
        to="/prices"
        class="flow-node flow-grid"
        :aria-label="`${labels.grid}: ${gridValue.value} ${gridValue.unit}, ${gridHint}`"
      >
        <span class="flow-node-label"><i class="bi bi-plug" aria-hidden="true"></i>{{ labels.grid }}</span>
        <strong class="flow-value tabular">
          {{ gridValue.value }}<span v-if="gridValue.unit" class="unit">{{ gridValue.unit }}</span>
        </strong>
        <span class="flow-hint">{{ gridHint }}</span>
      </RouterLink>

      <button
        class="flow-node flow-house"
        type="button"
        aria-haspopup="dialog"
        :aria-label="`${labels.house}: ${houseValue.value} ${houseValue.unit}, ${labels.inspectHouse}`"
        @click="emit('open-house')"
      >
        <span class="flow-house-icon" aria-hidden="true"><i class="bi bi-house-door-fill"></i></span>
        <span class="flow-node-label">{{ labels.house }}</span>
        <strong class="flow-value tabular">
          {{ houseValue.value }}<span v-if="houseValue.unit" class="unit">{{ houseValue.unit }}</span>
        </strong>
        <span class="flow-hint">{{ labels.inspectHouse }}</span>
      </button>

      <RouterLink
        to="/plan"
        class="flow-node flow-battery"
        :aria-label="`${labels.battery}: ${batteryValue.value} ${batteryValue.unit}, ${batteryHint}${soc !== null ? `, ${Math.round(soc)} %` : ''}`"
      >
        <span class="flow-node-label"><i class="bi bi-battery-half" aria-hidden="true"></i>{{ labels.battery }}</span>
        <strong class="flow-value tabular">
          {{ batteryValue.value }}<span v-if="batteryValue.unit" class="unit">{{ batteryValue.unit }}</span>
        </strong>
        <span class="flow-hint">{{ batteryHint }}</span>
        <span v-if="soc !== null" class="flow-soc">{{ Math.round(soc) }} %</span>
      </RouterLink>

      <RouterLink
        v-if="evAvailable"
        to="/car"
        class="flow-node flow-car"
        :aria-label="`${labels.car}: ${carValue.value} ${carValue.unit}, ${carHint}`"
      >
        <span class="flow-node-label"><i class="bi bi-ev-station" aria-hidden="true"></i>{{ labels.car }}</span>
        <strong class="flow-value tabular">
          {{ carValue.value }}<span v-if="carValue.unit" class="unit">{{ carValue.unit }}</span>
        </strong>
        <span class="flow-hint">{{ carHint }}</span>
      </RouterLink>
    </div>

    <ul class="visually-hidden">
      <li>{{ labels.solar }}: {{ solarValue.value }} {{ solarValue.unit }}, {{ solarHint }}</li>
      <li>{{ labels.grid }}: {{ gridValue.value }} {{ gridValue.unit }}, {{ gridHint }}</li>
      <li>{{ labels.battery }}: {{ batteryValue.value }} {{ batteryValue.unit }}, {{ batteryHint }}</li>
      <li>{{ labels.house }}: {{ houseValue.value }} {{ houseValue.unit }}</li>
      <li v-if="evAvailable">{{ labels.car }}: {{ carValue.value }} {{ carValue.unit }}, {{ carHint }}</li>
    </ul>
  </section>
</template>
