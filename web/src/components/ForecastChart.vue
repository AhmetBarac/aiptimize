<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { graphic } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { ForecastPoint } from '../api/types'

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

const props = withDefaults(
  defineProps<{
    points: ForecastPoint[]
    timezone: string
    locale: string
    labelPv: string
    labelLoad: string
    labelPvActual?: string
    labelLoadActual?: string
    actualPv?: Array<number | null>
    actualLoad?: Array<number | null>
    height?: number
  }>(),
  {
    height: 260,
    labelPvActual: '',
    labelLoadActual: '',
    actualPv: () => [],
    actualLoad: () => [],
  },
)

const el = ref<HTMLDivElement | null>(null)
let chart: ReturnType<typeof echarts.init> | null = null
let observer: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

function css(name: string, fallback: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function label(iso: string): string {
  return new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: props.timezone,
  }).format(new Date(iso))
}

function render(): void {
  if (!chart || props.points.length === 0) return
  const labels = props.points.map((p) => label(p.start))
  const pv = props.points.map((p) => Math.round((p.pv_w / 1000) * 100) / 100)
  const load = props.points.map((p) => Math.round((p.load_w / 1000) * 100) / 100)
  const measuredPv = props.actualPv
  const measuredLoad = props.actualLoad
  const hasMeasuredPv = measuredPv.some((value) => value !== null && value !== undefined)
  const hasMeasuredLoad = measuredLoad.some((value) => value !== null && value !== undefined)
  const text = css('--aipt-muted', '#9a9aa2')
  const border = css('--aipt-border', 'rgba(255,255,255,.08)')
  const surface = css('--aipt-surface', '#141416')
  const warm = css('--aipt-solar-warm', '#ffd166')
  const blue = css('--aipt-blue', '#0a84ff')

  const fmt = (value: number | null | undefined) =>
    value === null || value === undefined ? '—' : `${value.toFixed(2)} kW`

  const series: Record<string, unknown>[] = [
    {
      name: props.labelPv,
      type: 'line',
      smooth: 0.25,
      symbol: 'none',
      data: pv,
      // Forecast, so dashed; the measured line below is solid.
      lineStyle: { width: 2, color: warm, type: 'dashed' },
      itemStyle: { color: warm },
      areaStyle: {
        color: new graphic.LinearGradient(0, 0, 0, 1, [
          { offset: 0, color: 'rgba(255,209,102,0.22)' },
          { offset: 1, color: 'rgba(255,209,102,0.02)' },
        ]),
      },
    },
    {
      name: props.labelLoad,
      type: 'line',
      smooth: 0.25,
      symbol: 'none',
      data: load,
      // Forecast load is an estimate, so it is drawn dashed.
      lineStyle: { width: 2, color: blue, type: 'dashed' },
      itemStyle: { color: blue },
    },
  ]

  if (hasMeasuredPv) {
    series.push({
      name: props.labelPvActual || props.labelPv,
      type: 'line',
      smooth: 0.2,
      symbol: 'none',
      connectNulls: false,
      data: measuredPv,
      // Same colour as the forecast, distinguished by being solid.
      lineStyle: { width: 2.4, color: warm },
      itemStyle: { color: warm },
    })
  }
  if (hasMeasuredLoad) {
    series.push({
      name: props.labelLoadActual || props.labelLoad,
      type: 'line',
      smooth: 0.2,
      symbol: 'none',
      connectNulls: false,
      data: measuredLoad,
      lineStyle: { width: 2.2, color: blue },
      itemStyle: { color: blue },
    })
  }

  chart.setOption(
    {
      animation: false,
      grid: { left: 4, right: 12, top: 36, bottom: 4, containLabel: true },
      legend: {
        top: 0,
        right: 0,
        icon: 'roundRect',
        itemWidth: 8,
        itemHeight: 8,
        textStyle: { color: text, fontSize: 10 },
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'line', lineStyle: { color: text, type: 'solid', width: 1.5, opacity: 0.6 } },
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        formatter: (params: unknown) => {
          const list = params as Array<{ dataIndex: number }>
          if (!list.length) return ''
          const i = list[0].dataIndex
          const lines = [
            labels[i],
            `${props.labelPv}: <strong>${fmt(pv[i])}</strong>`,
            `${props.labelLoad}: <strong>${fmt(load[i])}</strong>`,
          ]
          if (hasMeasuredPv) {
            lines.push(`${props.labelPvActual || props.labelPv}: <strong>${fmt(measuredPv[i])}</strong>`)
          }
          if (hasMeasuredLoad) {
            lines.push(`${props.labelLoadActual || props.labelLoad}: <strong>${fmt(measuredLoad[i])}</strong>`)
          }
          return lines.join('<br/>')
        },
      },
      xAxis: {
        type: 'category',
        data: labels,
        boundaryGap: false,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: border } },
        axisLabel: { color: text, fontSize: 10, interval: 'auto', hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        name: 'kW',
        nameTextStyle: { color: text, fontSize: 10 },
        axisLabel: { color: text, fontSize: 10, formatter: (v: number) => v.toFixed(1) },
        splitLine: { lineStyle: { color: border, type: 'dashed' } },
      },
      series,
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
  () => [props.points, props.actualPv, props.actualLoad],
  () => render(),
  { deep: true },
)
</script>

<template>
  <div ref="el" class="forecast-chart" :style="{ height: `${height}px` }" role="img" :aria-label="`${labelPv} / ${labelLoad}`"></div>
</template>

<style scoped>
.forecast-chart {
  width: 100%;
}
</style>
