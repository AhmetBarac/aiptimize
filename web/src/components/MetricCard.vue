<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import Sparkline from './Sparkline.vue'

const props = withDefaults(
  defineProps<{
    label: string
    value: string
    unit?: string
    icon?: string
    tone?: 'solar' | 'grid' | 'battery' | 'car' | 'neutral'
    hint?: string
    points?: number[]
    to?: string
    loading?: boolean
  }>(),
  {
    unit: '',
    icon: 'bi-activity',
    tone: 'neutral',
    hint: '',
    points: () => [],
    to: '',
    loading: false,
  },
)

const sparkColor = computed(() => {
  switch (props.tone) {
    case 'solar':
      return 'var(--aipt-solar-warm)'
    case 'grid':
      return 'var(--aipt-orange)'
    case 'battery':
      return 'var(--aipt-blue)'
    case 'car':
      return 'var(--aipt-purple)'
    default:
      return 'var(--aipt-text)'
  }
})
</script>

<template>
  <component
    :is="to ? RouterLink : 'div'"
    :to="to || undefined"
    class="metric-card"
    :class="[`metric-tone-${tone}`, { 'metric-link': Boolean(to) }]"
  >
    <div class="metric-top">
      <span class="metric-label">
        <i v-if="icon" class="bi" :class="icon" aria-hidden="true"></i>
        {{ label }}
      </span>
      <i v-if="to" class="bi bi-arrow-up-right metric-arrow" aria-hidden="true"></i>
    </div>
    <div v-if="loading" class="metric-skeleton" aria-hidden="true"></div>
    <div v-else class="metric-value tabular">
      {{ value }}<span v-if="unit" class="unit">{{ unit }}</span>
    </div>
    <Sparkline v-if="!loading && points.length > 1" :points="points" :color="sparkColor" />
    <span v-if="hint" class="metric-hint">{{ hint }}</span>
  </component>
</template>
