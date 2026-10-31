<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../api/client'
import type {
  PriceBlock,
  PricePoint,
  PriceSnapshot,
  PriceWindow,
  SettingsDocument,
} from '../api/types'
import { session } from '../session'
import { formatPrice } from '../lib/energy'
import PageHead from '../components/PageHead.vue'
import PriceChart from '../components/PriceChart.vue'
import SegmentedControl from '../components/SegmentedControl.vue'

const { t, locale } = useI18n()

const snapshot = ref<PriceSnapshot | null>(null)
const settings = ref<SettingsDocument | null>(null)
const loading = ref(true)
const error = ref('')
const message = ref('')
const refreshing = ref(false)
const activeDay = ref('')
const resolution = ref<'15' | '60'>('15')
const nowIso = ref(new Date().toISOString())

const tz = computed(() => snapshot.value?.timezone ?? 'UTC')
const currency = computed(() => snapshot.value?.currency ?? '')
const fullPrice = computed(() => session.meta?.site.show_full_price === true)
const priceModeLabel = computed(() => (fullPrice.value ? t('prices.import') : t('prices.spot')))
const manualTariff = computed(
  () => settings.value?.site.tariff_mode !== 'api',
)
const tariffModeLabel = computed(() =>
  manualTariff.value ? t('prices.manualTariff') : t('prices.apiTariff'),
)

function displayImport(block: PriceBlock): number {
  return fullPrice.value ? block.import_price : block.price_kwh
}

function displayExport(block: PriceBlock): number {
  return fullPrice.value ? block.export_price : block.price_kwh
}

function dayKey(iso: string): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: tz.value,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date(iso))
  const value = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  return `${value('year')}-${value('month')}-${value('day')}`
}

interface DayGroup {
  key: string
  start: string
}

const groups = computed<DayGroup[]>(() => {
  const map = new Map<string, string>()
  for (const block of snapshot.value?.blocks ?? []) {
    const key = dayKey(block.start)
    if (!map.has(key)) map.set(key, block.start)
  }
  return [...map.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([key, start]) => ({ key, start }))
})

const blocks = computed(() =>
  (snapshot.value?.blocks ?? []).filter((b) => dayKey(b.start) === activeDay.value),
)
const points = computed(() =>
  (snapshot.value?.points ?? []).filter((p) => dayKey(p.delivery_start) === activeDay.value),
)
const rawChartPoints = computed<PricePoint[]>(() => {
  if (resolution.value === '15' || points.value.length < 2) return points.value
  const hourly = new Map<string, PricePoint[]>()
  for (const point of points.value) {
    const hour = new Intl.DateTimeFormat('en-CA', {
      timeZone: tz.value,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      hourCycle: 'h23',
    }).format(new Date(point.delivery_start))
    const list = hourly.get(hour) ?? []
    list.push(point)
    hourly.set(hour, list)
  }
  return [...hourly.values()].map((list) => {
    const first = list[0]
    const last = list[list.length - 1]
    return {
      delivery_start: first.delivery_start,
      delivery_end: last.delivery_end,
      price_mwh: list.reduce((sum, item) => sum + item.price_mwh, 0) / list.length,
      price_kwh: list.reduce((sum, item) => sum + item.price_kwh, 0) / list.length,
      state: list.some((item) => item.state === 'Preliminary') ? 'Preliminary' : first.state,
    }
  })
})

function blockForPoint(iso: string): PriceBlock | null {
  const at = Date.parse(iso)
  return blocks.value.find((block) => Date.parse(block.start) <= at && Date.parse(block.end) > at) ?? null
}

const chartPoints = computed<PricePoint[]>(() => {
  if (!fullPrice.value) return rawChartPoints.value
  return rawChartPoints.value.map((point) => {
    const block = blockForPoint(point.delivery_start)
    if (!block) return point
    return { ...point, price_kwh: block.import_price, price_mwh: block.import_price * 1000 }
  })
})
const resolutionOptions = computed(() => [
  { value: '15', label: t('prices.resolution15') },
  { value: '60', label: t('prices.resolution60') },
])

const stats = computed(() => {
  const valid = blocks.value.filter((b) => !b.missing)
  if (!valid.length) return null
  const prices = valid.map(displayImport)
  return {
    min: Math.min(...prices),
    max: Math.max(...prices),
    avg: prices.reduce((a, b) => a + b, 0) / prices.length,
  }
})

const dayState = computed(() => {
  const list = blocks.value
  if (!list.length) return 'Missing'
  if (list.some((b) => b.state === 'Preliminary')) return 'Preliminary'
  if (list.some((b) => b.state === 'Missing')) return 'Missing'
  return 'Final'
})

