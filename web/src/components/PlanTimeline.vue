<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import type { PlanSlot, PriceBlock } from '../api/types'
import { formatPower, priceShade, timelineFraction, timelineSegment, zonedDayBounds } from '../lib/energy'

const props = withDefaults(
  defineProps<{
    slots: PlanSlot[]
    day: string
    timezone: string
    locale: string
    now?: string
    compact?: boolean
    prices?: PriceBlock[]
    editable?: boolean
    selectedIndex?: number
    labels: {
      charge: string
      discharge: string
      now: string
      empty: string
    }
  }>(),
  { now: '', compact: false, prices: () => [], editable: false, selectedIndex: -1 },
)

const emit = defineEmits<{
  (event: 'select-slot', index: number): void
  (event: 'update-slot', payload: { index: number; start: string; duration: number }): void
  (event: 'add-slot', payload: { start: string }): void
}>()

const bounds = computed(() => zonedDayBounds(props.day, props.timezone))

function time(iso: string): string {
  return new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: props.timezone,
  }).format(new Date(iso))
}

interface Block {
  index: number
  slot: PlanSlot
  left: number
  width: number
  direction: 'charge' | 'discharge'
  title: string
}

const blocks = computed<Block[]>(() => {
  const { start, end } = bounds.value
  return props.slots.flatMap((slot, index) => {
    const segment = timelineSegment(slot.start, slot.end, start, end)
    if (!segment) return []
    return [
      {
        index,
        slot,
        left: segment.left * 100,
        width: Math.max(0.8, segment.width * 100),
        direction: slot.direction,
        title: `${slot.direction === 'charge' ? props.labels.charge : props.labels.discharge} ${time(slot.start)}–${time(slot.end)} · ${formatPower(slot.power_w, props.locale)} · ${slot.energy_kwh.toFixed(1)} kWh`,
      },
    ]
  })
})

const nowLeft = computed(() => {
  if (!props.now) return null
  const { start, end } = bounds.value
  if (Date.parse(props.now) < start.getTime() || Date.parse(props.now) > end.getTime()) return null
  return timelineFraction(props.now, start, end) * 100
})

type DragMode = 'move' | 'start' | 'end'
interface DragState {
  mode: DragMode
  index: number
  startX: number
  startMs: number
  durationMin: number
  trackWidth: number
}
let drag: DragState | null = null

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value))
}

function onPointerMove(event: PointerEvent): void {
  if (!drag) return
  const dayStart = bounds.value.start.getTime()
  const dayEnd = bounds.value.end.getTime()
  const deltaMinutes = Math.round(
    ((event.clientX - drag.startX) / Math.max(1, drag.trackWidth)) * (24 * 60) / 30,
  ) * 30
  let start = drag.startMs
  let duration = drag.durationMin
  if (drag.mode === 'move') {
    start = clamp(start + deltaMinutes * 60_000, dayStart, dayEnd - duration * 60_000)
  } else if (drag.mode === 'start') {
    const nextStart = clamp(start + deltaMinutes * 60_000, dayStart, start + (duration - 30) * 60_000)
    duration = clamp(Math.round((start + duration * 60_000 - nextStart) / 60_000), 30, 240)
    start = nextStart
  } else {
    duration = clamp(duration + deltaMinutes, 30, 240)
    if (start + duration * 60_000 > dayEnd) {
      duration = Math.floor((dayEnd - start) / 60_000)
    }
  }
  emit('update-slot', { index: drag.index, start: new Date(start).toISOString(), duration })
}

function stopDrag(): void {
  drag = null
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', stopDrag)
  window.removeEventListener('pointercancel', stopDrag)
}

function startDrag(event: PointerEvent, block: Block, mode: DragMode): void {
  if (!props.editable) return
  event.preventDefault()
  event.stopPropagation()
  emit('select-slot', block.index)
  const track = (event.currentTarget as HTMLElement).closest('.timeline-track') as HTMLElement | null
  if (!track) return
  drag = {
    mode,
    index: block.index,
    startX: event.clientX,
    startMs: Date.parse(block.slot.start),
    durationMin: Math.max(30, Math.round((Date.parse(block.slot.end) - Date.parse(block.slot.start)) / 60_000)),
    trackWidth: track.getBoundingClientRect().width,
  }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', stopDrag)
  window.addEventListener('pointercancel', stopDrag)
}

