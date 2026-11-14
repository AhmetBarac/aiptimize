<script setup lang="ts">
import { computed } from 'vue'
import type { TelemetrySample } from '../api/types'
import { houseSupply, powerParts } from '../lib/energy'
import Sheet from './Sheet.vue'
import SolarIcon from './SolarIcon.vue'

const props = defineProps<{
  open: boolean
  sample: TelemetrySample | null
  locale: string
  labels: {
    title: string
    subtitle: string
    solar: string
    battery: string
    grid: string
    surplus: string
    estimate: string
    noData: string
  }
}>()

const emit = defineEmits<{ (event: 'update:open', value: boolean): void }>()

const supply = computed(() => houseSupply(props.sample))
const icons: Record<string, string> = {
  solar: 'solar-panel',
  battery: 'bi-battery-half',
  grid: 'bi-plug',
}

function power(value: number): string {
  const parts = powerParts(value, props.locale)
  return parts.unit ? `${parts.value} ${parts.unit}` : parts.value
}
</script>

<template>
  <Sheet
    :open="open"
    :title="labels.title"
    :subtitle="labels.subtitle"
    size="sm"
    @update:open="emit('update:open', $event)"
  >
    <div v-if="sample" class="supply-summary">
      <span class="supply-total">{{ power(supply.loadW) }}</span>
      <span class="supply-total-label">{{ labels.subtitle }}</span>
    </div>

    <ul v-if="sample" class="supply-list">
      <li v-for="row in supply.rows" :key="row.key" class="supply-row">
        <span class="supply-icon" :class="`supply-${row.key}`">
          <SolarIcon v-if="icons[row.key] === 'solar-panel'" />
          <i v-else class="bi" :class="icons[row.key]" aria-hidden="true"></i>
        </span>
        <span class="supply-name">{{ labels[row.key] }}</span>
        <span class="supply-value tabular">{{ power(row.watts) }}</span>
        <span class="supply-percent tabular">{{ Math.round(row.percent) }} %</span>
        <span class="supply-bar" aria-hidden="true">
          <span :class="`supply-fill supply-${row.key}`" :style="{ width: `${Math.max(0, Math.min(100, row.percent))}%` }"></span>
        </span>
      </li>
    </ul>

    <p v-if="sample && supply.surplusW > 1" class="supply-surplus">
      <i class="bi bi-arrow-up-right" aria-hidden="true"></i>
      {{ labels.surplus }}: <strong>{{ power(supply.surplusW) }}</strong>
    </p>
    <p v-if="sample" class="supply-note">{{ labels.estimate }}</p>
    <p v-else class="empty-state mb-0">{{ labels.noData }}</p>
  </Sheet>
</template>