const updated = computed(() => {
  const at = snapshot.value?.fetched_at
  if (!at) return ''
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
    hourCycle: 'h23',
    timeZone: tz.value,
  }).format(new Date(at))
})

// Cheapest two-hour windows within the selected day, computed from its blocks
// so the card always follows the active day tab.
const cheapestWindows = computed<PriceWindow[]>(() => {
  const span = 4
  const valid = blocks.value.filter((b) => !b.missing && b.points >= 2)
  if (valid.length < span) return []
  const candidates: PriceWindow[] = []
  for (let i = 0; i + span <= valid.length; i++) {
    const slice = valid.slice(i, i + span)
    let contiguous = true
    for (let j = 1; j < slice.length; j++) {
      if (Date.parse(slice[j].start) !== Date.parse(slice[j - 1].end)) {
        contiguous = false
        break
      }
    }
    if (!contiguous) continue
    candidates.push({
      start: slice[0].start,
      end: slice[span - 1].end,
      avg_import_price: slice.reduce((a, b) => a + displayImport(b), 0) / span,
      avg_export_price: slice.reduce((a, b) => a + displayExport(b), 0) / span,
      blocks: span,
    })
  }
  candidates.sort(
    (a, b) =>
      a.avg_import_price - b.avg_import_price || Date.parse(b.start) - Date.parse(a.start),
  )
  const picked: PriceWindow[] = []
  for (const candidate of candidates) {
    if (picked.length === 3) break
    const overlaps = picked.some(
      (p) => Date.parse(candidate.start) < Date.parse(p.end) && Date.parse(p.start) < Date.parse(candidate.end),
    )
    if (!overlaps) picked.push(candidate)
  }
  return picked.sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
})

const currentBlock = computed(() => {
  const at = Date.parse(nowIso.value)
  return blocks.value.find((block) => Date.parse(block.start) <= at && Date.parse(block.end) > at) ?? null
})
const gauge = computed(() => {
  const value = currentBlock.value ? displayImport(currentBlock.value) : stats.value?.avg ?? 0
  const min = stats.value?.min ?? value
  const max = stats.value?.max ?? value
  const pct = max > min ? Math.max(0, Math.min(1, (value - min) / (max - min))) : 0.5
  const color = pct >= 0.66 ? 'var(--aipt-orange)' : pct >= 0.33 ? '#e6c84b' : 'var(--aipt-green)'
  return { value, pct, color, start: currentBlock.value?.start, end: currentBlock.value?.end }
})
const nextBlock = computed(() => {
  const at = Date.parse(nowIso.value)
  return blocks.value.find((block) => Date.parse(block.start) > at) ?? null
})
const cheapestNow = computed(() => cheapestWindows.value[0] ?? null)
const tariffSample = computed(
  () => currentBlock.value ?? blocks.value.find((block) => !block.missing) ?? null,
)
const todayKey = computed(() => dayKey(nowIso.value))
const tomorrowKey = computed(() => {
  const d = new Date(nowIso.value)
  d.setUTCDate(d.getUTCDate() + 1)
  return dayKey(d.toISOString())
})
const dayOptions = computed(() =>
  groups.value.map((group) => ({
    value: group.key,
    label:
      group.key === todayKey.value
        ? t('prices.today')
        : group.key === tomorrowKey.value
          ? t('prices.tomorrow')
          : dayShort(group.start),
  })),
)

function time(iso: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: tz.value,
  }).format(new Date(iso))
}

function dayShort(iso: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    timeZone: tz.value,
  }).format(new Date(iso))
}

function money(value: number | null | undefined): string {
  return formatPrice(value, locale.value)
}

function windowLabel(window: PriceWindow): string {
  return `${dayShort(window.start)} ${time(window.start)}–${time(window.end)}`
}

function stateLabel(state: string): string {
  if (state === 'Preliminary') return t('prices.statePreliminary')
  if (state === 'Missing') return t('prices.stateMissing')
  return t('prices.stateFinal')
}

function stateClass(state: string): string {
  if (state === 'Final') return 'badge-on'
  if (state === 'Preliminary') return 'badge-warn'
  return 'badge-off'
}

function defaultDay(): string {
  const today = dayKey(new Date().toISOString())
  return groups.value.find((g) => g.key === today)?.key ?? groups.value[0]?.key ?? ''
}

async function load(showSpinner = true): Promise<void> {
  if (showSpinner) loading.value = true
  error.value = ''
  try {
    const [priceSnapshot, settingsDocument] = await Promise.all([
      api.get<PriceSnapshot>('/prices'),
      api.get<SettingsDocument>('/settings').catch(() => null),
    ])
    snapshot.value = priceSnapshot
    settings.value = settingsDocument
    if (!groups.value.some((g) => g.key === activeDay.value)) {
      activeDay.value = defaultDay()
    }
    nowIso.value = new Date().toISOString()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('errors.network')
  } finally {
    loading.value = false
  }
}

