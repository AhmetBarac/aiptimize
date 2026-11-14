<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import {
  GridComponent,
  MarkLineComponent,
  MarkPointComponent,
  TooltipComponent,
  VisualMapComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { PricePoint } from '../api/types'
import { formatPrice, roundPrice } from '../lib/energy'

echarts.use([
  LineChart,
  GridComponent,
  TooltipComponent,
  MarkLineComponent,
  MarkPointComponent,
  VisualMapComponent,
  CanvasRenderer,
])

const props = defineProps<{
  points: PricePoint[]
  currency: string
  timezone: string
  locale: string
  now?: string
  labelNow: string
}>()

const el = ref<HTMLDivElement | null>(null)
let chart: ReturnType<typeof echarts.init> | null = null
let observer: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null

function css(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

function formatTime(iso: string): string {
  return new Intl.DateTimeFormat(props.locale, {
    hour: '2-digit',
    minute: '2-digit',
    timeZone: props.timezone,
    hour12: false,
  }).format(new Date(iso))
}

function render(): void {
  if (!chart || props.points.length === 0) return
  const labels = props.points.map((p) => formatTime(p.delivery_start))
  const values = props.points.map((p) => roundPrice(p.price_kwh))
  const text = css('--aipt-muted', '#5b6b82')
  const border = css('--aipt-border', '#e2e8f2')
  const surface = css('--aipt-surface', '#ffffff')
  const green = css('--aipt-green', '#16a34a')
  const orange = css('--aipt-orange', '#ea7a1c')

  const data = values
  // Index, not label: a 48-hour range repeats every HH:mm, so matching by name
  // puts the marker on the wrong day.
  let markLineData: Array<{ xAxis: number }> = []
  let markPointData: Array<{ coord: [number, number]; value: string }> = []
  if (props.now) {
    const nowMs = Date.parse(props.now)
    let index = 0
    let best = Number.POSITIVE_INFINITY
    props.points.forEach((p, i) => {
      const delta = Math.abs(Date.parse(p.delivery_start) - nowMs)
      if (delta < best) {
        best = delta
        index = i
      }
    })
    if (best < 60 * 60 * 1000) {
      markLineData = [{ xAxis: index }]
      markPointData = [{ coord: [index, values[index]], value: formatPrice(values[index], props.locale) }]
    }
  }

  chart.setOption(
    {
      animation: false,
      grid: { left: 4, right: 12, top: 28, bottom: 4, containLabel: true },
      tooltip: {
        trigger: 'axis',
        // Solid and readable: this is the line the reader follows while
        // comparing values across the axis.
        axisPointer: { type: 'line', lineStyle: { color: text, type: 'dashed', width: 1.5, opacity: 0.6 } },
        backgroundColor: surface,
        borderColor: border,
        textStyle: { color: text, fontSize: 12 },
        formatter: (params: unknown) => {
          const list = params as Array<{ dataIndex: number }>
          if (!list.length) return ''
          const i = list[0].dataIndex
          return `${labels[i]}<br/><strong>${formatPrice(values[i], props.locale)} ${props.currency}/kWh</strong>`
        },
      },
      visualMap: {
        show: false,
        dimension: 1,
        min: Math.min(...values),
        max: Math.max(...values),
        inRange: { color: [green, '#e6c84b', orange] },
        seriesIndex: 0,
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
        name: `${props.currency}/kWh`,
        nameTextStyle: { color: text, fontSize: 10 },
        axisLabel: { color: text, fontSize: 10, formatter: (v: number) => v.toFixed(2) },
        splitLine: { lineStyle: { color: border, type: 'dashed' } },
      },
      series: [
        {
          type: 'line',
          name: props.currency,
          data,
          smooth: 0.35,
          symbol: 'none',
          lineStyle: { width: 2.2 },
          areaStyle: { opacity: 0.16 },
          markLine: markLineData.length
            ? {
                symbol: 'none',
                silent: true,
                label: { formatter: props.labelNow, color: text, fontSize: 10 },
                lineStyle: { color: orange, type: 'dashed', width: 1 },
                data: markLineData,
              }
            : undefined,
          markPoint: markPointData.length
            ? {
                symbol: 'circle',
                symbolSize: 9,
                itemStyle: { color: orange, borderColor: surface, borderWidth: 2 },
                label: { show: false },
                data: markPointData,
              }
            : undefined,
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
  () => [props.points, props.currency, props.now],
  () => render(),
  { deep: true },
)
</script>

<template>
  <div
    ref="el"
    class="price-chart"
    role="img"
    :aria-label="`${currency}/kWh, ${points.length} values`"
  ></div>
</template>

<style scoped>
.price-chart {
  width: 100%;
  height: 300px;
}
</style>
