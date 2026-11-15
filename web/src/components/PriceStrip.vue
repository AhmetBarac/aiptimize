<script setup lang="ts">
import { computed, ref } from 'vue'
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

// The pointer follows the cursor while it is over the strip and reports the
// block underneath, so a parent can show that hour's price. It snaps back to
// the live position as soon as the pointer is released or leaves.
const emit = defineEmits<{ (event: 'hover', block: PriceBlock | null): void }>()
const pointerPct = ref<number | null>(null)

function blockAt(clientX: number, el: HTMLElement): PriceBlock | null {
  const rect = el.getBoundingClientRect()
  if (rect.width <= 0) return null
  const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  const at = start.value.getTime() + ratio * (end.value.getTime() - start.value.getTime())
  return visible.value.find((block) => Date.parse(block.start) <= at && Date.parse(block.end) > at) ?? null
}

function onPointerMove(event: PointerEvent): void {
  const el = event.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  if (rect.width <= 0) return
  pointerPct.value = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width)) * 100
  emit('hover', blockAt(event.clientX, el))
}

function onPointerDown(event: PointerEvent): void {
  const el = event.currentTarget as HTMLElement
  el.setPointerCapture?.(event.pointerId)
  onPointerMove(event)
}

function releasePointer(): void {
  pointerPct.value = null
  emit('hover', null)
}

function onPointerUp(event: PointerEvent): void {
  const el = event.currentTarget as HTMLElement
  if (el.hasPointerCapture?.(event.pointerId)) el.releasePointerCapture(event.pointerId)
  releasePointer()
}
</script>

<template>
  <div v-if="visible.length" class="price-strip-wrap" role="img" :aria-label="summary">
    <div
      class="price-strip"
      @pointermove="onPointerMove"
      @pointerdown="onPointerDown"
      @pointerup="onPointerUp"
      @pointercancel="releasePointer"
      @pointerleave="releasePointer"
    >
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
    <span v-if="pointerPct !== null" class="price-strip-pointer" :style="{ left: `${pointerPct}%` }" aria-hidden="true"></span>
    <div class="price-strip-hours" aria-hidden="true">
      <span v-for="tick in hourTicks" :key="tick">{{ tick }}</span>
    </div>
  </div>
  <p v-else class="empty-state mb-0">—</p>
</template>
