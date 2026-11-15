<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api, ApiError } from '../api/client'
import type {
  EVConfig,
  ManagedUser,
  PVStringSettings,
  PlanApplyResult,
  PlanView,
  Role,
  SettingsDocument,
  Status,
  UpdateStatus,
} from '../api/types'
import { loadMeta, loadSettings, patchSettings, session } from '../session'
import PageHead from '../components/PageHead.vue'
import Sheet from '../components/Sheet.vue'

const { t, locale } = useI18n()

// Account management (administrators only): two roles, admin and user.
const users = ref<ManagedUser[]>([])
const userForm = reactive<{ username: string; password: string; role: Role }>({
  username: '',
  password: '',
  role: 'user',
})
const savingUser = ref(false)
const userError = ref('')
const userMessage = ref('')

async function loadUsers(): Promise<void> {
  if (session.user?.role !== 'admin') return
  try {
    users.value = (await api.get<{ users: ManagedUser[] }>('/users')).users
    userError.value = ''
  } catch (err) {
    userError.value = err instanceof Error ? err.message : String(err)
  }
}

async function createUser(): Promise<void> {
  savingUser.value = true
  userError.value = ''
  userMessage.value = ''
  try {
    await api.post('/users', {
      username: userForm.username.trim(),
      password: userForm.password,
      role: userForm.role,
    })
    userForm.username = ''
    userForm.password = ''
    userForm.role = 'user'
    userMessage.value = t('settings.userAdded')
    await loadUsers()
  } catch (err) {
    userError.value = err instanceof Error ? err.message : String(err)
  } finally {
    savingUser.value = false
  }
}

async function removeUser(item: ManagedUser): Promise<void> {
  if (!window.confirm(t('settings.confirmRemoveUser', { name: item.username }))) return
  savingUser.value = true
  userError.value = ''
  userMessage.value = ''
  try {
    await api.del(`/users/${item.id}`)
    userMessage.value = t('settings.userRemoved')
    await loadUsers()
  } catch (err) {
    userError.value = err instanceof Error ? err.message : String(err)
  } finally {
    savingUser.value = false
  }
}
const route = useRoute()

const sections = ['site', 'inverter', 'solar', 'load', 'plan', 'ev', 'safety', 'system', 'users'] as const
const section = computed(() => {
  const value = String(route.params.section ?? 'site')
  return (sections as readonly string[]).includes(value) ? value : 'site'
})

const settings = ref<SettingsDocument | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const saved = ref('')
const planHelpOpen = ref(false)
const evConfig = ref<EVConfig | null>(null)
const evProviderBusy = ref(false)
const evCredentialsReady = computed(
  () =>
    evConfig.value?.credentials.zaptec === true ||
    (evForm.zaptecUsername.trim() !== '' && evForm.zaptecPassword !== ''),
)
const settingInfoOpen = ref(false)
const settingInfoTitle = ref('')
const settingInfoText = ref('')

const settingInfoKeys: Record<string, string> = {
  name: 'siteName',
  tz: 'timezone',
  lat: 'latitude',
  lon: 'longitude',
  'max-import': 'maxImport',
  'grid-owner': 'gridOwner',
  'facility-id': 'facilityID',
  'tariff-product': 'tariffProduct',
  'eltariff-url': 'eltariffURL',
  'tariff-mode': 'tariffMode',
  'transfer-fee': 'transferFeeOre',
  'energy-tax': 'energyTaxOre',
  'variable-cost': 'variableCostOre',
  'fixed-markup': 'fixedMarkupOre',
  'export-benefit': 'exportBenefitOre',
  'export-extra': 'exportExtraOre',
  'show-full-price': 'showFullPrice',
  'site-license': 'license',
  'inv-host': 'host',
  'inv-scheme': 'scheme',
  'inv-port': 'port',
  'inv-serial': 'serial',
  'inv-capacity': 'capacity',
  'inv-interval': 'telemetryInterval',
  'inv-max-charge': 'maxCharge',
  'inv-max-discharge': 'maxDischarge',
  'sign-pv': 'sign',
  'sign-grid': 'sign',
  'sign-battery': 'sign',
  'pv-ac': 'acLimit',
  'pv-conf': 'confidence',
  'pv-refresh': 'refresh',
  'pv-cal': 'calibration',
  'load-kwh': 'dailyKwh',
  'load-measured': 'useMeasured',
  'load-temperature': 'useTemperature',
  'temperature-reference': 'temperatureReference',
  'temperature-sensitivity': 'temperatureSensitivity',
  'plan-slots': 'maxSlots',
  'plan-min': 'maxSlotMin',
  'plan-days': 'horizonDays',
  'plan-saving': 'minDailySaving',
  'plan-charge': 'chargeW',
  'plan-discharge': 'dischargeW',
  'plan-end': 'endSoc',
  'plan-min-soc': 'minSoc',
  'plan-max-soc': 'maxSoc',
  'plan-reserve-soc': 'reserveSoc',
  'plan-charge-eff': 'chargeEfficiency',
  'plan-discharge-eff': 'dischargeEfficiency',
  'plan-preserve': 'preserveSoc',
  'plan-grid': 'allowGrid',
  'settings-plan-strategy': 'strategy',
  'settings-min-diff': 'minPriceDifference',
  'settings-max-purchase': 'maxPurchasePrice',
  'settings-peak-shaving': 'peakShaving',
  'settings-peak-limit': 'peakLimit',
  'ev-mode': 'evMode',
  'ev-provider': 'evProvider',
  'ev-charger-id': 'evChargerID',
  'ev-installation-id': 'evInstallationID',
  'ev-zaptec-username': 'evZaptecUsername',
  'ev-zaptec-password': 'evZaptecPassword',
  'ev-min': 'evCurrent',
  'ev-max': 'evCurrent',
  'ev-phases': 'evPhases',
  'ev-voltage': 'evVoltage',
  'ev-solar': 'evSolarShare',
  'ev-floor': 'evBatteryFloor',
  'ev-always': 'evAlwaysCharge',
  'ev-cost': 'evSmartCost',
  'ev-feed': 'evFeedIn',
  'ev-grid': 'evSolarOnly',
  'ev-battery': 'evAllowBatteryCharging',
  writes: 'writesArm',
  ev: 'evArm',
  watchdog: 'watchdog',
  horizon: 'horizonLimit',
  'auto-write': 'autoScheduleWrite',
}

function settingInfoKey(id: string): string {
  if (settingInfoKeys[id]) return settingInfoKeys[id]
  if (/^(kwp|tilt|azimuth|pr|temp|max)-/.test(id)) {
    const prefix = id.split('-')[0]
    return { kwp: 'kwp', tilt: 'tilt', azimuth: 'azimuth', pr: 'pr', temp: 'tempCoefficient', max: 'maxW' }[prefix] ?? ''
  }
  if (id.startsWith('profile-')) return 'hourlyProfile'
  return ''
}

function decorateSettings(): void {
  document.querySelectorAll('.setting-info-btn[data-generated]').forEach((node) => node.remove())
  if (!settings.value) return
  document.querySelectorAll<HTMLLabelElement>('.aipt-card label[for]').forEach((label) => {
    const key = settingInfoKey(label.htmlFor)
    if (!key) return
    const title = (label.textContent ?? '').replace(/\s+/g, ' ').trim()
    label.classList.add('setting-label-with-info')
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'setting-info-btn'
    button.dataset.generated = 'true'
    button.setAttribute('aria-label', `${title}: ${t('settings.settingHelp')}`)
    button.innerHTML = '<i class="bi bi-info-circle" aria-hidden="true"></i>'
    button.addEventListener('click', (event) => {
      event.preventDefault()
      event.stopPropagation()
      settingInfoTitle.value = title || t('settings.settingHelp')
      settingInfoText.value = t(`settings.info.${key}`)
      settingInfoOpen.value = true
    })
    label.appendChild(button)
  })
}

watch(section, async () => {
  await nextTick()
  decorateSettings()
})

const siteForm = reactive({
  name: '',
  timezone: '',
  latitude: 0,
  longitude: 0,
  maxImport: 0,
  gridOwner: '',
  facilityID: '',
  tariffProduct: '',
  eltariffURL: '',
  tariffMode: 'manual',
  transferFeeOre: 0,
  energyTaxOre: 0,
  variableCostOre: 0,
  fixedMarkupOre: 0,
  exportBenefitOre: 0,
  exportExtraOre: 0,
  showFullPrice: false,
  license: '',
})

const licenseSet = ref(false)

const timezoneOptions = computed(() => {
  const supported = (
    Intl as unknown as { supportedValuesOf?: (key: string) => string[] }
  ).supportedValuesOf?.('timeZone') ?? []
  const values = [...supported, siteForm.timezone, 'UTC'].filter(Boolean)
  return [...new Set(values)].sort((a, b) => a.localeCompare(b))
})

const inverterForm = reactive({
  host: '',
  scheme: 'http',
  port: 8484,
  serial: '',
  maxCharge: 6000,
  maxDischarge: 3000,
  capacity: 15.36,
  interval: 30,
  signPv: 1,
  signGrid: 1,
  signBattery: 1,
})

interface InverterTestResult {
  ok: boolean
  identity: {
    serial: string
    model: string
    firmware: string
    rated_power_w: number
    capacity_kwh: number
    work_mode: number
  }
  telemetry: {
    soc: number
    pv_w: number
    grid_w: number
    battery_w: number
    load_w: number
    ts: string
  }
}

const testing = ref(false)
const testResult = ref<InverterTestResult | null>(null)
const testError = ref('')

const safetyForm = reactive({
  writes: false,
  ev: false,
  watchdog: 1800,
  horizon: 2,
  autoWrite: 'off' as 'off' | 'when_changed',
  foreignTakeover: false,
})

