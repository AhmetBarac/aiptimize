<script setup lang="ts">
import { computed } from 'vue'
import type { PriceBlock, PriceWindow } from '../api/types'
import { formatMoney, timelineFraction } from '../lib/energy'

const props = defineProps<{
  blocks: PriceBlock[]
  now: string
  timezone: string
  locale: string
  currency: string
  cheapest?: PriceWindow | null
}>()

const HOURS = 24
const start = computed(() => new Date(props.now))
const end = computed(() => new Date(start.value.getTime() + HOURS * 3_600_000))

const visible = computed(() =>
  props.blocks.filter((block) => {
    if (block.missing) return false
    const blockStart = Date.parse(block.start)
    const blockEnd = Date.parse(block.end)
    return blockEnd > start.value.getTime() && blockStart < end.value.getTime()
  }),
)

const prices = computed(() => visible.value.map((block) => block.import_price))
const min = computed(() => Math.min(...prices.value, 0))
const max = computed(() => Math.max(...prices.value, 0.01))
const span = computed(() => Math.max(0.01, max.value - min.value))

function height(price: number): number {
  return 18 + ((price - min.value) / span.value) * 82
}

function color(block: PriceBlock): string {
  if (block.import_price < 0) return 'var(--aipt-green)'
  const ratio = (block.import_price - min.value) / span.value
  if (ratio <= 0.25) return 'var(--aipt-green)'
  if (ratio >= 0.75) return 'var(--aipt-orange)'
  return 'var(--aipt-blue)'
}

function left(iso: string): number {
  return timelineFraction(iso, start.value, end.value) * 100
}

function width(block: PriceBlock): number {
  const blockStart = Math.max(Date.parse(block.start), start.value.getTime())
  const blockEnd = Math.min(Date.parse(block.end), end.value.getTime())
  return Math.max(0.4, ((blockEnd - blockStart) / (end.value.getTime() - start.value.getTime())) * 100)
}

const cheapestBand = computed(() => {
  if (!props.cheapest) return null
  const from = Math.max(Date.parse(props.cheapest.start), start.value.getTime())
  const to = Math.min(Date.parse(props.cheapest.end), end.value.getTime())
  if (to <= from) return null
  return {
    left: ((from - start.value.getTime()) / (end.value.getTime() - start.value.getTime())) * 100,
    width: ((to - from) / (end.value.getTime() - start.value.getTime())) * 100,
  }
})

const current = computed(() => {
  const at = Date.parse(props.now)
  return visible.value.find((block) => Date.parse(block.start) <= at && Date.parse(block.end) > at) ?? null
})

const summary = computed(() => {
  const low = prices.value.length ? formatMoney(Math.min(...prices.value), props.locale, 3) : '—'
  const high = prices.value.length ? formatMoney(Math.max(...prices.value), props.locale, 3) : '—'
  return `24 h ${props.currency}: ${low}–${high}`
})
</script>

<template>
  <div v-if="visible.length" class="price-strip" role="img" :aria-label="summary">
    <span v-if="cheapestBand" class="price-strip-band" :style="{ left: `${cheapestBand.left}%`, width: `${cheapestBand.width}%` }" aria-hidden="true"></span>
    <span
      v-for="block in visible"
      :key="block.start"
      class="price-strip-bar"
      :style="{ left: `${left(block.start)}%`, width: `${width(block)}%`, height: `${height(block.import_price)}%`, background: color(block) }"
      aria-hidden="true"
    ></span>
    <span class="price-strip-now" :style="{ left: `${Math.max(0, Math.min(100, timelineFraction(now, start, end) * 100))}%` }" aria-hidden="true"></span>
    <span v-if="current" class="visually-hidden">Now {{ formatMoney(current.import_price, locale, 3) }} {{ currency }}/kWh</span>
  </div>
  <p v-else class="empty-state mb-0">—</p>
</template>
