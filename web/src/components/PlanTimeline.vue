<script setup lang="ts">
import { computed } from 'vue'
import type { PlanSlot } from '../api/types'
import { formatPower, timelineFraction, timelineSegment, zonedDayBounds } from '../lib/energy'

const props = withDefaults(
  defineProps<{
    slots: PlanSlot[]
    day: string
    timezone: string
    locale: string
    now?: string
    compact?: boolean
    labels: {
      charge: string
      discharge: string
      now: string
      empty: string
    }
  }>(),
  { now: '', compact: false },
)

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
  slot: PlanSlot
  left: number
  width: number
  direction: 'charge' | 'discharge'
  title: string
}

const blocks = computed<Block[]>(() => {
  const { start, end } = bounds.value
  return props.slots.flatMap((slot) => {
    const segment = timelineSegment(slot.start, slot.end, start, end)
    if (!segment) return []
    return [
      {
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
    <div class="timeline-track">
      <div class="timeline-lane timeline-charge">
        <span v-for="block in blocks.filter((item) => item.direction === 'charge')" :key="`${block.slot.start}-c`" class="timeline-block charge" :style="{ left: `${block.left}%`, width: `${block.width}%` }" :title="block.title"></span>
      </div>
      <div class="timeline-axis" aria-hidden="true"></div>
      <div class="timeline-lane timeline-discharge">
        <span v-for="block in blocks.filter((item) => item.direction === 'discharge')" :key="`${block.slot.start}-d`" class="timeline-block discharge" :style="{ left: `${block.left}%`, width: `${block.width}%` }" :title="block.title"></span>
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