const scheduleForeign = ref(false)
const scheduleUnknown = ref(false)
const takeoverBusy = ref(false)
const takeoverDone = ref('')
const takeoverError = ref('')
const writesArmed = computed(() => session.meta?.gates.writes_armed === true)
const evGateModeLabel = computed(() => {
  const mode = session.meta?.gates.ev_control_mode ?? 'auto'
  if (mode === 'on') return t('settings.evGateModeOn')
  if (mode === 'off') return t('settings.evGateModeOff')
  return t('settings.evGateModeAuto')
})
const canTakeControl = computed(
  () => writesArmed.value && scheduleForeign.value && !scheduleUnknown.value,
)
const scheduleStateLabel = computed(() => {
  if (scheduleUnknown.value) return t('settings.scheduleUnknown')
  if (scheduleForeign.value) return t('settings.scheduleForeign')
  return t('settings.scheduleOwned')
})

type PreconditionStatus = 'success' | 'pending' | 'failed'

interface Precondition {
  key: string
  label: string
  detail: string
  status: PreconditionStatus
}

const gateInfoOpen = ref(false)
const statusSnapshot = ref<Status | null>(null)

// The planner refuses to plan from telemetry older than this, so it is the
// threshold that matters here; /status.stale flips much earlier (90 s).
const writeTelemetryLimitS = 300

const preconditions = computed<Precondition[]>(() => {
  const gates = session.meta?.gates
  const telemetry = statusSnapshot.value?.telemetry
  const reserve = settings.value?.inverter.reserve_soc ?? 0
  const soc = telemetry?.sample?.soc
  const age = telemetry?.age_s

  let telemetryStatus: PreconditionStatus = 'pending'
  let telemetryDetail = t('settings.preconditionTelemetryMissing')
  if (telemetry?.available && telemetry.sample) {
    const ageS = Math.round(age ?? 0)
    if (ageS > writeTelemetryLimitS) {
      telemetryStatus = 'failed'
      telemetryDetail = t('settings.preconditionTelemetryStale', { age: ageS })
    } else {
      telemetryStatus = 'success'
      telemetryDetail = t('settings.preconditionTelemetryFresh', {
        age: ageS,
        soc: Math.round(soc ?? 0),
      })
    }
  }

  let socStatus: PreconditionStatus = 'pending'
  let socDetail = t('settings.preconditionSocUnknown')
  if (soc !== undefined) {
    socStatus = soc > reserve ? 'success' : 'failed'
    socDetail = t('settings.preconditionSocDetail', { soc: Math.round(soc), reserve })
  }

  let ownerStatus: PreconditionStatus = 'success'
  let ownerDetail = t('settings.preconditionForeignOk')
  if (scheduleUnknown.value) {
    ownerStatus = 'pending'
    ownerDetail = t('settings.preconditionForeignUnknown')
  } else if (scheduleForeign.value) {
    ownerStatus = 'failed'
    ownerDetail = t('settings.preconditionForeignFailed')
  }

  return [
    {
      key: 'env',
      label: t('settings.preconditionEnv'),
      detail: gates?.writes_env ? t('settings.preconditionEnvOn') : t('settings.preconditionEnvOff'),
      status: gates?.writes_env ? 'success' : 'failed',
    },
    {
      key: 'ui',
      label: t('settings.preconditionUI'),
      detail: gates?.writes_ui ? t('settings.preconditionUIOn') : t('settings.preconditionUIOff'),
      status: gates?.writes_ui ? 'success' : 'failed',
    },
    { key: 'telemetry', label: t('settings.preconditionTelemetry'), detail: telemetryDetail, status: telemetryStatus },
    { key: 'soc', label: t('settings.preconditionSoc'), detail: socDetail, status: socStatus },
    { key: 'owner', label: t('settings.preconditionForeign'), detail: ownerDetail, status: ownerStatus },
  ]
})

function preconditionIcon(status: PreconditionStatus): string {
  if (status === 'success') return 'bi-check-circle-fill'
  if (status === 'failed') return 'bi-x-circle-fill'
  return 'bi-hourglass-split'
}

function preconditionTone(status: PreconditionStatus): string {
  if (status === 'success') return 'success'
  if (status === 'failed') return 'danger'
  return 'warning'
}

async function refreshStatus(): Promise<void> {
  try {
    statusSnapshot.value = await api.get<Status>('/status')
  } catch {
    statusSnapshot.value = null
  }
}

async function openGateInfo(): Promise<void> {
  gateInfoOpen.value = true
  await Promise.all([refreshStatus(), loadScheduleOwnership()])
}

const update = ref<UpdateStatus | null>(null)
const updateBusy = ref(false)
const updateApplying = ref(false)
const updateConfirming = ref(false)
const updateError = ref('')
const updateApplyError = ref('')

async function checkUpdate(): Promise<void> {
  updateBusy.value = true
  updateError.value = ''
  updateApplyError.value = ''
  updateConfirming.value = false
  try {
    update.value = await api.get<UpdateStatus>('/system/update')
  } catch (err) {
    update.value = null
    if (err instanceof ApiError) {
      const details = err.details as { error?: string } | undefined
      updateError.value = details?.error ?? err.message
    } else {
      updateError.value = t('settings.updateCheckFailed')
    }
  } finally {
    updateBusy.value = false
  }
}

// The confirmation is an in-page panel rather than window.confirm(), which
// browsers can suppress, leaving the click silently ignored.
async function applyUpdate(): Promise<void> {
  updateApplying.value = true
  updateApplyError.value = ''
  const previous = update.value?.current ?? ''
  try {
    await api.post<{ updating: boolean }>('/system/update/apply', { confirm: true })
  } catch (err) {
    // The container can restart before the response arrives; the trigger may
    // still have been written, so treat a network error as "keep waiting".
    if (!(err instanceof ApiError) || err.status !== 0) {
      updateApplying.value = false
      if (err instanceof ApiError) {
        const details = err.details as { error?: string } | undefined
        const detail = typeof details?.error === 'string' ? details.error : ''
        updateApplyError.value = detail ? `${err.message} ${detail}` : err.message
      } else {
        updateApplyError.value = t('settings.updateFailed')
      }
      return
    }
  }
  updateConfirming.value = false
  await waitForRestart(previous)
}

// The container is replaced by the host, so wait for /meta to report a new
// version before reloading the SPA.
async function waitForRestart(previous: string): Promise<void> {
  for (let attempt = 0; attempt < 45; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 2000))
    try {
      const response = await fetch('/api/v1/meta', { cache: 'no-store' })
      if (!response.ok) continue
      const meta = (await response.json()) as { version?: string }
      if (meta.version && meta.version !== previous) {
        window.location.reload()
        return
      }
    } catch {
      // The server is restarting; keep polling.
    }
  }
  updateApplying.value = false
  updateApplyError.value = t('settings.updateRestartTimeout')
}

function formatBuildDate(value?: string): string {
  if (!value) return t('settings.unknownValue')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' })
}

const pvForm = reactive({
  strings: [] as PVStringSettings[],
  acLimit: 8000,
  confidence: 0.7,
  refresh: 30,
  calibration: false,
})

const loadForm = reactive({
  dailyKwh: 12,
  useMeasured: false,
  useTemperature: false,
  temperatureReference: 18,
  temperatureSensitivity: 2,
  profile: [] as number[],
})

const planForm = reactive({
  maxSlots: 4,
  maxSlotMin: 240,
  horizonDays: 2,
  minDailySaving: 1,
  preserveSoc: true,
  endSoc: 50,
  minSoc: 15,
  maxSoc: 100,
  reserveSoc: 15,
  chargeEfficiency: 0.95,
  dischargeEfficiency: 0.95,
  chargeW: 6000,
  dischargeW: 3000,
  allowGrid: true,
  strategy: 'minimize_cost',
  minPriceDifference: 0,
  maxPurchasePrice: 0,
  peakShaving: false,
  peakLimitW: 0,
})

const evForm = reactive({
  provider: 'zaptec',
  chargerID: '',
  installationID: '',
  zaptecUsername: '',
  zaptecPassword: '',
  mode: 'off',
  minCurrent: 6,
  maxCurrent: 16,
  phases: 3,
  voltage: 230,
  solarSharePct: 100,
  solarOnly: true,
  allowBatteryCharging: false,
  alwaysCharge: 'off',
  batteryFloor: 20,
  smartCostLimit: '' as number | '',
  feedInLimit: '' as number | '',
})

onMounted(load)

// After an in-app update the container is replaced; retry briefly so a restart
// window does not surface as an error banner.
async function loadSettingsWithRetry(): Promise<SettingsDocument> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await loadSettings()
    } catch (err) {
      const transient = err instanceof ApiError && err.status === 0
      if (!transient || attempt >= 5) throw err
      await new Promise((resolve) => setTimeout(resolve, 2000))
    }
  }
}

