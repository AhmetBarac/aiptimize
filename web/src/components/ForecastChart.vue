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
    height?: number
  }>(),
  { height: 260 },
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
  const text = css('--aipt-muted', '#9a9aa2')
  const border = css('--aipt-border', 'rgba(255,255,255,.08)')
  const surface = css('--aipt-surface', '#141416')
  const warm = css('--aipt-solar-warm', '#ffd166')
  const blue = css('--aipt-blue', '#0a84ff')

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
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        formatter: (params: unknown) => {
          const list = params as Array<{ dataIndex: number }>
          if (!list.length) return ''
          const i = list[0].dataIndex
          return `${labels[i]}<br/>${props.labelPv}: <strong>${pv[i].toFixed(2)} kW</strong><br/>${props.labelLoad}: <strong>${load[i].toFixed(2)} kW</strong>`
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
      series: [
        {
          name: props.labelPv,
          type: 'line',
          smooth: 0.25,
          symbol: 'none',
          data: pv,
          lineStyle: { width: 2, color: warm },
          itemStyle: { color: warm },
          areaStyle: {
            color: new graphic.LinearGradient(0, 0, 0, 1, [
              { offset: 0, color: 'rgba(255,209,102,0.34)' },
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
  () => props.points,
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
