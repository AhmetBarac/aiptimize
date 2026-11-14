<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { HistoryBucket } from '../api/types'

echarts.use([BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{
  buckets: HistoryBucket[]
  locale: string
  labels: {
    pv: string
    load: string
    gridImport: string
    gridExport: string
    cost: string
    savings: string
  }
}>()

const el = ref<HTMLDivElement | null>(null)
let chart: ReturnType<typeof echarts.init> | null = null
let observer: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

function css(name: string, fallback: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

function render(): void {
  if (!chart || props.buckets.length === 0) return
  const labels = props.buckets.map((b) => b.label)
  const pv = props.buckets.map((b) => Math.round(b.pv_kwh * 10) / 10)
  const load = props.buckets.map((b) => Math.round(b.load_kwh * 10) / 10)
  const imp = props.buckets.map((b) => Math.round(b.grid_import_kwh * 10) / 10)
  const exp = props.buckets.map((b) => Math.round(b.grid_export_kwh * 10) / 10)
  const cost = props.buckets.map((b) => Math.round(b.cost * 100) / 100)
  const savings = props.buckets.map((b) => Math.round(b.savings * 100) / 100)
  const text = css('--aipt-muted', '#9a9aa2')
  const border = css('--aipt-border', 'rgba(255,255,255,.08)')
  const surface = css('--aipt-surface', '#141416')
  const green = css('--aipt-green', '#32d74b')
  const blue = css('--aipt-blue', '#0a84ff')
  const orange = css('--aipt-orange', '#ff9f0a')
  const cyan = css('--aipt-cyan', '#64d2ff')
  const yellow = css('--aipt-yellow', '#ffd60a')
  const accent = css('--aipt-accent', '#7c5cff')

  chart.setOption(
    {
      animation: false,
      grid: { left: 4, right: 52, top: 54, bottom: 4, containLabel: true },
      legend: {
        top: 0,
        left: 0,
        icon: 'roundRect',
        itemWidth: 8,
        itemHeight: 8,
        itemGap: 8,
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
            `${props.labels.pv}: ${pv[i]} kWh`,
            `${props.labels.load}: ${load[i]} kWh`,
            `${props.labels.gridImport}: ${imp[i]} kWh`,
            `${props.labels.gridExport}: ${exp[i]} kWh`,
            `${props.labels.cost}: ${cost[i]}`,
            `${props.labels.savings}: ${savings[i]}`,
          ].join('<br/>')
        },
      },
      xAxis: {
        type: 'category',
        data: labels,
        axisTick: { show: false },
        axisLine: { lineStyle: { color: border } },
        axisLabel: { color: text, fontSize: 10, interval: 'auto', hideOverlap: true },
      },
      yAxis: [
        {
          type: 'value',
          name: 'kWh',
          nameTextStyle: { color: text, fontSize: 10 },
          axisLabel: { color: text, fontSize: 10 },
          splitLine: { lineStyle: { color: border, type: 'dashed' } },
        },
        {
          type: 'value',
          position: 'right',
          axisLabel: { color: text, fontSize: 10 },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: props.labels.pv,
          type: 'line',
          data: pv,
          symbol: 'none',
          smooth: 0.2,
          lineStyle: { color: green, width: 2 },
          itemStyle: { color: green },
          areaStyle: { color: 'rgba(50,215,75,0.14)' },
        },
        {
          name: props.labels.load,
          type: 'line',
          data: load,
          symbol: 'none',
          smooth: 0.2,
          // Measured load, so a solid line distinguishes it from the estimates.
          lineStyle: { color: blue, width: 2 },
          itemStyle: { color: blue },
        },
        {
          name: props.labels.gridImport,
          type: 'bar',
          data: imp,
          itemStyle: { color: orange, borderRadius: [3, 3, 0, 0] },
          barMaxWidth: 10,
        },
        {
          name: props.labels.gridExport,
          type: 'bar',
          data: exp,
          itemStyle: { color: cyan, borderRadius: [3, 3, 0, 0] },
          barMaxWidth: 10,
        },
        {
          name: props.labels.cost,
          type: 'line',
          yAxisIndex: 1,
          data: cost,
          symbol: 'circle',
          symbolSize: 4,
          lineStyle: { color: yellow, width: 1.8 },
          itemStyle: { color: yellow },
        },
        {
          name: props.labels.savings,
          type: 'line',
          yAxisIndex: 1,
          data: savings,
          symbol: 'none',
          lineStyle: { color: accent, width: 1.5, type: 'dashed' },
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
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-bs-theme'] })
})

onBeforeUnmount(() => {
  observer?.disconnect()
  themeObserver?.disconnect()
  chart?.dispose()
  chart = null
})

watch(() => props.buckets, () => render(), { deep: true })
</script>

<template>
  <div ref="el" class="history-chart" role="img" :aria-label="`${labels.pv} / ${labels.cost}`"></div>
</template>

<style scoped>
.history-chart {
  width: 100%;
  height: 340px;
}
</style>
