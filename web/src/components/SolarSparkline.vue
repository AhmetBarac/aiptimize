<script setup lang="ts">
import { computed } from 'vue'
import type { SolarPoint } from '../api/types'

const props = defineProps<{
  points: SolarPoint[]
}>()

interface Segment {
  line: string
  area: string
}

const backdrop = computed(() => {
  const values = props.points.map((p) => p.expected_w)
  const max = Math.max(...values, 1)
  const n = props.points.length
  if (n < 2) return ''
  const path = values
    .map((v, i) => {
      const x = (i / (n - 1)) * 100
      const y = 28 - (v / max) * 24
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
  return `${path} L100,30 L0,30 Z`
})

const segments = computed<Segment[]>(() => {
  const values = props.points.map((p) => p.actual_w)
  const max = Math.max(...props.points.map((p) => p.expected_w), ...values.filter((v): v is number => v !== null), 1)
  const n = props.points.length
  if (n < 2) return []
  const out: Segment[] = []
  let current: string[] = []
  const flush = (): void => {
    if (current.length > 1) {
      const first = current[0]
      const last = current[current.length - 1]
      const x0 = first.split(',')[0]
      const x1 = last.split(',')[0]
      out.push({
        line: `M${current.join(' L')}`,
        area: `M${x0},30 L${current.join(' L')} L${x1},30 Z`,
      })
    }
    current = []
  }
  props.points.forEach((p, i) => {
    if (p.actual_w === null) {
      flush()
      return
    }
    const x = (i / (n - 1)) * 100
    const y = 28 - (p.actual_w / max) * 24
    current.push(`${x.toFixed(2)},${y.toFixed(2)}`)
  })
  flush()
  return out
})
</script>

<template>
  <svg class="solar-sparkline" viewBox="0 0 100 30" preserveAspectRatio="none" aria-hidden="true">
    <path v-if="backdrop" :d="backdrop" fill="rgba(255, 209, 102, 0.08)" />
    <path
      v-for="(segment, index) in segments"
      :key="index"
      :d="segment.area"
      fill="rgba(255, 209, 102, 0.18)"
    />
    <path
      v-for="(segment, index) in segments"
      :key="`line-${index}`"
      :d="segment.line"
      fill="none"
      stroke="var(--aipt-solar-warm)"
      stroke-width="1.6"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>

<style scoped>
.solar-sparkline {
  display: block;
  width: 100%;
  height: 30px;
  margin-top: 0.35rem;
}
</style>