async function refresh(): Promise<void> {
  refreshing.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await api.post<{ results: Array<{ ok: boolean; code?: string }> }>(
      '/prices/refresh',
      {},
    )
    const updatedDays = result.results.filter((r) => r.ok).length
    message.value =
      updatedDays > 0
        ? t('prices.refreshed', { count: updatedDays })
        : t('prices.refreshNotPublished')
    await load(false)
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('prices.refreshFailed')
  } finally {
    refreshing.value = false
  }
}

onMounted(() => load())
</script>

<template>
  <PageHead :title="t('prices.title')" :subtitle="t('prices.subtitle')">
    <button class="btn btn-primary btn-sm" type="button" :disabled="refreshing" @click="refresh">
      <i class="bi bi-arrow-clockwise" :class="{ spin: refreshing }" aria-hidden="true"></i>
      <span class="ms-1">{{ refreshing ? t('prices.refreshing') : t('prices.refresh') }}</span>
    </button>
  </PageHead>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="message" class="alert alert-success py-2 small" role="status">{{ message }}</div>

  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="snapshot && snapshot.points.length">
    <div class="d-flex flex-wrap align-items-center gap-2 mb-2">
      <span class="badge-soft" :class="stateClass(dayState)">{{ stateLabel(dayState) }}</span>
      <span v-if="snapshot.stale" class="badge-soft badge-warn">{{ t('prices.stale') }}</span>
      <span class="ms-auto small text-muted">
        {{ t('prices.pointsBlocks', { points: snapshot.points.length, blocks: snapshot.blocks.length }) }}
      </span>
    </div>

    <section class="price-tibber-hero mb-3">
      <div
        class="price-gauge"
        :style="{ '--gauge-pct': `${Math.max(4, Math.round(gauge.pct * 100))}%`, '--gauge-color': gauge.color }"
      >
        <div class="price-gauge-inner">
          <span>{{ t('prices.now') }}</span>
          <strong class="tabular">{{ money(gauge.value) }}</strong>
          <small>{{ currency }}/kWh</small>
          <em v-if="gauge.start && gauge.end">{{ time(gauge.start) }}–{{ time(gauge.end) }}</em>
        </div>
      </div>
      <div class="price-hero-side">
        <div class="price-side-stat">
          <span>{{ t('prices.next') }}</span>
          <strong class="tabular">{{ nextBlock ? money(displayImport(nextBlock)) : '—' }}</strong>
          <small>{{ nextBlock ? time(nextBlock.start) : '' }}</small>
        </div>
        <div class="price-side-stat">
          <span>{{ t('prices.cheapestShort') }}</span>
          <strong class="tabular">{{ cheapestNow ? money(cheapestNow.avg_import_price) : '—' }}</strong>
          <small>{{ cheapestNow ? `${time(cheapestNow.start)}–${time(cheapestNow.end)}` : '' }}</small>
        </div>
      </div>
    </section>

    <div class="row g-3">
      <div class="col-12 col-xl-8">
        <section class="aipt-card h-100">
          <div class="price-chart-toolbar">
            <SegmentedControl
              v-model="resolution"
              :options="resolutionOptions"
              :group-label="t('prices.chartTitle')"
              size="sm"
            />
            <span class="small text-muted">{{ snapshot.area }} · {{ snapshot.currency }}</span>
          </div>
          <PriceChart
            :points="chartPoints"
            :currency="snapshot.currency"
            :timezone="snapshot.timezone"
            :locale="locale"
            :now="nowIso"
            :label-now="t('prices.now')"
          />
        </section>
      </div>
      <div class="col-12 col-xl-4">
        <section class="aipt-card mb-3">
          <h2>{{ priceModeLabel }} ({{ currency }}/kWh)</h2>
          <div class="price-stats">
            <div>
              <span class="tile-label">{{ t('prices.min') }}</span>
              <strong>{{ money(stats?.min) }}</strong>
            </div>
            <div>
              <span class="tile-label">{{ t('prices.max') }}</span>
              <strong>{{ money(stats?.max) }}</strong>
            </div>
            <div>
              <span class="tile-label">{{ t('prices.average') }}</span>
              <strong>{{ money(stats?.avg) }}</strong>
            </div>
          </div>
        </section>
        <section class="aipt-card">
          <h2>{{ t('prices.cheapestWindows') }}</h2>
          <ul v-if="cheapestWindows.length" class="cheapest-list">
            <li v-for="window in cheapestWindows" :key="window.start">
              <span>{{ windowLabel(window) }}</span>
              <strong class="tabular">{{ money(window.avg_import_price) }} {{ currency }}</strong>
            </li>
          </ul>
          <p v-else class="empty-state mb-0">{{ t('prices.min') }} —</p>
        </section>
      </div>
    </div>

    <div class="price-day-tabs mt-3">
      <SegmentedControl v-model="activeDay" :options="dayOptions" :group-label="t('prices.title')" size="sm" />
      <span class="badge-soft" :class="stateClass(dayState)">{{ stateLabel(dayState) }}</span>
    </div>

    <section class="aipt-card mt-3">
      <h2>{{ t('prices.blocks') }}</h2>
      <div class="price-table-wrap">
        <table class="table table-sm align-middle price-table mb-0">
          <thead>
            <tr>
              <th scope="col">{{ t('prices.time') }}</th>
              <th scope="col" class="text-end">{{ t('prices.spot') }}</th>
              <th v-if="fullPrice" scope="col" class="text-end">{{ t('prices.import') }}</th>
              <th v-if="fullPrice" scope="col" class="text-end d-none d-sm-table-cell">{{ t('prices.export') }}</th>
              <th scope="col">{{ t('prices.state') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="block in blocks" :key="block.start" :class="{ 'row-missing': block.missing }">
              <td class="tabular">{{ time(block.start) }}–{{ time(block.end) }}</td>
              <td class="text-end tabular">{{ money(block.price_kwh) }}</td>
              <td
                v-if="fullPrice"
                class="text-end tabular fw-semibold"
                :class="{ 'price-negative': block.import_price < 0 }"
              >
                {{ money(block.import_price) }}
              </td>
              <td v-if="fullPrice" class="text-end tabular d-none d-sm-table-cell">
                {{ money(block.export_price) }}
              </td>
              <td>
                <span class="badge-soft" :class="stateClass(block.state)">
                  {{ block.missing ? t('prices.missing') : stateLabel(block.state) }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="aipt-card mt-3">
      <div class="d-flex flex-wrap align-items-center gap-2">
        <h2 class="mb-0">{{ t('prices.tariff') }}</h2>
        <span class="badge-soft">{{ tariffModeLabel }}</span>
      </div>

      <template v-if="!manualTariff">
        <div class="row g-2 small mt-1">
          <div class="col-12 col-md-6">
            <div class="tile-label">{{ t('settings.gridOwner') }}</div>
            <div>{{ settings?.site.grid_owner || '—' }}</div>
          </div>
          <div class="col-12 col-md-6">
            <div class="tile-label">{{ t('settings.facilityID') }}</div>
            <div class="mono">{{ settings?.site.facility_id || '—' }}</div>
          </div>
          <div class="col-12 col-md-6">
            <div class="tile-label">{{ t('settings.tariffProduct') }}</div>
            <div>{{ settings?.site.grid_tariff_product || '—' }}</div>
          </div>
          <div class="col-12 col-md-6">
            <div class="tile-label">{{ t('settings.eltariffURL') }}</div>
            <div class="mono small text-break">{{ settings?.site.eltariff_url || '—' }}</div>
          </div>
        </div>
        <p class="small text-muted mt-3 mb-0">{{ t('prices.apiTariffHint') }}</p>
      </template>

      <h3 class="h6 mt-3">{{ t('prices.currentPrices') }}</h3>
      <div class="row g-2 small">
        <div class="col-6 col-md-3">
          <div class="tile-label">{{ t('prices.spot') }}</div>
          <div class="tabular">{{ money(tariffSample?.price_kwh ?? null) }} {{ currency }}/kWh</div>
        </div>
        <div class="col-6 col-md-3">
          <div class="tile-label">{{ t('prices.import') }}</div>
          <div class="tabular">{{ money(tariffSample?.import_price ?? null) }} {{ currency }}/kWh</div>
        </div>
        <div class="col-6 col-md-3">
          <div class="tile-label">{{ t('prices.export') }}</div>
          <div class="tabular">{{ money(tariffSample?.export_price ?? null) }} {{ currency }}/kWh</div>
        </div>
      </div>

      <p class="small text-muted mt-3 mb-0">{{ t('prices.tariffFormula') }}</p>
    </section>

    <footer class="price-attribution">
      <span>{{ snapshot.attribution }}</span>
      <span v-if="updated"> · {{ t('prices.updated', { time: updated }) }}</span>
    </footer>
  </template>

  <section v-else class="aipt-card phase-notice">
    <div class="phase-icon" aria-hidden="true"><i class="bi bi-graph-up"></i></div>
    <h2>{{ t('prices.noData') }}</h2>
    <p>{{ t('prices.noDataHint') }}</p>
    <button class="btn btn-primary" type="button" :disabled="refreshing" @click="refresh">
      {{ refreshing ? t('prices.refreshing') : t('prices.refresh') }}
    </button>
  </section>
</template>