async function load(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const doc = await loadSettingsWithRetry()
    settings.value = doc
    siteForm.name = doc.site.name
    siteForm.timezone = doc.site.timezone
    siteForm.latitude = doc.site.latitude
    siteForm.longitude = doc.site.longitude
    siteForm.maxImport = Math.round(doc.site.max_import_power_w / 1000)
    siteForm.gridOwner = doc.site.grid_owner ?? ''
    siteForm.facilityID = doc.site.facility_id ?? ''
    siteForm.tariffProduct = doc.site.grid_tariff_product ?? ''
    siteForm.eltariffURL = doc.site.eltariff_url ?? ''
    siteForm.tariffMode = doc.site.tariff_mode || 'manual'
    siteForm.transferFeeOre = doc.site.transfer_fee_ore_kwh ?? 0
    siteForm.energyTaxOre = doc.site.energy_tax_ore_kwh ?? 0
    siteForm.variableCostOre = doc.site.variable_cost_ore_kwh ?? 0
    siteForm.fixedMarkupOre = doc.site.fixed_markup_ore_kwh ?? 0
    siteForm.exportBenefitOre = doc.site.export_benefit_ore_kwh ?? 0
    siteForm.exportExtraOre = doc.site.export_extra_ore_kwh ?? 0
    siteForm.showFullPrice = doc.site.show_full_price === true
    siteForm.license = ''
    licenseSet.value = doc.site.license_set === true
    inverterForm.host = doc.inverter.host
    inverterForm.scheme = doc.inverter.scheme
    inverterForm.port = doc.inverter.port
    inverterForm.serial = doc.inverter.serial
    inverterForm.maxCharge = doc.inverter.max_charge_w
    inverterForm.maxDischarge = doc.inverter.max_discharge_w
    inverterForm.capacity = doc.inverter.capacity_kwh
    inverterForm.interval = doc.inverter.telemetry_interval_s
    inverterForm.signPv = doc.inverter.sign_overrides.pv
    inverterForm.signGrid = doc.inverter.sign_overrides.grid
    inverterForm.signBattery = doc.inverter.sign_overrides.battery
    pvForm.strings = doc.pv.strings.map((str) => ({ ...str }))
    pvForm.acLimit = doc.pv.inverter_ac_limit_w
    pvForm.confidence = doc.pv.confidence
    pvForm.refresh = doc.pv.refresh_minutes
    pvForm.calibration = doc.pv.calibration_enabled
    loadForm.dailyKwh = doc.load.daily_kwh
    loadForm.useMeasured = doc.load.use_measured_profile
    loadForm.useTemperature = doc.load.use_temperature_adjustment
    loadForm.temperatureReference = doc.load.temperature_reference_c
    loadForm.temperatureSensitivity = doc.load.temperature_sensitivity_pct_c
    loadForm.profile = [...doc.load.profile]
    planForm.maxSlots = doc.plan.max_slots_per_day
    planForm.maxSlotMin = doc.plan.max_slot_min
    planForm.horizonDays = doc.plan.horizon_days
    planForm.minDailySaving = doc.plan.min_daily_saving
    planForm.preserveSoc = doc.plan.preserve_soc
    planForm.endSoc = doc.plan.end_soc_target
    planForm.minSoc = doc.inverter.min_soc
    planForm.maxSoc = doc.inverter.max_soc
    planForm.reserveSoc = doc.inverter.reserve_soc
    planForm.chargeEfficiency = doc.inverter.charge_efficiency
    planForm.dischargeEfficiency = doc.inverter.discharge_efficiency
    planForm.chargeW = doc.plan.charge_w
    planForm.dischargeW = doc.plan.discharge_w
    planForm.allowGrid = doc.plan.allow_discharge_to_grid
    planForm.strategy = doc.plan.strategy === 'savings' || !doc.plan.strategy ? 'minimize_cost' : doc.plan.strategy
    planForm.minPriceDifference = doc.plan.min_price_difference ?? 0
    planForm.maxPurchasePrice = doc.plan.max_purchase_price ?? 0
    planForm.peakShaving = doc.plan.peak_shaving_enabled ?? false
    planForm.peakLimitW = doc.plan.peak_shaving_limit_w ?? 0
    evForm.mode = doc.ev.mode
    evForm.provider = doc.ev.provider || 'zaptec'
    evForm.chargerID = doc.ev.charger_id || ''
    evForm.installationID = doc.ev.installation_id || ''
    evForm.zaptecUsername = doc.ev.zaptec_username || ''
    evForm.zaptecPassword = ''
    evForm.minCurrent = doc.ev.min_current_a
    evForm.maxCurrent = doc.ev.max_current_a
    evForm.phases = doc.ev.phases
    evForm.voltage = doc.ev.voltage_v
    evForm.solarSharePct = Math.round(doc.ev.solar_share * 100)
    evForm.solarOnly = !doc.ev.grid_charge_allowed
    evForm.allowBatteryCharging = doc.ev.allow_ev_battery_charging ?? false
    evForm.alwaysCharge = doc.ev.always_charge
    evForm.batteryFloor = doc.ev.house_battery_floor_soc
    evForm.smartCostLimit = doc.ev.smart_cost_limit_sek_kwh ?? ''
    evForm.feedInLimit = doc.ev.smart_feed_in_priority_limit_sek_kwh ?? ''
    safetyForm.writes = doc.safety.writes_armed_ui
    safetyForm.ev = doc.safety.ev_control_armed_ui
    safetyForm.watchdog = doc.safety.watchdog_clear_after_s
    safetyForm.horizon = doc.safety.horizon_limit_days
    safetyForm.autoWrite = doc.safety.auto_schedule_write === 'when_changed' ? 'when_changed' : 'off'
    safetyForm.foreignTakeover = doc.safety.foreign_schedule_takeover === true
    await loadScheduleOwnership()
    await refreshStatus()
    try {
      evConfig.value = await api.get<EVConfig>('/ev/config')
    } catch {
      evConfig.value = null
    }
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.loadFailed')
  } finally {
    loading.value = false
  }
  await nextTick()
  decorateSettings()
}

