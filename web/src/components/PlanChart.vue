<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, MarkAreaComponent, TooltipComponent, VisualMapComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { Plan } from '../api/types'
import { formatPrice, roundPrice } from '../lib/energy'

echarts.use([
  BarChart,
  LineChart,
  GridComponent,
  LegendComponent,
  MarkAreaComponent,
  TooltipComponent,
  VisualMapComponent,
  CanvasRenderer,
])

const props = defineProps<{
  plan: Plan
  locale: string
  labels: {
    price: string
    pv: string
    load: string
    battery: string
    soc: string
    charge: string
    discharge: string
  }
}>()

const el = ref<HTMLDivElement | null>(null)
let chart: ReturnType<typeof echarts.init> | null = null
let observer: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

function css(name: string, fallback: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function timeLabel(iso: string): string {
  const date = new Date(iso)
  const hour = new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    hour12: false,
    timeZone: props.plan.timezone,
  }).format(date)
  if (hour === '00' || hour === '24') {
    return new Intl.DateTimeFormat(props.locale, {
      weekday: 'short',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
      timeZone: props.plan.timezone,
    }).format(date)
  }
  return new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: props.plan.timezone,
  }).format(date)
}

function render(): void {
  if (!chart || (props.plan.curve?.length ?? 0) === 0) return
  const curve = props.plan.curve ?? []
  const labels = curve.map((p) => timeLabel(p.start))
  const price = curve.map((p) => roundPrice(p.price_import))
  const pv = curve.map((p) => Math.round((p.pv_w / 1000) * 100) / 100)
  const load = curve.map((p) => Math.round((p.load_w / 1000) * 100) / 100)
  const battery = curve.map((p) => Math.round((p.battery_w / 1000) * 100) / 100)
  const soc = curve.map((p) => Math.round(p.soc))
  const maxPrice = Math.max(...price, 0.1)

  const text = css('--aipt-muted', '#9a9aa2')
  const border = css('--aipt-border', 'rgba(255,255,255,.08)')
  const surface = css('--aipt-surface', '#141416')
  const green = css('--aipt-green', '#32d74b')
  const warm = css('--aipt-solar-warm', '#ffd166')
  const blue = css('--aipt-blue', '#0a84ff')
  const orange = css('--aipt-orange', '#ff9f0a')
  const accent = css('--aipt-accent', '#7c5cff')

  const indexByStart = new Map(curve.map((point, index) => [point.start, index]))
  const slotAreas = props.plan.slots.map((slot) => {
    const color = slot.direction === 'charge' ? 'rgba(50,215,75,0.10)' : 'rgba(255,159,10,0.10)'
    const startIndex = indexByStart.get(slot.start) ?? 0
    const endIndex = Math.max(startIndex, (indexByStart.get(slot.end) ?? curve.length) - 1)
    return [
      { coord: [startIndex, 0], itemStyle: { color } },
      { coord: [endIndex, maxPrice] },
    ]
  })

  chart.setOption(
    {
      animation: false,
      grid: { left: 4, right: 12, top: 58, bottom: 4, containLabel: true },
      legend: {
        top: 0,
        left: 0,
        icon: 'roundRect',
        itemWidth: 8,
        itemHeight: 8,
        itemGap: 10,
        textStyle: { color: text, fontSize: 10 },
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'line', lineStyle: { color: text, type: 'dashed', width: 1.5, opacity: 0.6 } },
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        formatter: (params: unknown) => {
          const list = params as Array<{ dataIndex: number }>
          if (!list.length) return ''
          const i = list[0].dataIndex
          return [
            labels[i],
            `${props.labels.price}: ${formatPrice(price[i], props.locale)}`,
            `${props.labels.pv}: ${pv[i].toFixed(2)} kW`,
            `${props.labels.load}: ${load[i].toFixed(2)} kW`,
            `${props.labels.battery}: ${battery[i].toFixed(2)} kW`,
            `${props.labels.soc}: ${soc[i]} %`,
          ].join('<br/>')
        },
      },
      visualMap: {
        show: false,
        dimension: 1,
        min: Math.min(...price),
        max: Math.max(...price),
        inRange: { color: [green, '#e6c84b', orange] },
        seriesIndex: 0,
      },
      xAxis: {
        type: 'category',
        data: labels,
        boundaryGap: true,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: border } },
        axisLabel: { color: text, fontSize: 10, interval: 'auto', hideOverlap: true },
      },
      yAxis: [
        {
          type: 'value',
          // Values are read from the tooltip; hiding the labels gives the
          // curves the full width of the card.
          axisLabel: { show: false },
          axisTick: { show: false },
          axisLine: { show: false },
          splitLine: { lineStyle: { color: border, type: 'dashed' } },
        },
        {
          type: 'value',
          min: 0,
          max: 100,
          position: 'right',
          axisLabel: { show: false },
          splitLine: { show: false },
        },
        {
          type: 'value',
          min: 0,
          max: maxPrice,
          position: 'right',
          axisLabel: { show: false },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: props.labels.price,
          type: 'line',
          yAxisIndex: 2,
          data: price,
          symbol: 'none',
          smooth: 0.15,
          // Colour comes from the visual map, so the line follows the same
          // red-to-green price scale as the prices view.
          lineStyle: { width: 1.6 },
          markArea: {
            silent: true,
            data: slotAreas,
          },
        },
        {
          name: props.labels.pv,
          type: 'line',
          yAxisIndex: 0,
          data: pv,
          symbol: 'none',
          smooth: 0.2,
          // PV here is the forecast, so it is dashed and solar yellow like the
          // forecast chart.
          lineStyle: { width: 1.8, color: warm, type: 'dashed' },
          itemStyle: { color: warm },
          areaStyle: { color: 'rgba(255,209,102,0.12)' },
        },
        {
          name: props.labels.load,
          type: 'line',
          yAxisIndex: 0,
          data: load,
          symbol: 'none',
          smooth: 0.2,
          // Plan load is the estimate the planner works from, not a measurement.
          lineStyle: { width: 1.6, color: blue, type: 'dashed' },
          itemStyle: { color: blue },
        },
        {
          name: props.labels.battery,
          type: 'bar',
          yAxisIndex: 0,
          data: battery.map((value) => ({
            value,
            itemStyle: { color: value >= 0 ? 'rgba(50,215,75,0.55)' : 'rgba(255,159,10,0.55)' },
          })),
          itemStyle: { color: green },
          barMaxWidth: 6,
        },
        {
          name: props.labels.soc,
          type: 'line',
          yAxisIndex: 1,
          data: soc,
          symbol: 'none',
          smooth: 0.1,
          lineStyle: { width: 1.5, color: accent, type: 'dashed' },
          itemStyle: { color: accent },
        },
      ],
    },
    true,
  )
}

onMounted(() => {
  if (!el.value) return
  chart = echarts.init(el.value)
  render()
  observer = new ResizeObserver(() => chart?.resize())
  observer.observe(el.value)
  themeObserver = new MutationObserver(() => render())
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-bs-theme'],
  })
})

onBeforeUnmount(() => {
  observer?.disconnect()
  themeObserver?.disconnect()
  chart?.dispose()
  chart = null
})

watch(
  () => props.plan,
  () => render(),
  { deep: true },
)
</script>

<template>
  <div ref="el" class="plan-chart" role="img" :aria-label="`${labels.price} / ${labels.soc}`"></div>
</template>

<style scoped>
.plan-chart {
  width: 100%;
  height: 380px;
}
</style>
