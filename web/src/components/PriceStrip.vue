<script setup lang="ts">
import { computed } from 'vue'
import type { PriceBlock, PriceWindow } from '../api/types'
import { formatPrice, localDateKey, priceShade, timelineFraction, zonedDayBounds } from '../lib/energy'

const props = defineProps<{
  blocks: PriceBlock[]
  now: string
  timezone: string
  locale: string
  currency: string
  cheapest?: PriceWindow | null
  fullPrice?: boolean
}>()

const day = computed(() => localDateKey(props.now, props.timezone))
const start = computed(() => zonedDayBounds(day.value, props.timezone).start)
const end = computed(() => zonedDayBounds(day.value, props.timezone).end)

const visible = computed(() =>
  props.blocks.filter((block) => {
    if (block.missing) return false
    const blockStart = Date.parse(block.start)
    const blockEnd = Date.parse(block.end)
    return blockEnd > start.value.getTime() && blockStart < end.value.getTime()
  }),
)

function displayPrice(block: PriceBlock): number {
  return props.fullPrice === false ? block.price_kwh : block.import_price
}

const prices = computed(() => visible.value.map(displayPrice))
const lowPrice = computed(() => Math.min(...prices.value))
const highPrice = computed(() => Math.max(...prices.value))

function color(block: PriceBlock): string {
  const priceSpan = highPrice.value - lowPrice.value
  const ratio = priceSpan > 0 ? (displayPrice(block) - lowPrice.value) / priceSpan : 0.5
  return priceShade(ratio)
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
  const low = prices.value.length ? formatPrice(Math.min(...prices.value), props.locale) : '—'
  const high = prices.value.length ? formatPrice(Math.max(...prices.value), props.locale) : '—'
  return `24 h ${props.currency}: ${low}–${high}`
})

const hourTicks = ['00', '06', '12', '18', '24']
</script>

<template>
  <div v-if="visible.length" class="price-strip-wrap" role="img" :aria-label="summary">
    <div class="price-strip">
      <span v-if="cheapestBand" class="price-strip-band" :style="{ left: `${cheapestBand.left}%`, width: `${cheapestBand.width}%` }" aria-hidden="true"></span>
      <span
        v-for="block in visible"
        :key="block.start"
        class="price-strip-bar"
        :style="{ left: `${left(block.start)}%`, width: `${width(block)}%`, background: color(block) }"
        aria-hidden="true"
      ></span>
      <span v-if="current" class="visually-hidden">Now {{ formatPrice(displayPrice(current), locale) }} {{ currency }}/kWh</span>
    </div>
    <span class="price-strip-now" :style="{ left: `${Math.max(0, Math.min(100, timelineFraction(now, start, end) * 100))}%` }" aria-hidden="true"></span>
    <div class="price-strip-hours" aria-hidden="true">
      <span v-for="tick in hourTicks" :key="tick">{{ tick }}</span>
    </div>
  </div>
  <p v-else class="empty-state mb-0">—</p>
</template>
