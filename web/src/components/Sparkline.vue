<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    points: number[]
    color?: string
    height?: number
    strokeWidth?: number
  }>(),
  { color: 'var(--aipt-green)', height: 34, strokeWidth: 1.6 },
)

const width = 100
const padding = 2

const geometry = computed(() => {
  const values = props.points.filter((value) => Number.isFinite(value))
  if (values.length < 2) return null
  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = max - min || 1
  const step = (width - padding * 2) / (values.length - 1)
  const usableHeight = props.height - padding * 2
  const coords = values.map((value, index) => {
    const x = padding + index * step
    const y = padding + usableHeight - ((value - min) / span) * usableHeight
    return { x, y }
  })
  const line = coords.map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)},${point.y.toFixed(2)}`).join(' ')
  const area = `${line} L${coords[coords.length - 1].x.toFixed(2)},${props.height} L${coords[0].x.toFixed(2)},${props.height} Z`
  return { line, area }
})
</script>

<template>
  <svg
    v-if="geometry"
    class="sparkline"
    :style="{ color, height: `${height}px` }"
    :viewBox="`0 0 100 ${height}`"
    preserveAspectRatio="none"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="geometry.area" fill="currentColor" opacity="0.12" />
    <path
      :d="geometry.line"
      fill="none"
      stroke="currentColor"
      :stroke-width="strokeWidth"
      stroke-linecap="round"
      stroke-linejoin="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>

<style scoped>
.sparkline {
  display: block;
  width: 100%;
  overflow: visible;
}
</style>