function addAt(event: PointerEvent): void {
  if (!props.editable) return
  const target = event.target as HTMLElement
  if (target.closest('.timeline-block, .timeline-now')) return
  const track = event.currentTarget as HTMLElement
  const rect = track.getBoundingClientRect()
  const fraction = clamp((event.clientX - rect.left) / rect.width, 0, 1)
  const minutes = Math.round((fraction * 24 * 60) / 30) * 30
  const start = bounds.value.start.getTime() + minutes * 60_000
  if (start >= bounds.value.end.getTime()) return
  emit('add-slot', { start: new Date(start).toISOString() })
}

onBeforeUnmount(stopDrag)

const priceBars = computed(() => {
  const { start, end } = bounds.value
  const blocks = props.prices
    .filter((block) => Date.parse(block.end) > start.getTime() && Date.parse(block.start) < end.getTime())
    .sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  if (!blocks.length) return []
  const values = blocks.map((block) => block.import_price)
  const min = Math.min(...values)
  const max = Math.max(...values)
  return blocks.flatMap((block) => {
    const segment = timelineSegment(block.start, block.end, start, end)
    if (!segment) return []
    const norm = max > min ? (block.import_price - min) / (max - min) : 0.5
    return [{
      start: block.start,
      left: segment.left * 100,
      width: Math.max(0.35, segment.width * 100),
      height: 25 + norm * 75,
      color: priceShade(norm),
    }]
  })
})

const hourTicks = computed(() => {
  const { start, end } = bounds.value
  const duration = end.getTime() - start.getTime()
  return [0, 0.25, 0.5, 0.75, 1].map((fraction) => {
    const at = new Date(start.getTime() + duration * fraction)
    return {
      label: new Intl.DateTimeFormat(props.locale, {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
        timeZone: props.timezone,
      }).format(at),
      left: fraction * 100,
    }
  })
})
</script>

<template>
  <div class="plan-timeline" :class="{ compact }">
    <div class="timeline-hours" aria-hidden="true">
      <span v-for="tick in hourTicks" :key="tick.label" :style="{ left: `${tick.left}%` }">{{ tick.label }}</span>
    </div>
    <div class="timeline-track" :class="{ editable }" @pointerdown="addAt">
      <div class="timeline-price-bars" aria-hidden="true">
        <span
          v-for="bar in priceBars"
          :key="`${bar.start}-price`"
          :style="{ left: `${bar.left}%`, width: `${bar.width}%`, height: `${bar.height}%`, background: bar.color }"
        ></span>
      </div>
      <div class="timeline-lane timeline-charge">
        <span
          v-for="block in blocks.filter((item) => item.direction === 'charge')"
          :key="`${block.slot.start}-c`"
          class="timeline-block charge"
          :class="{ selected: selectedIndex === block.index }"
          :style="{ left: `${block.left}%`, width: `${block.width}%` }"
          :title="block.title"
          @pointerdown="startDrag($event, block, 'move')"
        >
          <span v-if="editable" class="timeline-handle start" @pointerdown="startDrag($event, block, 'start')"></span>
          <span v-if="editable" class="timeline-handle end" @pointerdown="startDrag($event, block, 'end')"></span>
        </span>
      </div>
      <div class="timeline-axis" aria-hidden="true"></div>
      <div class="timeline-lane timeline-discharge">
        <span
          v-for="block in blocks.filter((item) => item.direction === 'discharge')"
          :key="`${block.slot.start}-d`"
          class="timeline-block discharge"
          :class="{ selected: selectedIndex === block.index }"
          :style="{ left: `${block.left}%`, width: `${block.width}%` }"
          :title="block.title"
          @pointerdown="startDrag($event, block, 'move')"
        >
          <span v-if="editable" class="timeline-handle start" @pointerdown="startDrag($event, block, 'start')"></span>
          <span v-if="editable" class="timeline-handle end" @pointerdown="startDrag($event, block, 'end')"></span>
        </span>
      </div>
      <span v-if="nowLeft !== null" class="timeline-now" :style="{ left: `${nowLeft}%` }">
        <span class="timeline-now-label">{{ labels.now }}</span>
      </span>
      <p v-if="blocks.length === 0" class="timeline-empty">{{ labels.empty }}</p>
    </div>
    <ul class="visually-hidden">
      <li v-for="block in blocks" :key="`${block.slot.start}-sr`">{{ block.title }}</li>
    </ul>
  </div>
</template>