async function saveSite(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('site', {
      name: siteForm.name,
      timezone: siteForm.timezone,
      latitude: Number(siteForm.latitude),
      longitude: Number(siteForm.longitude),
      max_import_power_w: Math.round(Number(siteForm.maxImport) * 1000),
      grid_owner: siteForm.gridOwner.trim(),
      facility_id: siteForm.facilityID.trim(),
      grid_tariff_product: siteForm.tariffProduct.trim(),
      eltariff_url: siteForm.eltariffURL.trim(),
      tariff_mode: siteForm.tariffMode,
      transfer_fee_ore_kwh: Number(siteForm.transferFeeOre) || 0,
      energy_tax_ore_kwh: Number(siteForm.energyTaxOre) || 0,
      variable_cost_ore_kwh: Number(siteForm.variableCostOre) || 0,
      fixed_markup_ore_kwh: Number(siteForm.fixedMarkupOre) || 0,
      export_benefit_ore_kwh: Number(siteForm.exportBenefitOre) || 0,
      export_extra_ore_kwh: Number(siteForm.exportExtraOre) || 0,
      show_full_price: siteForm.showFullPrice,
      ...(siteForm.license.trim() ? { license: siteForm.license.trim() } : {}),
    })
    saved.value = t('settings.siteSaved')
    await load()
    await loadMeta()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveSafety(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('safety', {
      writes_armed_ui: safetyForm.writes,
      ev_control_armed_ui: safetyForm.ev,
      watchdog_clear_after_s: Number(safetyForm.watchdog),
      horizon_limit_days: Number(safetyForm.horizon),
      auto_schedule_write: safetyForm.autoWrite,
      foreign_schedule_takeover: safetyForm.foreignTakeover,
    })
    saved.value = t('settings.safetySaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

// loadScheduleOwnership asks the plan view who currently owns the inverter
// schedule: aiptimize, a foreign controller, or unknown when the read fails.
async function loadScheduleOwnership(): Promise<void> {
  try {
    const view = await api.get<PlanView>('/plan')
    scheduleForeign.value = view.foreign_schedule === true
    scheduleUnknown.value = view.inverter_read_ok === false
  } catch {
    scheduleUnknown.value = true
  }
}

async function takeControl(): Promise<void> {
  if (!canTakeControl.value) return
  if (!window.confirm(t('plan.confirmTakeover'))) return
  takeoverBusy.value = true
  takeoverDone.value = ''
  takeoverError.value = ''
  try {
    const result = await api.post<PlanApplyResult>('/schedule/takeover', { confirm: true })
    takeoverDone.value = result.message || t('settings.takeControlDone')
    await loadScheduleOwnership()
  } catch (err) {
    if (err instanceof ApiError) {
      const details = err.details as { error?: string } | undefined
      takeoverError.value = details?.error ?? err.message
    } else {
      takeoverError.value = err instanceof Error ? err.message : t('settings.saveFailed')
    }
  } finally {
    takeoverBusy.value = false
  }
}

async function saveInverter(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  testResult.value = null
  testError.value = ''
  try {
    await patchSettings('inverter', {
      host: inverterForm.host.trim(),
      scheme: inverterForm.scheme,
      port: Number(inverterForm.port),
      serial: inverterForm.serial.trim(),
      max_charge_w: Math.round(Number(inverterForm.maxCharge)),
      max_discharge_w: Math.round(Number(inverterForm.maxDischarge)),
      capacity_kwh: Number(inverterForm.capacity),
      telemetry_interval_s: Number(inverterForm.interval),
      sign_overrides: {
        pv: Number(inverterForm.signPv),
        grid: Number(inverterForm.signGrid),
        battery: Number(inverterForm.signBattery),
      },
    })
    saved.value = t('settings.inverterSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function testInverter(): Promise<void> {
  testing.value = true
  testError.value = ''
  testResult.value = null
  try {
    testResult.value = await api.post<InverterTestResult>('/settings/test/inverter', {})
  } catch (err) {
    if (err instanceof ApiError) {
      const details = err.details as { error?: string } | undefined
      testError.value = details?.error ?? err.message
    } else {
      testError.value = t('settings.testFailed')
    }
  } finally {
    testing.value = false
  }
}

function onOff(value: boolean | undefined): string {
  return value ? t('settings.on') : t('settings.off')
}

function addString(): void {
  if (pvForm.strings.length >= 3) return
  pvForm.strings.push({
    name: `MPPT ${pvForm.strings.length + 1}`,
    kwp: 4,
    tilt: 35,
    azimuth: 180,
    performance_ratio: 0.85,
    temp_coefficient: -0.004,
    max_w: 0,
  })
}

function removeString(index: number): void {
  pvForm.strings.splice(index, 1)
}

async function saveSolar(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('pv', {
      strings: pvForm.strings.map((str) => ({
        name: str.name.trim() || 'MPPT',
        kwp: Number(str.kwp),
        tilt: Number(str.tilt),
        azimuth: Number(str.azimuth),
        performance_ratio: Number(str.performance_ratio),
        temp_coefficient: Number(str.temp_coefficient),
        max_w: Number(str.max_w),
      })),
      inverter_ac_limit_w: Number(pvForm.acLimit),
      confidence: Number(pvForm.confidence),
      refresh_minutes: Number(pvForm.refresh),
      calibration_enabled: pvForm.calibration,
    })
    saved.value = t('settings.solarSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveLoad(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    const sum = loadForm.profile.reduce((a, b) => a + b, 0)
    const profile = sum > 0 ? loadForm.profile.map((v) => v / sum) : loadForm.profile
    await patchSettings('load', {
      daily_kwh: Number(loadForm.dailyKwh),
      profile,
      use_measured_profile: loadForm.useMeasured,
      use_temperature_adjustment: loadForm.useTemperature,
      temperature_reference_c: Number(loadForm.temperatureReference),
      temperature_sensitivity_pct_c: Number(loadForm.temperatureSensitivity),
    })
    saved.value = t('settings.loadSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function savePlan(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    if (planForm.peakShaving && planForm.peakLimitW <= 0) {
      throw new Error(t('plan.peakLimitRequired'))
    }
    await patchSettings('inverter', {
      min_soc: Number(planForm.minSoc),
      max_soc: Number(planForm.maxSoc),
      reserve_soc: Number(planForm.reserveSoc),
      charge_efficiency: Number(planForm.chargeEfficiency),
      discharge_efficiency: Number(planForm.dischargeEfficiency),
    })
    await patchSettings('plan', {
      max_slots_per_day: Number(planForm.maxSlots),
      max_slot_min: Number(planForm.maxSlotMin),
      horizon_days: Number(planForm.horizonDays),
      min_daily_saving: Number(planForm.minDailySaving),
      preserve_soc: planForm.preserveSoc,
      end_soc_target: Number(planForm.endSoc),
      charge_w: Math.round(Number(planForm.chargeW)),
      discharge_w: Math.round(Number(planForm.dischargeW)),
      allow_discharge_to_grid: planForm.allowGrid,
      strategy: planForm.strategy,
      min_price_difference: Number(planForm.minPriceDifference) || 0,
      max_purchase_price: Number(planForm.maxPurchasePrice) || 0,
      peak_shaving_enabled: planForm.peakShaving,
      peak_shaving_limit_w: Number(planForm.peakLimitW) || 0,
    })
    try {
      await api.post('/plan/recompute', {})
    } catch {
      // The planner will retry on its next five-minute cycle.
    }
    saved.value = t('settings.planSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : err instanceof Error ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function saveEV(): Promise<void> {
  saving.value = true
  error.value = ''
  saved.value = ''
  try {
    await patchSettings('ev', {
      mode: evForm.mode,
      min_current_a: Number(evForm.minCurrent),
      max_current_a: Number(evForm.maxCurrent),
      phases: Number(evForm.phases),
      voltage_v: Number(evForm.voltage),
      solar_share: Number(evForm.solarSharePct) / 100,
      grid_charge_allowed: !evForm.solarOnly,
      allow_ev_battery_charging: evForm.allowBatteryCharging,
      always_charge: evForm.alwaysCharge,
      house_battery_floor_soc: Number(evForm.batteryFloor),
      smart_cost_limit_sek_kwh: evForm.smartCostLimit === '' ? null : Number(evForm.smartCostLimit),
      smart_feed_in_priority_limit_sek_kwh: evForm.feedInLimit === '' ? null : Number(evForm.feedInLimit),
    })
    saved.value = t('settings.evSaved')
    await load()
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    saving.value = false
  }
}

async function applyEVProvider(): Promise<void> {
  const hasCredentials =
    evConfig.value?.credentials.zaptec === true ||
    (evForm.zaptecUsername.trim() !== '' && evForm.zaptecPassword !== '')
  if (evForm.provider === 'zaptec' && !hasCredentials) {
    error.value = t('settings.evCredentialsMissing')
    return
  }
  if (!window.confirm(t('settings.evProviderRestartConfirm'))) return
  evProviderBusy.value = true
  error.value = ''
  saved.value = ''
  try {
    evConfig.value = await api.put<EVConfig>('/ev/config', {
      provider: evForm.provider,
      charger_id: evForm.chargerID.trim(),
      installation_id: evForm.installationID.trim(),
      zaptec_username: evForm.zaptecUsername.trim(),
      zaptec_password: evForm.zaptecPassword,
    })
    saved.value = t('settings.evProviderReady')
    if (evConfig.value?.restart_required) {
      await api.post('/system/restart', {})
    } else {
      saved.value = t('settings.evCredentialsSaved')
      await load()
    }
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : t('settings.saveFailed')
  } finally {
    evProviderBusy.value = false
  }
}

async function exportSettings(): Promise<void> {
  error.value = ''
  try {
    const data = await api.get<Record<string, unknown>>('/settings/export')
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = 'aiptimize-settings.json'
    link.click()
    URL.revokeObjectURL(link.href)
    saved.value = t('settings.exported')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settings.exportFailed')
  }
}

async function importSettings(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  saving.value = true
  error.value = ''
  try {
    const text = await file.text()
    const parsed = JSON.parse(text) as Record<string, unknown>
    await api.post('/settings/import', parsed)
    saved.value = t('settings.imported')
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settings.importFailed')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageHead :title="t('settings.title')" :subtitle="t('settings.subtitle')" />

  <nav class="settings-nav" aria-label="Settings sections">
    <router-link
      v-for="item in sections"
      :key="item"
      :to="`/settings/${item}`"
      :class="{ active: section === item }"
    >
      {{ t(`settings.sections.${item}`) }}
    </router-link>
  </nav>

  <div v-if="error" class="alert alert-danger" role="alert">{{ error }}</div>
  <div v-if="saved" class="alert alert-success" role="status">{{ saved }}</div>
  <div v-if="loading" class="empty-state">{{ t('common.loading') }}</div>

  <template v-else-if="settings">
    <section v-if="section === 'site'" class="aipt-card">
      <h2>{{ t('settings.sections.site') }}</h2>
      <form novalidate @submit.prevent="saveSite">
        <div class="row g-3">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="name">{{ t('settings.siteName') }}</label>
            <input id="name" v-model="siteForm.name" class="form-control" type="text" required />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="tz">{{ t('settings.timezone') }}</label>
            <select id="tz" v-model="siteForm.timezone" class="form-select" required>
              <option v-for="zone in timezoneOptions" :key="zone" :value="zone">{{ zone }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lat">{{ t('settings.latitude') }}</label>
            <input id="lat" v-model.number="siteForm.latitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="lon">{{ t('settings.longitude') }}</label>
            <input id="lon" v-model.number="siteForm.longitude" class="form-control" type="number" step="0.0001" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="max-import">{{ t('settings.maxImport') }}</label>
            <input
              id="max-import"
              v-model.number="siteForm.maxImport"
              class="form-control"
              type="number"
              min="1"
              step="1"
            />
          </div>
        </div>

        <h3 class="h6 mt-4">{{ t('settings.gridTariff') }}</h3>
        <div class="row g-3">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="tariff-mode">{{ t('settings.tariffMode') }}</label>
            <select id="tariff-mode" v-model="siteForm.tariffMode" class="form-select">
              <option value="manual">{{ t('settings.tariffManual') }}</option>
              <option value="api">{{ t('settings.tariffApi') }}</option>
            </select>
          </div>
        </div>

        <template v-if="siteForm.tariffMode === 'manual'">
          <h4 class="h6 mt-4">{{ t('settings.purchaseSettings') }}</h4>
          <div class="row g-3">
            <div class="col-6 col-sm-3">
              <label class="form-label" for="transfer-fee">{{ t('settings.transferFeeOre') }}</label>
              <input id="transfer-fee" v-model.number="siteForm.transferFeeOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="energy-tax">{{ t('settings.energyTaxOre') }}</label>
              <input id="energy-tax" v-model.number="siteForm.energyTaxOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="variable-cost">{{ t('settings.variableCostOre') }}</label>
              <input id="variable-cost" v-model.number="siteForm.variableCostOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="fixed-markup">{{ t('settings.fixedMarkupOre') }}</label>
              <input id="fixed-markup" v-model.number="siteForm.fixedMarkupOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
          </div>

          <h4 class="h6 mt-4">{{ t('settings.exportSettings') }}</h4>
          <div class="row g-3">
            <div class="col-6 col-sm-3">
              <label class="form-label" for="export-benefit">{{ t('settings.exportBenefitOre') }}</label>
              <input id="export-benefit" v-model.number="siteForm.exportBenefitOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="export-extra">{{ t('settings.exportExtraOre') }}</label>
              <input id="export-extra" v-model.number="siteForm.exportExtraOre" class="form-control" type="number" min="0" step="0.01" />
            </div>
          </div>
        </template>

        <div v-if="siteForm.tariffMode === 'api'" class="row g-3 mt-1">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="grid-owner">{{ t('settings.gridOwner') }}</label>
            <input id="grid-owner" v-model="siteForm.gridOwner" class="form-control" type="text" autocomplete="organization" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="facility-id">{{ t('settings.facilityID') }}</label>
            <input id="facility-id" v-model="siteForm.facilityID" class="form-control mono" type="text" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="tariff-product">{{ t('settings.tariffProduct') }}</label>
            <input id="tariff-product" v-model="siteForm.tariffProduct" class="form-control" type="text" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="eltariff-url">{{ t('settings.eltariffURL') }}</label>
            <input
              id="eltariff-url"
              v-model="siteForm.eltariffURL"
              class="form-control mono"
              type="text"
              placeholder="https://…/tariffs"
            />
          </div>
        </div>

        <div class="form-check form-switch mt-4">
          <input id="show-full-price" v-model="siteForm.showFullPrice" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="show-full-price">{{ t('settings.showFullPrice') }}</label>
        </div>

        <div class="row g-3 mt-2">
          <div class="col-12">
            <label class="form-label" for="site-license">{{ t('settings.license') }}</label>
            <input
              id="site-license"
              v-model="siteForm.license"
              class="form-control mono"
              type="password"
              autocomplete="off"
              :placeholder="licenseSet ? t('settings.licensePlaceholderSet') : t('settings.licensePlaceholder')"
            />
            <p class="small text-muted mt-1 mb-0">
              <i class="bi" :class="licenseSet ? 'bi-check-circle-fill' : 'bi-dash-circle'" aria-hidden="true"></i>
              <span class="ms-1">{{ licenseSet ? t('settings.licenseConfigured') : t('settings.licenseMissing') }}</span>
            </p>
            <p class="small text-muted mt-1 mb-0">{{ t('settings.licenseHint') }}</p>
          </div>
        </div>

        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSite') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'inverter'" class="aipt-card">
      <h2>{{ t('settings.sections.inverter') }}</h2>
      <p class="text-muted small">{{ t('settings.inverterIntro') }}</p>
      <form novalidate @submit.prevent="saveInverter">
        <div class="row g-3">
          <div class="col-12 col-sm-5">
            <label class="form-label" for="inv-host">{{ t('settings.host') }}</label>
            <input id="inv-host" v-model="inverterForm.host" class="form-control mono" type="text" placeholder="192.0.2.10" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-scheme">{{ t('settings.scheme') }}</label>
            <select id="inv-scheme" v-model="inverterForm.scheme" class="form-select">
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </div>
          <div class="col-6 col-sm-4">
            <label class="form-label" for="inv-port">{{ t('settings.port') }}</label>
            <input id="inv-port" v-model.number="inverterForm.port" class="form-control" type="number" min="1" max="65535" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="inv-serial">{{ t('settings.serial') }}</label>
            <input id="inv-serial" v-model="inverterForm.serial" class="form-control mono" type="text" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-capacity">{{ t('settings.batteryCapacity') }}</label>
            <input id="inv-capacity" v-model.number="inverterForm.capacity" class="form-control" type="number" step="0.01" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-interval">{{ t('settings.telemetryInterval') }}</label>
            <input id="inv-interval" v-model.number="inverterForm.interval" class="form-control" type="number" min="5" step="5" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-max-charge">{{ t('settings.maxCharge') }}</label>
            <input id="inv-max-charge" v-model.number="inverterForm.maxCharge" class="form-control" type="number" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="inv-max-discharge">{{ t('settings.maxDischarge') }}</label>
            <input id="inv-max-discharge" v-model.number="inverterForm.maxDischarge" class="form-control" type="number" step="100" />
          </div>
        </div>

        <h2 class="mt-4">{{ t('settings.signOverrides') }}</h2>
        <p class="text-muted small mb-2">{{ t('settings.signHelp') }}</p>
        <div class="row g-3">
          <div class="col-4">
            <label class="form-label" for="sign-pv">{{ t('settings.signPv') }}</label>
            <select id="sign-pv" v-model.number="inverterForm.signPv" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
          <div class="col-4">
            <label class="form-label" for="sign-grid">{{ t('settings.signGrid') }}</label>
            <select id="sign-grid" v-model.number="inverterForm.signGrid" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
          <div class="col-4">
            <label class="form-label" for="sign-battery">{{ t('settings.signBattery') }}</label>
            <select id="sign-battery" v-model.number="inverterForm.signBattery" class="form-select">
              <option :value="1">1</option>
              <option :value="-1">−1</option>
            </select>
          </div>
        </div>

        <div class="d-flex flex-wrap gap-2 mt-4">
          <button class="btn btn-primary" type="submit" :disabled="saving">
            <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ saving ? t('common.saving') : t('settings.saveInverter') }}
          </button>
          <button class="btn btn-outline-secondary" type="button" :disabled="testing" @click="testInverter">
            <span v-if="testing" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ testing ? t('settings.testing') : t('settings.testConnection') }}
          </button>
        </div>
        <p class="text-muted small mt-2 mb-0">{{ t('settings.testHint') }}</p>
      </form>

      <div v-if="testError" class="alert alert-danger mt-3 small" role="alert">{{ testError }}</div>
      <div v-if="testResult" class="alert alert-success mt-3 small" role="status">
        <div class="fw-semibold mb-2">
          <i class="bi bi-check-circle me-1" aria-hidden="true"></i>
          {{ t('settings.testOk') }} — {{ testResult.identity.model }}
        </div>
        <dl class="mb-0">
          <div class="key-value">
            <dt>{{ t('settings.serial') }}</dt>
            <dd class="mono">{{ testResult.identity.serial }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.firmware') }}</dt>
            <dd class="mono">{{ testResult.identity.firmware }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.ratedPower') }}</dt>
            <dd>{{ testResult.identity.rated_power_w }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.soc') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.soc) }} %</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.solar') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.pv_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.grid') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.grid_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.battery') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.battery_w) }} W</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('dashboard.house') }}</dt>
            <dd>{{ Math.round(testResult.telemetry.load_w) }} W</dd>
          </div>
        </dl>
      </div>
    </section>

    <section v-else-if="section === 'solar'" class="aipt-card">
      <h2>{{ t('settings.sections.solar') }}</h2>
      <p class="text-muted small">{{ t('settings.solarIntro') }}</p>
      <form novalidate @submit.prevent="saveSolar">
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-ac">{{ t('settings.acLimit') }}</label>
            <input id="pv-ac" v-model.number="pvForm.acLimit" class="form-control" type="number" step="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-conf">{{ t('settings.confidence') }}</label>
            <input id="pv-conf" v-model.number="pvForm.confidence" class="form-control" type="number" step="0.05" min="0" max="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="pv-refresh">{{ t('settings.refreshMinutes') }}</label>
            <input id="pv-refresh" v-model.number="pvForm.refresh" class="form-control" type="number" min="5" step="5" />
          </div>
          <div class="col-12 col-sm-3 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="pv-cal" v-model="pvForm.calibration" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="pv-cal">{{ t('settings.calibrationEnabled') }}</label>
            </div>
          </div>
        </div>

        <div v-for="(str, index) in pvForm.strings" :key="index" class="string-card">
          <div class="d-flex align-items-center gap-2 mb-3">
            <input
              v-model="str.name"
              class="form-control"
              type="text"
              :aria-label="t('settings.stringName')"
              :placeholder="t('settings.stringName')"
            />
            <button class="btn btn-sm btn-outline-secondary" type="button" @click="removeString(index)">
              <i class="bi bi-trash" aria-hidden="true"></i>
              <span class="d-none d-sm-inline ms-1">{{ t('settings.removeString') }}</span>
            </button>
          </div>
          <div class="row g-3">
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`kwp-${index}`">{{ t('settings.kwp') }}</label>
              <input :id="`kwp-${index}`" v-model.number="str.kwp" class="form-control" type="number" step="0.1" min="0" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`tilt-${index}`">{{ t('settings.tilt') }}</label>
              <input :id="`tilt-${index}`" v-model.number="str.tilt" class="form-control" type="number" min="0" max="90" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`azimuth-${index}`">{{ t('settings.azimuth') }}</label>
              <input :id="`azimuth-${index}`" v-model.number="str.azimuth" class="form-control" type="number" min="0" max="359" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`pr-${index}`">{{ t('settings.performanceRatio') }}</label>
              <input :id="`pr-${index}`" v-model.number="str.performance_ratio" class="form-control" type="number" step="0.01" min="0" max="1.5" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`temp-${index}`">{{ t('settings.tempCoefficient') }}</label>
              <input :id="`temp-${index}`" v-model.number="str.temp_coefficient" class="form-control" type="number" step="0.001" />
            </div>
            <div class="col-6 col-sm-2">
              <label class="form-label" :for="`max-${index}`">{{ t('settings.maxW') }}</label>
              <input :id="`max-${index}`" v-model.number="str.max_w" class="form-control" type="number" step="100" min="0" />
            </div>
          </div>
        </div>

        <button
          v-if="pvForm.strings.length < 3"
          class="btn btn-outline-secondary mt-3"
          type="button"
          @click="addString"
        >
          <i class="bi bi-plus-lg" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.addString') }}</span>
        </button>

        <div class="mt-4">
          <button class="btn btn-primary" type="submit" :disabled="saving">
            <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ saving ? t('common.saving') : t('settings.saveSolar') }}
          </button>
        </div>
      </form>
    </section>

    <section v-else-if="section === 'load'" class="aipt-card">
      <h2>{{ t('settings.sections.load') }}</h2>
      <p class="text-muted small">{{ t('settings.loadIntro') }}</p>
      <form novalidate @submit.prevent="saveLoad">
        <div class="row g-3">
          <div class="col-6 col-sm-4">
            <label class="form-label" for="load-kwh">{{ t('settings.dailyKwh') }}</label>
            <input id="load-kwh" v-model.number="loadForm.dailyKwh" class="form-control" type="number" min="0" step="0.5" />
          </div>
          <div class="col-12 col-sm-8 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="load-measured" v-model="loadForm.useMeasured" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="load-measured">{{ t('settings.useMeasured') }}</label>
            </div>
          </div>
        </div>

        <div class="row g-3 mt-1 align-items-end">
          <div class="col-12 col-sm-6">
            <div class="form-check form-switch mb-2">
              <input
                id="load-temperature"
                v-model="loadForm.useTemperature"
                class="form-check-input"
                type="checkbox"
                role="switch"
              />
              <label class="form-check-label" for="load-temperature">{{ t('settings.useTemperature') }}</label>
            </div>
          </div>
          <template v-if="loadForm.useTemperature">
            <div class="col-6 col-sm-3">
              <label class="form-label" for="temperature-reference">{{ t('settings.temperatureReference') }}</label>
              <input
                id="temperature-reference"
                v-model.number="loadForm.temperatureReference"
                class="form-control"
                type="number"
                min="-40"
                max="50"
                step="0.5"
              />
            </div>
            <div class="col-6 col-sm-3">
              <label class="form-label" for="temperature-sensitivity">{{ t('settings.temperatureSensitivity') }}</label>
              <input
                id="temperature-sensitivity"
                v-model.number="loadForm.temperatureSensitivity"
                class="form-control"
                type="number"
                min="0"
                max="20"
                step="0.1"
              />
            </div>
          </template>
        </div>
        <p v-if="loadForm.useTemperature" class="text-muted small mt-2 mb-0">{{ t('settings.temperatureHelp') }}</p>

        <details class="mt-4">
          <summary class="fw-semibold">{{ t('settings.hourlyProfile') }}</summary>
          <p class="text-muted small mt-2">{{ t('settings.profileHelp') }}</p>
          <div class="row g-2">
            <div v-for="(value, hour) in loadForm.profile" :key="hour" class="col-4 col-sm-2">
              <label class="form-label" :for="`profile-${hour}`">{{ hour }}:00</label>
              <input
                :id="`profile-${hour}`"
                v-model.number="loadForm.profile[hour]"
                class="form-control"
                type="number"
                step="0.001"
                min="0"
              />
            </div>
          </div>
        </details>

        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveLoad') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'plan'" class="aipt-card">
      <div class="d-flex align-items-center gap-2">
        <h2 class="mb-0 settings-plan-title">{{ t('settings.sections.plan') }}</h2>
        <button
          class="btn btn-link plan-info-button p-0"
          type="button"
          :aria-label="t('settings.planHelpTitle')"
          :title="t('settings.planHelpTitle')"
          @click="planHelpOpen = true"
        >
          <i class="bi bi-info-circle" aria-hidden="true"></i>
        </button>
      </div>
      <p class="text-muted small">{{ t('settings.planIntro') }}</p>
      <form novalidate @submit.prevent="savePlan">
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-slots">{{ t('settings.maxSlots') }}</label>
            <input id="plan-slots" v-model.number="planForm.maxSlots" class="form-control" type="number" min="1" max="6" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-min">{{ t('settings.maxSlotMin') }}</label>
            <input id="plan-min" v-model.number="planForm.maxSlotMin" class="form-control" type="number" min="30" max="240" step="30" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-days">{{ t('settings.horizonDays') }}</label>
            <input id="plan-days" v-model.number="planForm.horizonDays" class="form-control" type="number" min="1" max="2" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-saving">{{ t('settings.minDailySaving') }}</label>
            <input id="plan-saving" v-model.number="planForm.minDailySaving" class="form-control" type="number" min="0" step="0.1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-charge">{{ t('settings.planChargeW') }}</label>
            <input id="plan-charge" v-model.number="planForm.chargeW" class="form-control" type="number" min="1" step="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-discharge">{{ t('settings.planDischargeW') }}</label>
            <input id="plan-discharge" v-model.number="planForm.dischargeW" class="form-control" type="number" min="1" step="1" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="plan-min-soc">{{ t('settings.minSoc') }}</label>
            <input id="plan-min-soc" v-model.number="planForm.minSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="plan-max-soc">{{ t('settings.maxSoc') }}</label>
            <input id="plan-max-soc" v-model.number="planForm.maxSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-4 col-sm-3">
            <label class="form-label" for="plan-reserve-soc">{{ t('settings.reserveSoc') }}</label>
            <input id="plan-reserve-soc" v-model.number="planForm.reserveSoc" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-12">
            <div class="form-check form-switch">
              <input id="plan-preserve" v-model="planForm.preserveSoc" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="plan-preserve">{{ t('settings.preserveSoc') }}</label>
            </div>
          </div>
          <div v-if="planForm.preserveSoc" class="col-6 col-sm-3">
            <label class="form-label" for="plan-end">{{ t('settings.endSoc') }}</label>
            <input id="plan-end" v-model.number="planForm.endSoc" class="form-control" type="number" min="0" max="100" step="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-charge-eff">{{ t('settings.chargeEfficiency') }}</label>
            <input id="plan-charge-eff" v-model.number="planForm.chargeEfficiency" class="form-control" type="number" step="0.01" min="0.5" max="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="plan-discharge-eff">{{ t('settings.dischargeEfficiency') }}</label>
            <input id="plan-discharge-eff" v-model.number="planForm.dischargeEfficiency" class="form-control" type="number" step="0.01" min="0.5" max="1" />
          </div>
        </div>
        <div class="row g-3 mt-2">
          <div class="col-12">
            <h3 class="h6 mb-0">{{ t('plan.strategy') }}</h3>
          </div>
          <div class="col-12 col-sm-4">
            <label class="form-label" for="settings-plan-strategy">{{ t('settings.selectStrategyType') }}</label>
            <select id="settings-plan-strategy" v-model="planForm.strategy" class="form-select">
              <option value="minimize_cost">{{ t('plan.strategyMinimizeCost') }}</option>
              <option value="maximize_profit">{{ t('plan.strategyMaximizeProfit') }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-4">
            <label class="form-label" for="settings-min-diff">
              {{ t('plan.minPriceDifference') }} ({{ settings?.price.currency || 'SEK' }}/kWh)
            </label>
            <input
              id="settings-min-diff"
              v-model.number="planForm.minPriceDifference"
              class="form-control"
              type="number"
              min="0"
              step="0.01"
            />
          </div>
          <div class="col-6 col-sm-4">
            <label class="form-label" for="settings-max-purchase">
              {{ t('plan.maxPurchasePrice') }} ({{ settings?.price.currency || 'SEK' }}/kWh)
            </label>
            <input
              id="settings-max-purchase"
              v-model.number="planForm.maxPurchasePrice"
              class="form-control"
              type="number"
              min="0"
              step="0.01"
            />
          </div>
          <div class="col-12">
            <div class="form-check form-switch">
              <input id="plan-grid" v-model="planForm.allowGrid" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="plan-grid">{{ t('settings.allowGridDischarge') }}</label>
            </div>
          </div>
          <div class="col-12">
            <div class="form-check form-switch">
              <input
                id="settings-peak-shaving"
                v-model="planForm.peakShaving"
                class="form-check-input"
                type="checkbox"
                role="switch"
              />
              <label class="form-check-label" for="settings-peak-shaving">{{ t('plan.peakShaving') }}</label>
            </div>
          </div>
          <div v-if="planForm.peakShaving" class="col-12 col-sm-4">
            <label class="form-label" for="settings-peak-limit">{{ t('plan.maxHourlyUsage') }}</label>
            <input
              id="settings-peak-limit"
              v-model.number="planForm.peakLimitW"
              class="form-control"
              type="number"
              min="100"
              step="50"
            />
          </div>
        </div>

        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.savePlan') }}
        </button>
      </form>

      <Sheet
        v-model:open="planHelpOpen"
        :title="t('settings.planHelpTitle')"
        :subtitle="t('settings.planHelpSubtitle')"
        size="md"
      >
        <ol class="plan-help-list">
          <li v-for="index in 8" :key="index">{{ t(`settings.planHelp${index}`) }}</li>
        </ol>
        <template #footer>
          <button class="btn btn-primary" type="button" @click="planHelpOpen = false">
            {{ t('common.close') }}
          </button>
        </template>
      </Sheet>
    </section>

    <section v-else-if="section === 'ev'" class="aipt-card">
      <h2>{{ t('settings.sections.ev') }}</h2>
      <p class="text-muted small">{{ t('settings.evIntro') }}</p>
      <form novalidate @submit.prevent="saveEV">
        <h3 class="h6">{{ t('settings.evConnectionTitle') }}</h3>
        <p class="text-muted small">{{ t('settings.evConnectionIntro') }}</p>
        <div class="row g-3">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="ev-provider">{{ t('settings.evProvider') }}</label>
            <select id="ev-provider" v-model="evForm.provider" class="form-select">
              <option value="zaptec">Zaptec</option>
              <option value="simulator">{{ t('settings.evSimulator') }}</option>
            </select>
          </div>
          <div v-if="evForm.provider !== 'simulator'" class="col-12 col-sm-6">
            <label class="form-label" for="ev-charger-id">{{ t('settings.evChargerID') }}</label>
            <input id="ev-charger-id" v-model="evForm.chargerID" class="form-control mono" type="text" />
          </div>
          <div v-if="evForm.provider === 'zaptec'" class="col-12 col-sm-6">
            <label class="form-label" for="ev-installation-id">{{ t('settings.evInstallationID') }}</label>
            <input id="ev-installation-id" v-model="evForm.installationID" class="form-control mono" type="text" />
          </div>
          <div v-if="evForm.provider === 'zaptec'" class="col-12 col-sm-6">
            <label class="form-label" for="ev-zaptec-username">{{ t('settings.evZaptecUsername') }}</label>
            <input id="ev-zaptec-username" v-model="evForm.zaptecUsername" class="form-control" type="text" autocomplete="username" />
          </div>
          <div v-if="evForm.provider === 'zaptec'" class="col-12 col-sm-6">
            <label class="form-label" for="ev-zaptec-password">{{ t('settings.evZaptecPassword') }}</label>
            <input
              id="ev-zaptec-password"
              v-model="evForm.zaptecPassword"
              class="form-control"
              type="password"
              autocomplete="current-password"
              :placeholder="evConfig?.zaptec_password_set ? t('settings.evPasswordKeep') : ''"
            />
            <div class="form-text">{{ t('settings.evPasswordKeepHint') }}</div>
          </div>
        </div>
        <div class="small text-muted mt-2">
          <div>{{ t('settings.evActiveProvider', { provider: evConfig?.active_provider || '—' }) }}</div>
          <div>{{ t('settings.evZaptecCredentials') }}: {{ evConfig?.credentials.zaptec ? t('settings.evConfigured') : t('settings.evNotConfigured') }}</div>
        </div>
        <div
          v-if="evForm.provider === 'zaptec' && !evCredentialsReady"
          class="alert alert-warning py-2 small mt-3"
          role="alert"
        >
          {{ t('settings.evCredentialsMissing') }}
        </div>
        <div
          v-else-if="evConfig && !evConfig.active_provider"
          class="alert alert-warning py-2 small mt-3"
          role="status"
        >
          {{ t('settings.evNoActiveProvider') }}
        </div>
        <div v-if="evConfig?.restart_required" class="alert alert-warning py-2 small mt-3" role="status">
          {{ t('settings.evRestartRequired') }}
        </div>
        <button
          class="btn btn-outline-primary mt-3"
          type="button"
          :disabled="evProviderBusy || Boolean(evForm.provider === 'zaptec' && !evCredentialsReady)"
          @click="applyEVProvider"
        >
          <span v-if="evProviderBusy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ t('settings.evApplyProvider') }}
        </button>

        <h3 class="h6 mt-4">{{ t('settings.evBehaviorTitle') }}</h3>
        <div class="row g-3">
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-mode">{{ t('settings.evMode') }}</label>
            <select id="ev-mode" v-model="evForm.mode" class="form-select">
              <option value="off">{{ t('car.modes.off') }}</option>
              <option value="now">{{ t('car.modes.now') }}</option>
              <option value="smart">{{ t('car.modes.smart') }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-min">{{ t('settings.evMinCurrent') }}</label>
            <input id="ev-min" v-model.number="evForm.minCurrent" class="form-control" type="number" min="6" max="32" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-max">{{ t('settings.evMaxCurrent') }}</label>
            <input id="ev-max" v-model.number="evForm.maxCurrent" class="form-control" type="number" min="6" max="32" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-phases">{{ t('settings.evPhases') }}</label>
            <input id="ev-phases" v-model.number="evForm.phases" class="form-control" type="number" min="1" max="3" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-voltage">{{ t('settings.evVoltage') }}</label>
            <input id="ev-voltage" v-model.number="evForm.voltage" class="form-control" type="number" min="100" max="300" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-solar">{{ t('settings.evSolarShare') }}</label>
            <input id="ev-solar" v-model.number="evForm.solarSharePct" class="form-control" type="number" min="0" max="100" step="1" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-floor">{{ t('settings.evBatteryFloor') }}</label>
            <input id="ev-floor" v-model.number="evForm.batteryFloor" class="form-control" type="number" min="0" max="100" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-always">{{ t('settings.evAlwaysCharge') }}</label>
            <select id="ev-always" v-model="evForm.alwaysCharge" class="form-select">
              <option value="off">off</option>
              <option value="on">on</option>
              <option value="once">once</option>
            </select>
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-cost">{{ t('settings.evSmartCost') }}</label>
            <input id="ev-cost" v-model.number="evForm.smartCostLimit" class="form-control" type="number" step="0.01" placeholder="—" />
          </div>
          <div class="col-6 col-sm-3">
            <label class="form-label" for="ev-feed">{{ t('settings.evFeedIn') }}</label>
            <input id="ev-feed" v-model.number="evForm.feedInLimit" class="form-control" type="number" step="0.01" placeholder="—" />
          </div>
          <div class="col-12 col-sm-6 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="ev-grid" v-model="evForm.solarOnly" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="ev-grid">{{ t('settings.evSolarOnly') }}</label>
            </div>
          </div>
          <div class="col-12 col-sm-6 d-flex align-items-end">
            <div class="form-check form-switch mb-2">
              <input id="ev-battery" v-model="evForm.allowBatteryCharging" class="form-check-input" type="checkbox" role="switch" />
              <label class="form-check-label" for="ev-battery">{{ t('settings.evAllowBatteryCharging') }}</label>
            </div>
          </div>
        </div>
        <p class="small text-muted mt-3">{{ t('settings.evGridWarning') }}</p>
        <p class="small text-muted mb-0">{{ t('settings.evBatteryChargingHint') }}</p>
        <button class="btn btn-primary mt-2" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveEV') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'safety'" class="aipt-card">
      <h2>{{ t('settings.sections.safety') }}</h2>
      <p class="text-muted small">{{ t('settings.safetyIntro') }}</p>
      <form novalidate @submit.prevent="saveSafety">
        <div class="form-check form-switch py-2">
          <input id="writes" v-model="safetyForm.writes" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="writes">{{ t('settings.writesArm') }}</label>
        </div>
        <div class="form-check form-switch py-2">
          <input id="ev" v-model="safetyForm.ev" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="ev">{{ t('settings.evArm') }}</label>
        </div>
        <div class="row g-3 mt-2">
          <div class="col-12 col-sm-6">
            <label class="form-label" for="watchdog">{{ t('settings.watchdog') }}</label>
            <input id="watchdog" v-model.number="safetyForm.watchdog" class="form-control" type="number" min="60" step="60" />
          </div>
          <div class="col-12 col-sm-6">
            <label class="form-label" for="horizon">{{ t('settings.horizon') }}</label>
            <input id="horizon" v-model.number="safetyForm.horizon" class="form-control" type="number" min="1" max="2" />
          </div>
          <div class="col-12">
            <label class="form-label" for="auto-write">{{ t('settings.autoScheduleWrite') }}</label>
            <select id="auto-write" v-model="safetyForm.autoWrite" class="form-select">
              <option value="off">{{ t('settings.autoScheduleWriteOff') }}</option>
              <option value="when_changed">{{ t('settings.autoScheduleWriteOnChange') }}</option>
            </select>
            <p class="small text-muted mt-2 mb-0">{{ t('settings.autoScheduleWriteHint') }}</p>
          </div>
        </div>
        <hr class="my-4" />
        <h3 class="h6">{{ t('settings.inverterSchedule') }}</h3>
        <p class="small text-muted mb-2">{{ t('settings.inverterScheduleHint') }}</p>
        <p class="small mb-2">
          <i class="bi" :class="scheduleForeign ? 'bi-exclamation-triangle' : 'bi-shield-check'" aria-hidden="true"></i>
          <span class="ms-1">{{ scheduleStateLabel }}</span>
        </p>
        <div class="form-check form-switch py-2">
          <input id="foreign-takeover" v-model="safetyForm.foreignTakeover" class="form-check-input" type="checkbox" role="switch" />
          <label class="form-check-label" for="foreign-takeover">{{ t('settings.foreignTakeover') }}</label>
        </div>
        <p class="small text-muted mb-2">{{ t('settings.foreignTakeoverHint') }}</p>
        <button type="button" class="btn btn-outline-secondary btn-sm" :disabled="takeoverBusy || !canTakeControl" @click="takeControl">
          <span v-if="takeoverBusy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ t('settings.takeControl') }}
        </button>
        <p v-if="!writesArmed" class="small text-muted mt-2 mb-0">{{ t('settings.takeControlNeedsArm') }}</p>
        <p v-else-if="takeoverDone" class="small text-success mt-2 mb-0">{{ takeoverDone }}</p>
        <p v-if="takeoverError" class="small text-danger mt-2 mb-0">{{ takeoverError }}</p>
        <div class="mt-3 small text-muted">
          <div class="gate-list-head">
            <span class="gate-list-title">{{ t('settings.environmentGates') }}</span>
            <button
              type="button"
              class="setting-info-btn gate-info-btn"
              :aria-label="t('settings.preconditionsTitle')"
              @click="openGateInfo"
            >
              <i class="bi bi-info-circle" aria-hidden="true"></i>
            </button>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.writes') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.envGate') }} — {{ t('dashboard.evControl') }}</dt>
            <dd>{{ onOff(session.meta?.gates.ev_control_env) }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.evGateMode') }}</dt>
            <dd>{{ evGateModeLabel }}</dd>
          </div>
          <div class="key-value">
            <dt>{{ t('settings.effective') }}</dt>
            <dd>{{ onOff(session.meta?.gates.writes_armed) }}</dd>
          </div>
        </div>
        <button class="btn btn-primary mt-4" type="submit" :disabled="saving">
          <span v-if="saving" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
          {{ saving ? t('common.saving') : t('settings.saveSafety') }}
        </button>
      </form>
    </section>

    <section v-else-if="section === 'users'" class="aipt-card">
      <h2>{{ t('settings.sections.users') }}</h2>
      <p class="text-muted small">{{ t('settings.usersIntro') }}</p>

      <div v-if="userError" class="alert alert-danger py-2 small" role="alert">{{ userError }}</div>
      <div v-if="userMessage" class="alert alert-success py-2 small" role="status">{{ userMessage }}</div>

      <div class="table-responsive">
        <table class="table table-sm align-middle mb-3">
          <thead>
            <tr>
              <th scope="col">{{ t('settings.userName') }}</th>
              <th scope="col">{{ t('settings.userRole') }}</th>
              <th scope="col"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in users" :key="item.id">
              <td>{{ item.username }}</td>
              <td>
                <span class="badge-soft" :class="item.role === 'admin' ? 'badge-on' : ''">
                  {{ t(`settings.roles.${item.role}`) }}
                </span>
              </td>
              <td class="text-end">
                <button
                  class="btn btn-sm btn-outline-secondary"
                  type="button"
                  :disabled="savingUser || item.id === session.user?.id"
                  @click="removeUser(item)"
                >
                  {{ t('car.delete') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <form novalidate @submit.prevent="createUser">
        <div class="row g-2 align-items-end">
          <div class="col-12 col-sm-4">
            <label class="form-label" for="user-name">{{ t('settings.userName') }}</label>
            <input id="user-name" v-model="userForm.username" class="form-control" autocomplete="off" />
          </div>
          <div class="col-12 col-sm-4">
            <label class="form-label" for="user-password">{{ t('settings.userPassword') }}</label>
            <input
              id="user-password"
              v-model="userForm.password"
              class="form-control"
              type="password"
              autocomplete="new-password"
            />
          </div>
          <div class="col-6 col-sm-2">
            <label class="form-label" for="user-role">{{ t('settings.userRole') }}</label>
            <select id="user-role" v-model="userForm.role" class="form-select">
              <option value="user">{{ t('settings.roles.user') }}</option>
              <option value="admin">{{ t('settings.roles.admin') }}</option>
            </select>
          </div>
          <div class="col-6 col-sm-2">
            <button class="btn btn-primary w-100" type="submit" :disabled="savingUser">
              {{ t('settings.addUser') }}
            </button>
          </div>
        </div>
      </form>
    </section>

    <section v-else class="aipt-card">
      <h2>{{ t('settings.sections.system') }}</h2>
      <p class="text-muted small">{{ t('settings.systemIntro') }}</p>
      <dl class="mb-0">
        <div class="key-value">
          <dt>{{ t('common.version') }}</dt>
          <dd class="mono">{{ session.meta?.version ?? '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.buildCommit') }}</dt>
          <dd class="mono">{{ session.meta?.commit || '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.buildDate') }}</dt>
          <dd class="mono">{{ session.meta?.build_date || '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.instanceID') }}</dt>
          <dd class="mono">{{ session.meta?.instance_id || '—' }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.inverter') }}</dt>
          <dd>{{ settings.inverter.type }}</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.host') }}</dt>
          <dd class="mono">
            {{ settings.inverter.host || t('settings.notConfigured') }}:{{ settings.inverter.port }}
          </dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.batteryCapacity') }}</dt>
          <dd>{{ settings.inverter.capacity_kwh }} kWh</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxCharge') }}</dt>
          <dd>{{ settings.inverter.max_charge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.maxDischarge') }}</dt>
          <dd>{{ settings.inverter.max_discharge_w }} W</dd>
        </div>
        <div class="key-value">
          <dt>{{ t('settings.area') }}</dt>
          <dd>{{ settings.price.area }} · {{ settings.price.currency }}</dd>
        </div>
      </dl>

      <div class="update-card mt-4">
        <div class="update-head">
          <div>
            <div class="update-title">{{ t('settings.updates') }}</div>
            <p class="update-hint mb-0">{{ t('settings.updatesHint') }}</p>
          </div>
          <button class="btn btn-outline-secondary btn-sm" type="button" :disabled="updateBusy" @click="checkUpdate">
            <span v-if="updateBusy" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
            {{ updateBusy ? t('settings.checkingUpdates') : t('settings.checkForUpdates') }}
          </button>
        </div>

        <p v-if="updateError" class="small text-danger mt-2 mb-0">{{ updateError }}</p>

        <div v-else-if="update" class="mt-3 small">
          <template v-if="update.update_available">
            <p class="update-version mb-1">
              <i class="bi bi-arrow-up-circle-fill text-warning" aria-hidden="true"></i>
              <span class="ms-1">{{ t('settings.updateAvailable', { version: update.latest }) }}</span>
            </p>
            <p v-if="update.notes" class="update-notes mb-1">{{ update.notes }}</p>
            <p class="text-muted mb-2">
              {{ t('settings.updateBuilt', { date: formatBuildDate(update.created), commit: update.revision || '—' }) }}
            </p>
            <button
              v-if="!updateConfirming"
              class="btn btn-primary btn-sm"
              type="button"
              @click="updateConfirming = true"
            >
              {{ t('settings.updateNow') }}
            </button>
            <div v-else class="update-confirm">
              <p class="mb-2">{{ t('settings.updateConfirm') }}</p>
              <button class="btn btn-primary btn-sm" type="button" :disabled="updateApplying" @click="applyUpdate">
                <span v-if="updateApplying" class="spinner-border spinner-border-sm me-2" aria-hidden="true"></span>
                {{ updateApplying ? t('settings.updating') : t('settings.updateConfirmYes') }}
              </button>
              <button
                class="btn btn-outline-secondary btn-sm ms-2"
                type="button"
                :disabled="updateApplying"
                @click="updateConfirming = false"
              >
                {{ t('settings.updateCancel') }}
              </button>
            </div>
            <p v-if="updateApplying" class="text-muted mt-2 mb-0">{{ t('settings.updateRestarting') }}</p>
            <p v-if="updateApplyError" class="small text-danger mt-2 mb-0">{{ updateApplyError }}</p>
          </template>
          <p v-else class="mb-0 text-success">
            <i class="bi bi-check-circle-fill" aria-hidden="true"></i>
            <span class="ms-1">{{ t('settings.updateCurrent', { version: update.latest }) }}</span>
          </p>
        </div>
      </div>

      <div class="d-flex flex-wrap gap-2 mt-4">
        <button class="btn btn-outline-secondary" type="button" @click="exportSettings">
          <i class="bi bi-download" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.export') }}</span>
        </button>
        <label class="btn btn-outline-secondary mb-0">
          <i class="bi bi-upload" aria-hidden="true"></i>
          <span class="ms-1">{{ t('settings.import') }}</span>
          <input class="d-none" type="file" accept="application/json" @change="importSettings" />
        </label>
      </div>
    </section>
  </template>

  <Sheet
    v-model:open="settingInfoOpen"
    :title="settingInfoTitle"
    :subtitle="t('settings.settingHelp')"
    size="sm"
  >
    <p class="setting-info-text mb-0">{{ settingInfoText }}</p>
    <template #footer>
      <button class="btn btn-primary" type="button" @click="settingInfoOpen = false">
        {{ t('common.close') }}
      </button>
    </template>
  </Sheet>

  <Sheet
    v-model:open="gateInfoOpen"
    :title="t('settings.preconditionsTitle')"
    :subtitle="t('settings.preconditionsSubtitle')"
    size="sm"
  >
    <ul class="precondition-list">
      <li v-for="item in preconditions" :key="item.key" class="precondition-item">
        <i
          class="bi precondition-icon"
          :class="[preconditionIcon(item.status), `text-${preconditionTone(item.status)}`]"
          aria-hidden="true"
        ></i>
        <div class="precondition-body">
          <div class="precondition-head">
            <span class="precondition-label">{{ item.label }}</span>
            <span class="precondition-state" :class="`text-${preconditionTone(item.status)}`">
              {{ t(`settings.preconditionState.${item.status}`) }}
            </span>
          </div>
          <p class="precondition-detail mb-0">{{ item.detail }}</p>
        </div>
      </li>
    </ul>
    <template #footer>
      <button class="btn btn-primary" type="button" @click="gateInfoOpen = false">
        {{ t('common.close') }}
      </button>
    </template>
  </Sheet>
</template>

<style scoped>
section.aipt-card form :deep(.row.g-3) > [class*='col-'] {
  flex: 0 0 100%;
  max-width: 100%;
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
}

section.aipt-card form :deep(.row.g-3) > [class*='col-'] > .form-label {
  margin-bottom: 0;
}

section.aipt-card form :deep(.row.g-3) > [class*='col-'] > .form-control,
section.aipt-card form :deep(.row.g-3) > [class*='col-'] > .form-select {
  width: 100%;
}

section.aipt-card form :deep(input[type='number']) {
  text-align: right;
}

.settings-plan-title,
section.aipt-card > h2 {
  font-size: 1.15rem;
  font-weight: 650;
  letter-spacing: -0.02em;
}

section.aipt-card .form-label,
section.aipt-card .form-check-label,
section.aipt-card h2,
section.aipt-card h3 {
  color: var(--aipt-text);
}

section.aipt-card .form-check.form-switch {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
  width: 100%;
  min-height: auto;
  padding-left: 0;
}

section.aipt-card .form-check.form-switch .form-check-label {
  order: 1;
}

section.aipt-card .form-check.form-switch .form-check-input {
  order: 2;
  justify-self: end;
  margin: 0;
  float: none;
}

section.aipt-card form :deep(.row.g-3) > [class*='col-']:has(> select),
section.aipt-card form :deep(.row.g-3) > [class*='col-']:has(> input[type='text']),
section.aipt-card form :deep(.row.g-3) > [class*='col-']:has(> input[type='password']) {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 0.25rem;
}

section.aipt-card form :deep(.row.g-3) > [class*='col-']:has(.form-check) {
  display: block !important;
}

.gate-list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.2rem;
}

.gate-list-title {
  font-weight: 600;
  color: var(--aipt-text);
}

.precondition-list {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
  margin: 0;
  padding: 0;
  list-style: none;
}

.precondition-item {
  display: flex;
  gap: 0.65rem;
  align-items: flex-start;
}

.precondition-icon {
  flex: 0 0 auto;
  font-size: 1rem;
  line-height: 1.4;
}

.precondition-body {
  min-width: 0;
  flex: 1 1 auto;
}

.precondition-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
}

.precondition-label {
  font-size: 0.88rem;
  font-weight: 600;
  color: var(--aipt-text);
}

.precondition-state {
  flex: 0 0 auto;
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.precondition-detail {
  font-size: 0.78rem;
  line-height: 1.45;
  color: var(--aipt-muted);
}

.update-card {
  padding: 0.9rem 1rem;
  border: 1px solid rgba(127, 127, 127, 0.25);
  border-radius: 0.75rem;
}

.update-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
  flex-wrap: wrap;
}

.update-title {
  font-weight: 600;
  color: var(--aipt-text);
}

.update-hint {
  font-size: 0.78rem;
  color: var(--aipt-muted);
}

.update-version {
  color: var(--aipt-text);
}

.update-notes {
  font-size: 0.82rem;
  color: var(--aipt-text);
}

.update-confirm {
  padding: 0.7rem 0.85rem;
  border: 1px solid rgba(127, 127, 127, 0.3);
  border-radius: 0.6rem;
  background: rgba(127, 127, 127, 0.08);
  max-width: 30rem;
}

.update-confirm button + button {
  margin-left: 0.5rem;
}
</style>
