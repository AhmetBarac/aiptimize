<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { SolarPoint } from '../api/types'

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

const props = defineProps<{
  points: SolarPoint[]
  timezone: string
  locale: string
  labels: {
    measured: string
    expected: string
    minimum: string
    maximum: string
    load: string
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
  return new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: props.timezone,
  }).format(new Date(iso))
}

function kw(value: number | null | undefined): number | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null
  return Math.round((value / 1000) * 100) / 100
}

function render(): void {
  if (!chart || props.points.length === 0) return
  const labels = props.points.map((p) => timeLabel(p.start))
  const actual = props.points.map((p) => kw(p.actual_w))
  const expected = props.points.map((p) => kw(p.expected_w))
  const min = props.points.map((p) => kw(p.min_w))
  const max = props.points.map((p) => kw(p.max_w))
  const load = props.points.map((p) => kw(p.load_w))

  const text = css('--aipt-muted', '#9a9aa2')
  const border = css('--aipt-border', 'rgba(255,255,255,.08)')
  const surface = css('--aipt-surface', '#141416')
  const green = css('--aipt-green', '#32d74b')
  const yellow = css('--aipt-yellow', '#ffd60a')
  const orange = css('--aipt-orange', '#ff9f0a')
  const blue = css('--aipt-blue', '#0a84ff')

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
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        formatter: (params: unknown) => {
          const list = params as Array<{ dataIndex: number }>
          if (!list.length) return ''
          const i = list[0].dataIndex
          const fmt = (value: number | null) => (value === null ? '—' : `${value.toFixed(2)} kW`)
          return [
            labels[i],
            `${props.labels.measured}: ${fmt(actual[i])}`,
            `${props.labels.expected}: ${fmt(expected[i])}`,
            `${props.labels.minimum}: ${fmt(min[i])}`,
            `${props.labels.maximum}: ${fmt(max[i])}`,
            `${props.labels.load}: ${fmt(load[i])}`,
          ].join('<br/>')
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
        axisLabel: { color: text, fontSize: 10, formatter: (v: number) => v.toFixed(1) },
        splitLine: { lineStyle: { color: border, type: 'dashed' } },
      },
      series: [
        {
          name: props.labels.measured,
          type: 'line',
          smooth: 0.2,
          symbol: 'none',
          data: actual,
          connectNulls: false,
          lineStyle: { width: 2.2, color: green },
          itemStyle: { color: green },
          areaStyle: {
            color: 'rgba(50,215,75,0.18)',
          },
        },
        {
          name: props.labels.expected,
          type: 'line',
          smooth: 0.2,
          symbol: 'none',
          data: expected,
          lineStyle: { width: 2, color: yellow },
          itemStyle: { color: yellow },
        },
        {
          name: props.labels.minimum,
          type: 'line',
          smooth: 0.2,
          symbol: 'none',
          data: min,
          lineStyle: { width: 1, color: text, type: 'dashed' },
          itemStyle: { color: text },
        },
        {
          name: props.labels.maximum,
          type: 'line',
          smooth: 0.2,
          symbol: 'none',
          data: max,
          lineStyle: { width: 1, color: orange, type: 'dashed' },
          itemStyle: { color: orange },
        },
        {
          name: props.labels.load,
          type: 'line',
          smooth: 0.2,
          symbol: 'none',
          data: load,
          lineStyle: { width: 1.6, color: blue },
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
  <div ref="el" class="solar-chart" role="img" :aria-label="`${labels.measured} / ${labels.expected}`"></div>
</template>

<style scoped>
.solar-chart {
  width: 100%;
  height: 340px;
}
</style>
