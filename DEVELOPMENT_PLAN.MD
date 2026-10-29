# Aiptimize v1 — standalone battery + EV energy scheduler (Go + Vue, Docker)

> Status: **Draft — implementation not started**
> Date: 2026-10-29
> Repository: `github.com/AhmetBarac/aiptimize`
> License: GPL-3.0
> Scope: home-battery scheduling; editable battery plans; EVCC-style EV charging plans and configuration

## 1. Objective and success criteria

Build one Docker service that:

- ingests Nord Pool day-ahead prices (15-minute MTU, public Data Portal API, no key);
- forecasts PV for **1–3 configured MPPT strings** and house load;
- plans **up to 4 battery slots per local day**, each starting on `:00`/`:30`, lasting **0.5–4 h in 0.5 h steps**, charge or discharge;
- writes the plan to the **Solplanet ASW08kH-T2** through the AI-Dongle's weekly Custom-mode schedule, then reconciles and verifies it;
- lets the user **edit the battery charge/discharge plan** slot by slot, preview the resulting SOC/cost, and **resubmit** it to the inverter;
- controls the **Zaptec Go** through the Zaptec cloud API from PV surplus and prices, behind a **pluggable charger interface** so other chargers/communication modes can be added;
- provides an **EVCC-style EV charging plan and configuration**: charge modes, current/phase limits, smart price and solar behaviour, one-off and repeating target plans, a cheapest/continuous charge planner, preview and warnings;
- serves a **mobile-first, evcc-inspired** web GUI;
- fails safe to self-consumption, and never writes to hardware unless explicitly armed.

**Acceptance criteria**

| Area | Criterion |
|---|---|
| Prices | 96 × 15-min points → 48 × 30-min blocks; DST days (92/100 points) correct; missing tomorrow never scheduled |
| Plan | ≤ 4 slots per local day, total across directions; 30–240 min; `:00`/`:30`; no overlap; no slot crosses local midnight; SOC floor/reserve respected |
| Actuation | Schedule payload is byte-matchable to the reference codec; write/readback/audit recorded; watchdog clears a stale schedule; dry-run never writes |
| Plan editing | User can add/remove/move/resize/redirect slots, see a validated SOC/cost preview, and resubmit the edited plan to the inverter; auto replans never silently overwrite an applied manual plan |
| EV plan | Static and repeating target plans (energy by deadline) generate cheapest/continuous windows on the tariff and PV forecast; preview matches execution; target reached and overrun are handled |
| EV config | Modes `off`/`now`/`smart`, always-charge, smart cost limit, solar share, min/max current, phases, battery floor and grid-charge gating behave as specified |
| EV control | Solar-surplus current follows measured PV within Zaptec's 15-minute cadence; pause/resume only in valid states; rate limits respected |
| UI | Usable at 390 × 844; dashboard, plan editor, prices, car plan/config, settings; English + Swedish; no horizontal scroll |
| Ops | `docker compose up -d --build` runs non-root with a healthcheck, persistent `/data`, and survives restart |
| Quality | `go test -race ./...`, `go vet`, frontend unit tests, Playwright mobile smoke test, and HIL checklist pass |

## 2. Decisions and assumptions

- **Backend:** Go 1.27 (installed locally), standard `net/http` router; `modernc.org/sqlite` (CGO-free); `log/slog`; `time/tzdata` embedded. No Python runtime in production.
- **Frontend:** Vue 3 + TypeScript + Vite + **Bootstrap 5 + ECharts 6** (the same visual stack as evcc) + Vue Router + vue-i18n. Built by Vite into the Go binary via `//go:embed`.
- **EV first adapter:** **Zaptec cloud REST**. The `Charger` interface allows later `ocpp16j`, `evcc`, `mqtt`, `homeassistant`, go-e, Easee adapters.
- **EV plan model:** mirrors evcc's loadpoint plan and configuration. Because the Zaptec Go cloud API exposes session energy but **not vehicle SOC**, v1 plans target **energy in kWh** by deadline; the data model reserves SOC targets for a future vehicle-SoC adapter. The UI supports one-off and repeating plans, precondition, and cheapest/continuous strategies.
- **EV smart-charging config:** modes `off`/`now`/`smart` (with `pv`/`minpv` accepted as aliases), always-charge, smart cost limit, feed-in priority limit, solar share, min/max current, phases, priority, house-battery floor, and battery discharge control. Grid EV charging is opt-in because the home battery can otherwise supply the car.
- **Battery hardware:** ASW08kH-T2 with AI-Dongle on `:8484` (or `:443` HTTPS on newer firmware), 15.36 kWh, 3 MPPT of which 2 are used. Verified facts and codec come from [solplanet-schedule.md](C:/Users/ahmet/.projects/aiptimize-evcc/docs/solplanet-schedule.md).
- **Slots:** 4 total per local day by default (configurable up to the hardware's 6). `Pin`/`Pout` are global, so all slots use the configured charge/discharge power; per-slot power is not expressible.
- **Manual plan override:** an edited plan locks the affected local day; the auto planner continues to compute a suggestion but never overwrites an applied manual plan until the user resets to automatic or the day rolls over. A draft is not effective until resubmitted.
- **Outside slots:** the inverter self-consumes — the desired resting state and fail-safe. The app never writes raw register `41104` (the effective mode); it only writes the schedule and selects `mod_r = 4` (Custom) via `setbattery`.
- **Single controller:** this app replaces evcc/old scheduler control. evcc must be stopped or have battery/charger control disabled. Zaptec must be switched from OCPP 1.6J mode back to **Zaptec / Default Charging mode** and a service account created. The Zaptec cloud API cannot start a brand-new session; it limits current and pauses/resumes an authorized session.
- **Site import limit:** a configured `site.max_import_power_w` (default 17,250 W for 3 × 25 A × 230 V) bounds battery charging + EV charging + house load together.
- **Write safety:** dry-run by default; writes require both an environment gate and an in-app arm confirmation. EV control has its own gate.
- **License:** GPLv3 (the existing repository license is preserved).
- **Units:** powers in W, energy in kWh, money in the configured currency with `float64` internally and 2-decimal display; all timestamps stored/transferred in UTC; local conversion only for display and slot alignment.
- **One site / one installation.** Multi-site, user roles, native apps, and notifications are out of scope for v1.
- **4-hour slots:** the codec can encode 240 minutes; the vendor app has only been observed up to 180, so this is explicitly HIL-verified and can be capped at 180 via config.

## 3. Architecture and data flow

```text
Nord Pool ──┐
Open-Meteo ─┼─► EV planner ──► EV plan ──────────────┐
EV config ──┘                                        │
                                                     ├─► battery planner ──► plan store ──► plan editor/draft ──► schedule builder ──► Solplanet dongle ──► inverter
Solplanet telemetry ────────────────────────────────┘                                      │
Zaptec cloud ◄── EV controller ◄── EV plan + telemetry                                      └─► readback/audit
       │
       └─► status snapshot ──► SSE/polling ──► Vue SPA
```

| Component | Responsibility | Failure behaviour |
|---|---|---|
| Price poller | Fetch today/tomorrow, cache, expose normalized blocks | Keep last valid data; warn; never plan tomorrow without prices |
| Forecast poller | PV per MPPT + load profile; cache snapshots | Use last forecast with staleness flag; zero PV after expiry |
| EV planner | Turn EV config + static/repeating plan into charge windows from tariff/PV; preview and warnings | No feasible plan → partial/simple plan with warning; never guesses |
| EV controller | Execute modes/plans: solar surplus, smart price limit, plan windows, pause/resume | Keep last safe limit; warn; never spam Zaptec |
| Telemetry poller | Poll inverter every 30 s, meter, battery SOC | Stale data blocks new applies; watchdog clears after timeout |
| Battery planner | Build inputs (including EV planned load), run DP, store suggestion | No plan on missing/stale critical inputs; keep previous valid plan |
| Plan editor/validator | Draft edits, validate constraints/SOC/EV overlap/site limit, preview, revisions | Reject invalid drafts; never write without resubmit + confirmation |
| Schedule reconciler | Compare desired/read schedule; write only if armed and drift | Backoff; alert; never retry blindly |
| Watchdog | Clear schedule when data is stale too long | Clear = self-consumption |
| Web/API | Auth, JSON, SSE, embedded SPA | 401/503 with typed errors |

Loops are independent goroutines with contexts, timeouts, and a shared immutable snapshot; one failing loop cannot block the others.

## 4. Repository layout

```text
aiptimize/
├── cmd/aiptimize/            # main; subcommands: serve, plan, apply, status, clear, healthcheck, version
├── internal/
│   ├── config/               # env loading, defaults, validation, secret handling
│   ├── store/                # SQLite, embedded migrations, repositories, retention
│   ├── domain/               # PricePoint, Block, Slot, Plan, Telemetry, Schedule, ChargerStatus
│   ├── prices/               # nordpool_dataportal provider, cache, block aggregation
│   ├── forecast/
│   │   ├── pv/               # Open-Meteo client, per-MPPT model, calibration
│   │   └── load/             # profile forecast, optional measured profile
│   ├── evplan/               # EV config, plan model, tariff/PV planner, state, preview
│   ├── planner/              # battery DP optimizer, constraints, explanations, savings
│   ├── planedit/             # plan drafts, validation, SOC/cost preview, revisions
│   ├── inverter/
│   │   ├── solplanet/        # dongle client, codec, telemetry, schedule writer
│   │   └── simulator/        # in-process fake inverter + HTTP fixture server
│   ├── charger/
│   │   ├── charger.go        # interface, capabilities, registry, config
│   │   ├── zaptec/           # OAuth, REST client, state mapping, commands
│   │   ├── simulator/
│   │   └── evcontrol/        # mode/plan state machine, hysteresis, sessions
│   ├── service/              # pollers, coordinators, reconciler, watchdog, event bus
│   ├── api/                  # handlers, auth/session, SSE, static SPA, middleware
│   └── webui/                # //go:embed dist
├── web/                      # Vue 3 + TS + Vite + Bootstrap + ECharts
│   ├── components/ev/        # Car plan card, plan modal, config, preview
│   └── components/plan/      # plan editor, slot sheet, preview chart
├── deploy/                   # Dockerfile, compose.yaml, .env.example
├── docs/                     # architecture, planning, solplanet, chargers, ev-charging, plan-editing, safety, api, adr/
├── testdata/                 # Nord Pool / Open-Meteo / dongle / Zaptec / EV plan fixtures
├── go.mod, Makefile, README.md, LICENSE
```

## 5. Domain model, persistence, settings

**Database:** `/data/aiptimize.db`, SQLite WAL, `busy_timeout=5000`, `foreign_keys=ON`, migrations embedded and applied at startup.

```sql
users(id, username, password_hash, created_at)
sessions(id, user_id, created_at, expires_at, last_seen, ip)
settings(key TEXT PRIMARY KEY, value_json TEXT, updated_at)
prices(area, currency, delivery_start, delivery_end, price_mwh, state, fetched_at,
       PRIMARY KEY(area, currency, delivery_start))
plans(id, created_at, horizon_start, horizon_end, timezone, status, inputs_hash,
      summary_json, inputs_json, origin, revision, locked_day, note,
      applied_at, applied_by, parent_plan_id)
plan_slots(plan_id, idx, day_local, start_utc, end_utc, direction, power_w, energy_kwh,
           avg_import_price, expected_saving, reason)
plan_revisions(id, plan_id, revision, created_at, actor, action, slots_json, summary_json)
schedule_writes(id, ts_utc, kind, payload_json, response, readback_json, ok,
                plan_id, plan_revision)
telemetry(ts_utc PRIMARY KEY, soc, pv_w, pv_strings_json, grid_w, battery_w,
          load_w, ev_w, inverter_mode, extra_json)
ev_configs(charger_id PRIMARY KEY, config_json, updated_at)
ev_plans(id, charger_id, kind, weekdays_json, deadline_local, target_kind,
         target_energy_kwh, target_soc_pct, precondition_min, strategy, active,
         created_at, updated_at)
ev_plan_runs(id, plan_id, computed_at, deadline_utc, required_kwh, duration_min,
             windows_json, status, warnings_json)
ev_sessions(id, charger_id, started_at, ended_at, energy_kwh, cost, mode,
            plan_id, target_kwh, energy_offset_kwh)
ev_commands(id, ts_utc, charger_id, command, params_json, result, error)
events(id, ts_utc, severity, code, message, details_json)
```

Indexes on `prices(delivery_start)`, `plans(created_at)`, `plans(locked_day)`, `telemetry(ts_utc)`, `events(ts_utc)`, `plan_slots(plan_id)`, `plan_revisions(plan_id)`, `ev_plan_runs(plan_id)`. Retention: telemetry 90 days, events 1 year, sessions kept; prune daily after a WAL checkpoint.

**Plan origin and status.** `origin` is `auto` or `manual`; `status` is `suggestion`, `draft`, `applied`, `superseded`, or `expired`. A manual plan locks one local day (`locked_day`), increments `revision` on every edit, and records every edit/apply in `plan_revisions`. `schedule_writes` links a write to the exact plan revision.

**Settings sections** (JSON in `settings`, defaults seeded, PATCH-validated; secrets never stored here):

| Section | Defaults / fields |
|---|---|
| `site` | name, timezone `Europe/Stockholm`, latitude, longitude, `max_import_power_w` `17250` |
| `inverter` | type `solplanet_ai_dongle`, host, scheme `http`, port `8484`, serial, max charge `6000 W`, max discharge `3000 W`, capacity `15.36 kWh`, min SOC `15`, max SOC `100`, charge/discharge efficiency `0.95`, cycle cost `0`, reserve SOC `15`, telemetry interval `30 s`, sign overrides |
| `pv` | 1–3 strings: name, kWp, tilt 0–90°, compass azimuth 0–359°, performance ratio `0.85`, temp coefficient `-0.004/°C`, optional max W; inverter AC limit `8000 W`; confidence `0.7`; refresh `30 min` |
| `price` | provider `nordpool_dataportal`, area `SE4`, currency `SEK`, grid fee, energy tax, VAT %, export fee |
| `plan` | max slots/day `4`, max slot `240 min`, horizon `2 days`, min daily saving `1.0`, preserve SOC `true`, end SOC target, SOC step `0.5%`, charge `6000 W`, discharge `3000 W`, allow discharge to grid |
| `load` | daily kWh `12`, 24-hour normalized profile, use measured profile `false` |
| `ev` | defaults, with per-charger overrides in `ev_configs`: enabled, mode `off`/`now`/`smart`, default mode, min current `6 A`, max current `16 A`, phases `3`, voltage `230 V`, priority, solar share `1.0`, smart cost limit, feed-in priority limit, always charge `off`/`on`/`once`, house battery floor `20%`, battery discharge control `true`, grid charge allowed `false`, battery boost `false`, plan defaults (`strategy`, `precondition_min`) |
| `safety` | writes armed (derived from env + UI), EV control armed, watchdog clear after `1800 s`, horizon limit `2 days`, foreign-schedule takeover `false` |

**Secrets / gates (environment only):** `AIPTIMIZE_ADMIN_PASSWORD`, `AIPTIMIZE_SESSION_SECRET`, `AIPTIMIZE_ZAPTEC_USERNAME`, `AIPTIMIZE_ZAPTEC_PASSWORD`, `AIPTIMIZE_ZAPTEC_INSTALLATION_ID`, `AIPTIMIZE_ENABLE_WRITES`, `AIPTIMIZE_ENABLE_EV_CONTROL`, `AIPTIMIZE_TZ`, `AIPTIMIZE_LISTEN`, `AIPTIMIZE_DATA_DIR`, `AIPTIMIZE_LOG_LEVEL`, `AIPTIMIZE_SIMULATOR`.

## 6. Price ingestion

**Provider:** `GET https://dataportal-api.nordpoolgroup.com/api/DayAheadPrices` with `date=YYYY-MM-DD`, `market=DayAhead`, `deliveryArea=<area>`, `currency=<currency>`. No key or special headers; send a descriptive `User-Agent: aiptimize/<version> (+repo URL)` and self-limit to ~1 req/s.

Status handling:
- `200`: parse `multiAreaEntries[].deliveryStart/deliveryEnd/entryPerArea[area]` — values are **currency/MWh**; divide by 1000 for kWh.
- `204`: tomorrow not published yet; retry every 15 min, no alarm before ~13:30 local.
- `400`/`401`: configuration or authority error; alarm and keep last data.
- `areaStates[0].state` (`Final`/`Preliminary`/`Missing`) is stored and shown; a `Preliminary` day may still change and triggers a re-plan when it becomes `Final`.

Schedule: fetch on startup, then at 13:02 local, retry every 15 min until tomorrow is available (or 23:00), and refresh today every 6 h.

**Aggregation:** map each 15-min point to the half-hour block containing its UTC start; block price is the mean of its two points. A block with fewer than two points is *missing* and excluded from planning. Derive days from returned UTC instants, never from counts, so DST works (92/96/100 points). The EV planner consumes the same 15-minute points directly, so EV windows can be finer than battery slots.

**Tariff formula** (shown in the UI):

```text
import = (spot_sek_kwh + grid_fee + energy_tax + markup) * (1 + vat/100)
export = spot_sek_kwh - export_fee
```

Negative prices are valid. `export` is floored by the configured export fee but never silently changed. Prices in other currencies are displayed in that currency; no FX conversion is invented.

Attribution: show “Spot prices: Nord Pool Data Portal” and document that the endpoint is the website backend, intended for personal/low-volume use, with no redistribution.

## 7. PV and load forecast

**MPPT model (1–3 strings).** Each string: `kWp`, `tilt`, compass `azimuth`, `performance_ratio`, optional max W. The UI allows adding/removing up to 3 strings; the plan sums them and clips to the inverter AC limit.

**Open-Meteo** (free, no key): one request per distinct tilt/azimuth:

```text
GET https://api.open-meteo.com/v1/forecast
  ?latitude=…&longitude=…&timezone=UTC&forecast_days=2
  &hourly=global_tilted_irradiance,temperature_2m
  &tilt=<tilt>&azimuth=<openMeteoAzimuth>
```

Open-Meteo's azimuth convention is **0° = south, -90° = east, 90° = west, ±180° = north**, so convert from compass: `openMeteoAzimuth = compassAzimuth - 180`, wrapped to ±180.

Per string:

```text
Tcell = Tair + (GTI / 800) * 25
tempFactor = 1 + (-0.004) * (Tcell - 25)
P_w = clamp(kWp * 1000 * (GTI / 1000) * PR * tempFactor, 0, stringMaxW)
```

Interpolate hourly power to 30-min block midpoints. Apply `pv.confidence` (default 0.7) in the planner. Cache 30 min; on failure reuse the last snapshot with a `stale` flag; after 3 h stale, plan with PV = 0 and warn. Optional calibration factor from the last 7 days of measured PV (clamped 0.5–1.5), behind a setting.

**Load:** default `daily_kwh` distributed over a 24-value normalized profile (configurable), split evenly into half-hours on system inital setup. Contunous update of profile: median measured house load by local weekday/hour over 14 days, blended with the configured profile, EV charging consuption excluded, enabled only after enough telemetry exists. The EV plan's expected load is added on top for the battery planner.

Every plan stores the exact forecast and input snapshot used, so the UI can explain and reproduce it.

## 8. Planner

**Inputs:** price blocks (import/export), PV forecast per block, load forecast, **EV plan windows and expected grid/PV load**, current SOC, battery limits/efficiency, slot budget, site import limit, reserve/end-SOC policy.

**Hard constraints**

- Total slots per local day ≤ `max_slots_per_day` (default 4, both directions share the budget).
- Slot duration 1–8 blocks (30–240 min), start at block boundaries (`:00`/`:30` local).
- No overlap, no direction change inside one slot, no slot crossing local midnight.
- SOC within `[min_soc, max_soc]`; no discharge below the reserve floor.
- Power clamped to `min(config, measured hardware limit)`.
- Battery charge + EV charge + house load must not exceed `site.max_import_power_w`.
- Avoid battery discharge slots overlapping EV grid-charging windows; if unavoidable, warn and prefer the EV plan.

**Objective** — minimise net cost over the horizon:

```text
netW = loadW + evW + chargeW - dischargeW - pvW
cost = max(netW,0)*0.5/1000 * importPrice
     - max(-netW,0)*0.5/1000 * exportPrice
     + cycleCost * dischargeKWh
```

SOC transition uses charge/discharge efficiencies; cycle cost defaults to 0 but is exposed for battery wear.

**DP algorithm** (pure Go, deterministic):

```text
state = (blockIndex, socStep, usedSlotsToday, remainingBlocks, direction)
value = minimum cost to reach the end
```

At each block:
- if a slot is active (`remainingBlocks > 0`): force that direction for this block, decrement;
- otherwise: idle, or start charge/discharge for `d = 1..min(8, blocks until local midnight, horizon end)` if the slot budget allows;
- after a block lands on local midnight, `usedSlotsToday` resets to 0.

Terminal states must satisfy `endSoc >= min(endTarget, startSoc)` (preserve by default); add the cost of the whole horizon.

**Outputs:** slots (local start/end, direction, power, energy, average price, expected saving, explanation), per-block curve (`price`, `pvW`, `loadW`, `batteryW`, `socPct`), summary (cost, baseline cost, savings, cycles, charge/discharge kWh).

**Post-processing / policy**
- Compute a no-battery baseline; if savings < `min_daily_savings`, return an empty plan (self-consumption) with the reason.
- Prefer fewer slots on ties with a tiny slot penalty; deterministic action ordering.
- If no tomorrow prices, plan only up to local midnight.
- If SOC is stale (>5 min), do not produce an applyable plan; show a warning and keep the previous plan.
- If measured SOC is below reserve at apply/reconcile time, remove upcoming discharge slots.
- Re-plan on new prices/forecast, **EV plan changes**, settings changes, day change, or SOC deviation >10% at a slot boundary.
- **Manual override:** if a manual plan is applied for a day, the auto planner stores its new result as a `suggestion` and never writes it. The UI shows both and offers “Use automatic.”

**Performance:** 96 blocks × ~200 SOC steps × 5 budget states × 9 remainder states × 2 directions is small; target < 500 ms. If a configuration exceeds a state cap, coarsen SOC to 1%.

**Tests:** brute-force comparison on tiny horizons, invariant fuzzing (slot count, duration, overlap, midnight, SOC, reserve), golden cases for cheap night/expensive evening, midday PV, negative prices, 4 h slot, DST day, missing tomorrow, EV-load and site-import effects.

## 9. Battery actuation (Solplanet)

**Client endpoints** (`internal/inverter/solplanet`):
- `GET /getdefine.cgi` — read the weekly schedule.
- `POST /setting.cgi` `{"device":4,"action":"setdefine","value":{...}}` — write all seven days and `Pin`/`Pout`; no delete action, so clearing means explicit zeros.
- `POST /setting.cgi` `{"device":4,"action":"setbattery","value":{"sn":…,"mod_r":4,"type":1}}` — select Custom mode before applying; never write raw `41104`.
- `GET /getdevdata.cgi?device=2&sn=…` — inverter/PV data, including `vpv` per MPPT.
- `GET /getdevdata.cgi?device=3` — grid meter.
- `GET /getdevdata.cgi?device=4&sn=…` — battery `soc`, `pb`, `bst`, `mod_r`, `charge_max`, `discharge_max`.
- `POST /fdbg.cgi` — read-only register diagnostics only; no raw writes in v1.

**Codec:** packed 32-bit slot `(hour<<24)|(minute<<16)|(durationMinutes<<8)|0x02|discharge`; day keys exactly `Mon, Tus, Wen, Thu, Fri, Sat, Sun`; max 6 slots/day hardware; durations 30–240 min; starts `:00`/`:30`; no slot crosses midnight. Golden vectors are ported from the reference (`0x1E02`, `0x011E1E02`, `0x02003C02`, `0x04007802`, `0x0200B402`, `0x0E1E3C02`, `0x0B1EB402`, `0`).

**Apply:** build the full 7-day payload with only today and tomorrow populated (all other days zeroed), `Pin = charge power`, `Pout = discharge power`. Write, then read back and store both. A readback mismatch is a warning, not a failed write, per the hardware notes.

**Resubmit an edited plan.** The user's draft is stored in SQLite; it never touches the inverter until **Resubmit to inverter** is pressed. Resubmit:
1. revalidates the draft against current time, telemetry, SOC, EV plan, site limit, and the structural slot rules;
2. requires `writes armed`, `confirm: true`, a matching plan `revision`, and no changed slot that has already started;
3. builds the schedule from the effective plan (manual day + automatic other day), writes `setdefine`, reads back, and records `schedule_writes(plan_id, plan_revision)`;
4. is idempotent: if the inverter already matches, it reports “already matches” and writes nothing;
5. returns `409` if the plan revision is stale, if the active slot would be interrupted, or if a foreign schedule must first be taken over.

**Guard rails**
- Requires writes armed (`AIPTIMIZE_ENABLE_WRITES=true` + UI confirmation), fresh telemetry, a fresh applyable plan, and no past slots.
- Write rate limit: at most once per 60 s; do not rewrite while a slot is active unless the plan changed or a safety clear is needed.
- **Foreign schedule detection:** if the inverter's schedule does not match what this app last wrote, mark `foreign_schedule`; refuse to overwrite until the user chooses **Take control** (which clears and writes).
- **Watchdog:** if successful telemetry/plan is older than `watchdog_clear_after` (default 30 min), clear the schedule and raise an event. On graceful shutdown, clear best-effort. On startup, read and reconcile.
- **Weekly recurrence hazard:** the schedule repeats weekly; writing only today + tomorrow and clearing all other days bounds staleness. Document that a hard crash after a write leaves at most those days programmed until restart.
- **HIL caveats:** 210/240-minute slots are encodable but have not been observed from the vendor app; verify with a low-power 30-min test first. Ai-Dongle ports/HTTPS variants and power sign conventions are calibrated during commissioning and stored as sign overrides.

**CLI:** `serve`, `plan` (dry-run), `apply`, `status`, `clear`, `healthcheck`, `version`.

## 10. EV charging — plans, configuration and control

**Reference:** evcc's loadpoint model (charge modes, limits, smart-charging switches, static/repeating plans, precondition/continuous strategy, plan preview) is the design baseline. See [evcc docs](https://docs.evcc.io/en/) and the local sources `evcc/core/loadpoint_plan.go`, `evcc/core/planner/`, and `evcc/assets/js/components/ChargingPlans/`. The Zaptec Go cloud adapter differs from evcc's local OCPP control: it exposes session energy but not vehicle SOC, and current changes are limited to one installation-wide update per 15 minutes.

### 10.1 Adapter contract

```go
type Charger interface {
    ID() string
    Type() string
    Capabilities() Capabilities
    Status(ctx) (Status, error)
    SetCurrent(ctx, amps float64) error
    Pause(ctx) error
    Resume(ctx) error
    Close() error
}

type Capabilities struct {
    MinCurrentA, MaxCurrentA float64
    Phases                   int
    CurrentScope             string // "charger" | "installation"
    SupportsPause            bool
    SupportsPhases           bool
    ControlLatency           time.Duration
}
```

A factory registry maps `type` to constructor; adapter config is JSON validated per type. v1 ships `zaptec_cloud` and `simulator`; `ocpp16j` and `evcc` are interface-ready but not implemented.

### 10.2 Zaptec Go adapter (first implementation)

- OAuth ROPC: `POST https://api.zaptec.com/oauth/token`, form `grant_type=password&username=…&password=…&scope=openid`; token 3600 s; cache and refresh before expiry. Descriptive `User-Agent` mandatory.
- Reads: `GET /api/installation`, `GET /api/installation/{id}`, `GET /api/chargers?InstallationId=…`, `GET /api/chargers/{id}`, `GET /api/chargers/{id}/state`.
- Control: `POST /api/installation/{id}/update` with `{"AvailableCurrent": <A>}` — installation-wide, **at most once per 15 minutes**; pause/resume via `POST /api/chargers/{id}/sendCommand/506` and `/507`, only when the state preconditions hold.
- Key observations: `-2` online, `501–503` voltages, `507–509` currents, `513` total charge power, `519` phases, `553` session energy, `708` charge current set, `710` charger operation mode, `718` FinalStopActive, `721` session identifier.
- **No vehicle SOC:** the cloud API has no state-of-charge observation. EV plans therefore target energy (kWh), and session progress comes from observation `553`.
- **Prerequisites:** installation in **Default Charging mode** (`AvailableCurrentMode == 0`), not OCPP/APM mode; a service account with owner/service rights. The setup/preflight page checks this and gives instructions if not.
- **Limits:** cannot authorize/start a new session; Zaptec Go is fixed-phase; min 6 A. Current control is coarse (15-minute cadence), which the UI states explicitly. The plan's windows use 15-minute boundaries to match.
- Error handling: 401 → one token refresh retry; 429/5xx → exponential backoff with jitter; never retry a rejected pause/resume blindly.

### 10.3 EVCC parity: modes and smart-charging switches

| Mode | Behaviour |
|---|---|
| `off` | Charger paused/current 0. A saved plan is not executed. |
| `now` | Charge at max current immediately, ignoring the plan; the UI warns that the plan is advisory. |
| `smart` | Normal mode: follow PV surplus, obey the plan windows and the smart/price switches below. |
| `pv` / `minpv` | Accepted as aliases and normalized to `smart` with always-charge off / on, as evcc does. |

Smart-mode switches:

- `always_charge` `off`/`on`/`once`: at least min current; `once` resets on unplug.
- `smart_cost_limit`: when the total import price is at or below the limit, charge at least min current from the grid.
- `smart_feed_in_priority_limit`: pause EV charging when the export price is at or above the limit, so the surplus is exported instead.
- `solar_share`: the fraction of minimum charging power that must be solar before PV charging starts (`1.0` = full min current must be solar).
- `house_battery_floor_soc`: pause EV charging when the house battery is below this SOC (best effort; no hold).
- `priority`: retained for future multiple-charger arbitration; one charger in v1.

With `grid_charge_allowed=false`, none of `smart_cost_limit`, `always_charge` or `solar_share` may cause grid import: the controller requires the full minimum charging power from PV surplus. A plan is executed in `smart` mode. In `off`/`now`, the plan is saved and previewed but the UI warns “plan only applies in smart mode.”

### 10.4 Configuration

Per-charger configuration (stored in `ev_configs`; credentials remain environment-only):

```text
enabled, mode: off|now|smart, default_mode,
min_current_a: 6, max_current_a: 16,
phases: 3, voltage_v: 230, priority: 0,
solar_share: 1.0,
smart_cost_limit_sek_kwh: null,
smart_feed_in_priority_limit_sek_kwh: null,
always_charge: off|on|once,
house_battery_floor_soc: 20,
battery_discharge_control: true,
grid_charge_allowed: false,
battery_boost: false, battery_boost_limit_soc: 100,   // phase 8
plan_defaults: { strategy: cheapest, precondition_min: 0 }
```

Validation: min 6 A; max ≤ charger/installation limit and ≤ 32 A; phases must be supported by the adapter (Zaptec Go is fixed); `solar_share` 0–1; price limits optional; grid charging disabled unless `grid_charge_allowed=true` and the battery-drain warning is acknowledged; battery boost/hold hidden or disabled until HIL approval.

### 10.5 Plan model and states

- **Static plan:** `target_energy_kwh`, `deadline` (local date/time), `precondition_min`, `strategy` (`cheapest`/`continuous`), `active`.
- **Repeating plans:** `weekdays[]`, `deadline_time`, `target_energy_kwh`, `timezone`, `active`; the next matching occurrence is selected.
- One active plan at a time; the earliest future deadline wins. Static plans take priority over repeating plans for the same deadline.
- Remaining energy = `target_kwh − (session_energy_kwh − plan_energy_offset_kwh)`; the offset is captured when the plan is set mid-session.
- States: `none`, `scheduled`, `active`, `target_reached`, `overrun`, `unreachable`, `expired`, `cleared_on_unplug`, `mode_mismatch`.
- Overrun: continue charging past the deadline until the target is reached while connected, with a visible warning; clear on unplug.
- SOC targets are modelled (`target_kind: energy|soc`) but the v1 UI defaults to energy; SOC requires a future vehicle-SoC adapter.

Plan warnings (mirroring evcc): target in the past, target beyond the tariff horizon, target unreachable in time, plan overrun, target above a configured limit, mode not smart, grid charging disabled, PV-only partial plan, car unplugged.

### 10.6 Plan algorithm

```text
Pmax = min(config.max_current, charger.max_current,
           site.max_import_power_w / (phases * voltage)) * phases * voltage
remaining = max(0, target_kwh - (session_energy - offset))
duration = remaining / Pmax, rounded up to 15-minute tariff slots
```

- Candidate windows are 15-minute tariff slots in `[now, deadline]`. For a PV-only plan, only slots where the forecast PV surplus covers the minimum charging power are eligible.
- `cheapest`: sort candidates by effective cost (grid price; zero or negative for covered PV surplus), take until the required duration, prefer later slots on ties, merge contiguous windows, and shorten the final window.
- `continuous`: find the cheapest contiguous window of the required duration.
- `precondition`: reserve the final `precondition_min` before the deadline as continuous; plan the remainder earlier.
- If time is insufficient: continuous plan from now + `unreachable`/overrun warning.
- If tariff coverage is insufficient or no tariff exists: fall back to a simple plan ending at the deadline; warn.
- If `grid_charge_allowed=false`: produce a PV-only plan; if the target cannot be met, show a partial plan with an explicit warning.
- Every computed run is persisted (`ev_plan_runs`) for the UI and audit; recompute on price/forecast/settings/session changes.

### 10.7 Runtime controller and battery/site coordination

- Evaluate every 15 s; enforce at most one installation-current change per 15 minutes.
- Effective current is the clamped maximum of the active plan window, smart PV current, smart price-limit current, and always-charge current; minimum 6 A; hysteresis for start/stop; feed-in-priority stop; battery-floor pause.
- Plan window active: target max current for grid windows, or PV-following for PV windows. Outside the window: 0. Target reached: pause and mark `target_reached`.
- Pause/resume only in valid Zaptec states (`710=3` for stop, `710=5` and `718=1` for resume); audit every command.
- **Battery protection and grid charging:** PV-only charging is surplus-based and cannot drain the battery by construction. Grid charging (`now`, smart cost limit, grid plan windows) can drain the home battery; v1 therefore requires `grid_charge_allowed=true` with an explicit acknowledgment. A HIL-verified inverter battery hold is phase 8; until then the UI states the limitation and the default is PV-only.
- EV expected load per 30-minute block is passed to the battery planner; EV grid-charge windows are excluded from battery discharge slots; combined battery + EV + house import is clamped to `site.max_import_power_w`.
- Recompute the battery plan when the EV plan changes. If a manual battery plan is applied, mark it `stale` and warn instead of overwriting it.
- On target reached or unplug: stop/pause, clear the session offset, update the EV session record.

### 10.8 EV UI

- **Car page:** mode selector, plan card (target kWh, deadline, projected start/end, remaining kWh, active state, overrun), tariff+plan chart, warnings, pause/resume, current limit, session energy/cost, Zaptec cadence notice.
- **Charging plan modal:** static plan form (target kWh, day/time, precondition, cheapest/continuous, active), repeating plans list (weekdays/time/target/active), plan preview on the tariff chart, and warnings. Mirrors evcc's plan modal.
- **EV settings:** mode/default mode, min/max current, phases, solar share, smart cost limit, feed-in priority, always charge, house battery floor, battery discharge control, grid-charge acknowledgment, Zaptec preflight.
- **Dashboard EV card:** current/next plan window, remaining energy, mode state, warning icon.

### 10.9 EV API and schema

```text
GET    /chargers
GET    /chargers/{id}
PATCH  /chargers/{id}/config
POST   /chargers/{id}/mode
POST   /chargers/{id}/current
POST   /chargers/{id}/pause
POST   /chargers/{id}/resume
GET    /chargers/{id}/status
GET    /chargers/{id}/plan?at=
POST   /chargers/{id}/plan/static
DELETE /chargers/{id}/plan/static
PUT    /chargers/{id}/plans/repeating
POST   /chargers/{id}/plan/preview
GET    /chargers/{id}/session
```

Plan preview returns windows, required energy/duration, cost, warnings, and mode mismatch. Errors: `409` when EV control is not armed, `422` for infeasible plans, `503` when the charger/cloud is unreachable.

Schema is defined in §5 (`ev_configs`, `ev_plans`, `ev_plan_runs`, extended `ev_sessions`).

### 10.10 EV tests and acceptance

- Plan: cheapest vs continuous, precondition, target unreachable, insufficient tariff, PV-only partial plan, session offset, target reached, overrun, unplug, repeating next occurrence, DST.
- Config: ranges, phase capability, solar-share limits, grid-charge gating, mode normalization.
- Controller: smart cost limit, always charge, feed-in priority, battery floor, plan windows, 15-minute rate limit, pause/resume preconditions, backoff.
- UI: plan modal, preview chart, warnings, mode switching, mobile layout.
- Acceptance: a synthetic day produces the expected cheapest windows; no battery discharge slot overlaps an EV grid window; grid charging is blocked unless acknowledged; a session stops at target energy.

## 11. Battery plan editing and resubmission

### 11.1 State model

- Plans carry `origin` (`auto`|`manual`), `status` (`suggestion`|`draft`|`applied`|`superseded`|`expired`), `revision`, `locked_day`, and `note`.
- A **manual override locks one local day**. The auto planner continues to compute and store a suggestion, but never overwrites a draft or applied manual plan for that day.
- Effective plan = applied manual plan for the day, otherwise applied auto plan. A draft is never effective until resubmitted.
- The manual lock ends on “Use automatic,” day rollover, or expiry; the auto plan then becomes effective again.
- Every edit/apply creates a `plan_revisions` row (actor, action, slots, summary). `schedule_writes` links to the exact revision.

### 11.2 Editing rules

- Edit today or tomorrow only; past days are read-only.
- 0–4 slots per local day (shared budget); 30–240 min in 30-minute steps; starts `:00`/`:30`; no overlap; no slot crosses midnight; power fixed by the configured `Pin`/`Pout`.
- Add, delete, move (start), resize (duration), and change direction of a slot.
- A slot that has already started is immutable. The user may edit future slots, or use **Clear schedule** to stop.
- The editor starts from the effective plan; “Reset to automatic” discards the manual draft/lock.

### 11.3 Validation and preview

Server-authoritative validation, mirrored in the UI:

- structural constraints above;
- SOC simulation with current SOC and forecast: reject SOC below `min_soc`/reserve; warn if end SOC is below target;
- no past slots; no changes to active slots;
- EV overlap: error if a discharge slot overlaps an EV grid-charge window when `battery_discharge_control` is effective; warning otherwise;
- site import limit check;
- negative-savings warning (allowed with confirmation);
- stale telemetry (>5 min) blocks apply; stale forecast only warns.

Preview response includes per-block SOC/price/PV/load, cost vs baseline and vs auto, savings, validation errors/warnings, and the exact schedule payload diff.

### 11.4 Apply / resubmit

- **Resubmit plan** writes the current effective (edited) plan to the inverter, whether it is the first apply or a reapply after drift.
- Requires writes armed, fresh telemetry, fresh plan, explicit `confirm: true`, and no changed active slot.
- Builds the full 7-day schedule with today+tomorrow populated, writes `setdefine`, reads back, records `schedule_writes` linked to `plan_id`/`revision`, and marks the plan `applied`.
- Idempotent: resubmitting an identical plan performs no hardware write and reports “already matches.”
- If the inverter holds a foreign schedule, require **Take control** before resubmit.
- If the active slot is being changed, return `409` and instruct the user to wait or clear; never silently interrupt a running slot.

### 11.5 API and schema

```text
GET    /plan?date=                     # auto, draft, applied, effective, lock, validation, schedule diff
POST   /plan/drafts                   # {date, revision, slots, note} -> draft + preview + validation
POST   /plan/drafts/{id}/preview      # revalidate without saving
POST   /plan/drafts/{id}/apply        # {confirm:true, revision} -> write + readback + audit
DELETE /plan/drafts/{id}              # discard draft
POST   /plan/auto/apply               # apply auto suggestion, clear manual lock
GET    /plan/revisions?date=          # revision history
POST   /plan/recompute                # existing auto recompute
```

Schema is defined in §5 (`plans`, `plan_slots`, `plan_revisions`, `schedule_writes`). Optimistic concurrency: every write requires the current `revision`; a mismatch returns `409` with the latest draft. Drafts expire at the local day boundary.

### 11.6 UI

- `/plan` gains an **Edit** toggle; slot cards with direction toggle, start picker, duration stepper (+/− 30 min), delete, and add-slot action.
- Live chart preview as the user edits (debounced); validation banner; SOC curve; cost/savings vs auto.
- Header actions: **Save draft**, **Resubmit to inverter**, **Reset to automatic**, **Discard**; status chip (`auto`, `manual draft`, `manual applied`, `stale`, `inverter differs`).
- Mobile: tap a slot to open a bottom-sheet editor with time/duration controls and 44 px targets; add via a floating action button; no drag requirement (desktop drag optional).
- Schedule card shows the inverter’s current weekly schedule vs the effective plan, last write/readback, and the resubmit result.
- Dashboard warning when a manual override is active or the auto suggestion has changed.

### 11.7 Tests and acceptance

- Validation matrix: 4/day, 30–240 min, half-hour starts, overlap, midnight, DST, reserve SOC, EV overlap, site import.
- Concurrency: stale revision → 409; two tabs; draft preserved across auto replans.
- Apply/resubmit: payload matches expected slots; readback; idempotent no-op; active-slot protection; foreign-schedule takeover; audit revision.
- Lock/reset: auto suggestion continues; manual survives recompute; day rollover expires lock; reset applies auto.
- UI: mobile edit flow, add/remove/resize, validation error, preview, resubmit, reset; Playwright at 390 × 844.
- Acceptance: an edited plan can be saved, previewed, resubmitted, verified on the inverter, and survives an automatic replan without being overwritten.

## 12. HTTP API

All endpoints under `/api/v1`; JSON; RFC3339 UTC timestamps; cookie auth; CSRF required for state-changing methods.

| Method | Path | Purpose |
|---|---|---|
| GET | `/healthz`, `/readyz` | Liveness / readiness (unauthenticated, minimal) |
| POST | `/auth/login`, `/auth/logout`; GET `/auth/me` | Session auth |
| POST | `/setup` | One-time first-run setup (only while no user exists) |
| GET | `/status` | Live snapshot: energy flow, SOC, price now/next, plan, EV, health |
| GET | `/stream` | SSE stream (`status`, `plan`, `event`) with heartbeat |
| GET | `/prices?from=&to=` | 15-min points + 30-min blocks + state |
| GET | `/plan?date=` | Auto, draft, applied and effective plans; lock, validation, schedule diff |
| POST | `/plan/recompute` | Recompute the automatic plan (dry-run unless armed) |
| POST | `/plan/drafts` | Save an edited plan draft; returns preview/validation |
| POST | `/plan/drafts/{id}/preview` | Revalidate/re-preview without saving |
| POST | `/plan/drafts/{id}/apply` | Resubmit the edited plan to the inverter (requires `confirm: true`) |
| DELETE | `/plan/drafts/{id}` | Discard the draft |
| POST | `/plan/auto/apply` | Apply the auto suggestion and clear the manual lock |
| GET | `/plan/revisions?date=` | Plan revision history |
| GET | `/schedule` | Inverter schedule, last write, readback, match/foreign state |
| POST | `/schedule/clear` | Clear schedule (requires confirmation) |
| POST | `/schedule/takeover` | Clear foreign schedule and take control |
| GET/PATCH | `/settings` | Read all settings; patch a section; secrets masked |
| POST | `/settings/test/inverter` | Connectivity/preflight check |
| POST | `/settings/test/charger` | Zaptec auth/installation/mode check |
| GET | `/chargers`, `/chargers/{id}`, `/chargers/{id}/status` | Adapter status |
| PATCH | `/chargers/{id}/config` | EV mode/limits/plan defaults |
| POST | `/chargers/{id}/mode` | Set EV mode |
| POST | `/chargers/{id}/current` | Manual current override |
| POST | `/chargers/{id}/pause`, `/resume` | Explicit controls |
| GET | `/chargers/{id}/plan?at=` | Active/next EV plan + preview |
| POST | `/chargers/{id}/plan/static`; DELETE | Set/clear a static plan |
| PUT | `/chargers/{id}/plans/repeating` | Replace repeating plans |
| POST | `/chargers/{id}/plan/preview` | Preview without saving |
| GET | `/chargers/{id}/session` | Current/previous EV session |
| GET | `/history?from=&to=&resolution=` | Energy/cost aggregates (phase 7) |
| GET | `/events?since=` | Warnings/audit feed |
| GET | `/meta` | Version, timezone, gates, feature flags |

Errors: `{"error":{"code":"price_stale","message":"…","details":{…}}}` with HTTP `400/401/403/409/422/503`. State-changing endpoints reject when writes/EV control are not armed (`409`).

## 13. Web GUI

**evcc inspiration:** card-based dashboard, large live values, colour-coded energy flow, plan drawn on the tariff chart, bottom tabs on mobile. Code reference: [evcc UI](https://docs.evcc.io/en/) and the local `evcc/assets/js` tree.

| Route | Screen | Content |
|---|---|---|
| `/login`, `/setup` | Auth / wizard | Password login; first-run wizard: admin → inverter test → battery → 1–3 MPPT strings → price area/tariffs → Zaptec test → EV plan defaults → arm choices |
| `/` | Dashboard | Energy-flow card (solar, grid, battery, house, car), SOC ring, current/next price, active/next slot, plan status chip, EV plan card, warning banners |
| `/plan` | Plan + editor | Today/tomorrow price chart with charge/discharge overlays, PV area, SOC curve; slot list with reasons and expected savings; Edit mode with add/move/resize/retarget, live preview, Save draft, Resubmit, Reset to automatic; draft/applied/verified/mismatch/foreign/stale states |
| `/prices` | Prices | 15-min bars/table, min/max/average, cheapest windows, area/currency, preliminary badge, source attribution |
| `/car` | Car | Online/connected/charging state, power/current, PV surplus gauge, mode selector, current limit, pause/resume, session kWh/cost, **charging plan card**, **plan modal** (static/repeating, target kWh, deadline, precondition, cheapest/continuous), plan chart, warnings, cloud-cadence notice |
| `/settings/:section?` | Settings | Inverter & battery, Solar (1–3 strings), Prices, Planner, EV (modes, limits, solar share, smart cost, feed-in priority, battery floor, grid-charge acknowledgment), System; connectivity tests; arm/clear actions; export settings JSON; version/logs |
| `/history` | History | Daily/weekly energy, cost, savings, plan-vs-actual (phase 7) |

**Mobile-first:** fixed bottom tab bar (Home, Plan, Prices, Car, Settings) with safe-area padding below 768 px; desktop switches to a left navigation; sticky header; 44 px minimum targets; single-column forms with numeric keyboards; charts full-width; no hover-only actions; tested at 390 × 844 and 1280 × 800.

**Plan editor UX:** tap a slot or its bar to open a bottom sheet; direction toggle, start time, duration stepper, delete; add slot via floating action button; live validation and cost/SOC preview; explicit **Resubmit to inverter** with confirmation and result banner. Desktop may add drag handles, but tapping/forms must remain complete.

**EV plan UX:** Car page plan card opens a modal mirroring evcc's charging plan: target kWh, day/time, precondition, cheapest/continuous, active switch, repeating plans (weekdays/time/target/active), plan preview on the tariff chart, and warnings. Mode and config controls live beside it.

**Design system:** light/dark auto theme with CSS variables; green for solar/charge, orange for discharge/grid import, blue for the car; 16 px card radius; tabular numerals; reduced-motion support. Bootstrap 5 CSS + small custom layer, ECharts 6 for charts, Bootstrap Icons.

**Charts:** price bars with plan mark-areas, PV area on a second axis, SOC line; a “now” marker; tooltips with local time and currency; a legend that is tappable on mobile. EV plan windows render on the same tariff chart with a deadline marker.

**i18n / a11y:** every string in `en.json` and `sv.json`; API errors carry stable codes and are translated client-side; focus management, labels, `aria-live` for live values, contrast ≥ 4.5:1, colour never the only signal.

**PWA:** manifest, icons, theme-colour, standalone display; no service worker in v1 (avoid stale control data).

## 14. Security, configuration, operations

- **First run:** if no user exists, only setup + static assets are reachable; setup creates the admin (password + basic settings) and logs in. If `AIPTIMIZE_ADMIN_PASSWORD` is set, it seeds the user and disables setup. Document that the first boot should happen on a trusted LAN.
- **Auth:** single admin; PBKDF2-SHA256 (≥600k iterations, random salt) using stdlib crypto; server-side session table; HttpOnly `Secure`/`SameSite=Lax` cookie; 30-day idle expiry; CSRF double-submit token; login rate limit 5/min/IP; logout revokes.
- **Headers:** CSP `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'`; no CORS; `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`, `Permissions-Policy`.
- **Write gates:** `AIPTIMIZE_ENABLE_WRITES=true` **and** UI confirmation; EV gate separately. UI shows both. Disarmed mode can still compute and preview plans. Manual plan apply/resubmit and all EV commands require the same gates and explicit confirmation.
- **Audit:** every plan revision, resubmit, schedule write/readback, EV mode/current/pause/resume command, and foreign-schedule takeover is recorded with actor, timestamp, and revision; manual overrides are never anonymous.
- **Secrets:** env only, never returned by the API, never logged; Zaptec tokens in memory; redaction middleware.
- **Network:** inbound 8080 on the LAN; outbound to the inverter LAN IP, Nord Pool, Open-Meteo, Zaptec. TLS terminates at a reverse proxy; document the proxy config and `Secure` cookie setting.
- **Data:** `/data` volume; SQLite WAL; session key `/data/session.key` (0600); settings export/import as JSON; document backup via a WAL checkpoint copy.
- **Logging/observability:** JSON logs to stdout with request IDs; every hardware write and EV command audited; `/healthz` for Docker; `/readyz` checks DB, telemetry age, price freshness, schedule writer; optional `/metrics` (Prometheus) behind auth in phase 7.

## 15. Docker and CI

**Dockerfile (multi-stage)**

1. `node:24-alpine`: `npm ci`, `npm run build` → `/web/dist`.
2. `golang:1.27-alpine`: `go mod download`, copy source + dist, `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=…"`.
3. `alpine:3.22`: `ca-certificates`, `tzdata`; user `aiptimize` (uid 10001); `/data` owned by it; copy binary + `webui/dist`; `USER 10001`; `EXPOSE 8080`; `VOLUME /data`; `HEALTHCHECK CMD ["/app/aiptimize","healthcheck"]`.

**Compose:** one service, `restart: unless-stopped`, `ports: 8080:8080`, `env_file: .env`, `volumes: aiptimize-data:/data`, `stop_grace_period: 30s`, `read_only: true` with `tmpfs: /tmp`, `security_opt: ["no-new-privileges:true"]`.

**Makefile:** `fmt`, `vet`, `test`, `test-race`, `web-build`, `build`, `docker`, `e2e`.

**CI** (`.github/workflows/ci.yml`): Go 1.27 + `go vet` + `gofmt -l` + `go test -race ./...`; Node 24 + `npm ci` + typecheck + unit tests + build; Docker build; Playwright mobile smoke against the simulator. Nightly fuzz job.

## 16. Testing and acceptance

| Layer | Cases |
|---|---|
| Unit — prices | Fixtures for 96/92/100-point days; 15→30 aggregation; 204/401/400; preliminary/final; missing block; DST; negative prices; tariff math |
| Unit — codec | All golden vectors; every duration 30–240; `:00`/`:30`; day keys `Tus`/`Wen`; clear all zeros; reject overlaps, >6 slots, cross-midnight, bad starts |
| Unit — battery planner | Brute-force small horizons; known optimal day; PV/reserve/efficiency/cycle-cost effects; 4 h slot; 4/day cap; midnight; DST; missing tomorrow; no-op when savings < threshold; EV-load and site-import effects; fuzzed invariants |
| Unit — EV plan | Cheapest vs continuous; precondition; target unreachable; insufficient tariff; PV-only partial; session offset; target reached; overrun; unplug; repeating next occurrence; DST; preview/warnings |
| Unit — EV config/controller | Mode normalization; min/max/phase/solar-share validation; smart cost limit; always charge; feed-in priority; battery floor; plan windows; 6 A floor; hysteresis; pause/resume preconditions; 15-min rate limit; token refresh; 429/5xx backoff; no secrets in logs |
| Unit — plan editing | 4/day, 30–240 min, half-hour starts, overlap, midnight, DST, reserve SOC, EV overlap, site import; active-slot immutability; revision conflict; manual lock/reset; auto-replan non-overwrite |
| Integration | `httptest` fakes for Nord Pool, Open-Meteo, Solplanet dongle, Zaptec; full daily cycle: prices → EV plan → battery plan → draft → resubmit → schedule payload → readback → status; watchdog; foreign schedule; auth/CSRF |
| Frontend | Vitest for API client, chart transforms, EV plan preview, settings validation, slot editor validation; Playwright at 390 × 844: setup, login, dashboard, battery plan edit/resubmit (dry-run), EV plan modal, mode/config, pause/resume; axe smoke |
| Docker | `docker compose up --build`; `/healthz` 200; non-root; data survives restart; schedule CLI against simulator |
| HIL | Read-only 24 h; dry-run plan review; one 30-min charge and one 30-min discharge at low power; manual edit/resubmit round-trip; SOC floor; 240-min encoding if permitted; clear returns to self-consumption; watchdog by stopping the service mid-plan; verify no raw `41104` writes |

**Acceptance:** all of the above pass; battery plan generation < 500 ms; EV plan preview < 250 ms; no data races; image starts in < 3 s; UI has no critical axe findings.

## 17. Delivery phases

| Phase | Scope | Exit |
|---|---|---|
| 0 — Foundation | Go module, config, SQLite migrations, logging, auth/setup, API skeleton, SSE, Vue shell (tabs/theme/i18n), Dockerfile/compose, CI, Makefile | `docker compose up` serves setup/login + placeholder dashboard; tests green |
| 1 — Prices | Nord Pool client, fixtures, DB, block aggregation, tariff settings, `/prices` UI | Live SE4 data visible; DST/gap tests pass |
| 2 — Telemetry | Solplanet client, codec decode, simulator, 30 s polling, status API, energy-flow dashboard, sign calibration | Real dongle read-only works; simulator works; no writes |
| 3 — Forecast | 1–3 MPPT settings, Open-Meteo client, PV model, load profile, forecast API/chart, cache/staleness | PV/load charts and fallbacks work |
| 4 — Battery planner | DP, constraints, savings, explanations, plan API, plan chart, dry-run, EV-load hook | Golden + invariant tests pass; plan visible and correct |
| 5 — Actuation + editor | Schedule builder/writer, reconcile, watchdog, foreign detection, arm gate, audit; plan drafts, validation/preview, revisions, resubmit UI, HIL | HIL sign-off; manual edit/resubmit verified; empty schedule is safe; watchdog clears |
| 6 — EV plans and control | Charger interface + simulator, Zaptec OAuth/REST, EV config, static/repeating plan planner, preview/warnings, controller, battery/site coordination, API/UI, rate limits | Fake-Zaptec e2e; plan windows match preview; live read-only then control approved |
| 7 — Release | History/stats, full i18n, PWA, backups, readiness/metrics, Playwright, docs, security review, v1.0 tag | Acceptance table complete |
| 8 — Later | Battery hold/boost for grid EV charging, vehicle-SoC adapter, OCPP 1.6J adapter, MILP planner, notifications/MQTT | Separate design + HIL approval |

Phase 5 must not begin until phases 1–4 acceptance and a reviewed HIL checklist are complete. Phase 6 depends on the price/forecast and battery-planning interfaces from phases 1–4 and the write/reconcile machinery from phase 5.

## 18. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Zaptec cloud current is installation-wide and 15-min limited | 15-min controller cadence; explicit UI notice; capability model leaves room for OCPP/local adapters |
| Zaptec cloud exposes no vehicle SOC | Energy-based plans by default; SOC target model reserved for a future vehicle adapter; session energy observation 553 |
| Switching Zaptec from OCPP to Zaptec mode breaks evcc control | Documented migration/rollback; app is the replacement; verify preflight; keep evcc config untouched |
| 240-min slots unverified on vendor firmware | Default 240 per requirement, HIL test; config cap 180 if needed; UI warning |
| Weekly schedule repeats if the service dies | Bounded today+tomorrow write, all other days zeroed; startup reconcile; graceful-shutdown clear; watchdog; docs |
| Battery drains into the car during grid EV charging | v1 default PV-only; grid charging requires `grid_charge_allowed` acknowledgment; battery hold is phase 8 and HIL-gated |
| EV plan forecast/tariff error leaves target unmet | Preview warnings, unreachable/overrun handling, continuous fallback, session-energy monitoring |
| Manual battery plan is unsafe or economically poor | Server validation, SOC/site/EV checks, explicit confirmation, audit, auto suggestion retained, reset to automatic |
| Manual edit conflicts across tabs/auto replan | Optimistic `revision` with 409; manual day lock; drafts preserved; active-slot immutability |
| Combined EV + battery import exceeds the fuse | `site.max_import_power_w` in both planners and the EV controller |
| EV and battery plans conflict | EV plan feeds the battery planner; discharge slots excluded from EV grid windows; recompute on EV plan change; manual plans marked stale |
| Forecast error | PV confidence, reserve SOC, stale-data blocking, optional calibration, no plan on missing data |
| Vendor app / another controller writes the schedule | Foreign-schedule detection; require explicit takeover; readback audit |
| Nord Pool endpoint/terms change | Provider interface + cache; attribution; personal-use note; fail to last valid data with warning |
| DST/timezone bugs | UTC blocks, local-day slot alignment, 92/96/100 tests, embedded tzdata |
| LAN/container reachability | Document Docker networking; connectivity test in settings; clear errors |
| SQLite corruption/backup | WAL, migrations, backup docs, readiness check |

## 19. Out of scope for v1

Native mobile apps; multi-site/multi-user; cloud accounts; evcc fork/integration; OCPP adapter implementation; battery hold/raw setpoint control/battery boost; manual EV plan slot editing; SOC-based EV plans without a vehicle-SoC adapter; multi-charger load balancing; MILP/MPC optimizer; MQTT/Home Assistant/notifications; dynamic export tariffs; vehicle APIs; automatic firmware updates; redistribution of Nord Pool data.

## 20. Documentation deliverables

- `README.md` — features, quick start, screenshots, safety notice.
- `docs/architecture.md` — components, loops, data flow, EV-plan/battery-plan coupling.
- `docs/planning.md` — battery constraints, DP math, objective, worked example.
- `docs/ev-charging.md` — EVCC parity, modes/config, energy-based plan algorithm, Zaptec constraints, preview/warnings.
- `docs/plan-editing.md` — draft/override workflow, validation rules, revision/resubmit, active-slot rules.
- `docs/solplanet.md` — codec, day keys, constraints, verified values, 240-min caveat, sign calibration.
- `docs/chargers.md` — `Charger` interface, Zaptec setup/migration, rate limits, future adapters.
- `docs/api.md` — endpoint reference and examples.
- `docs/safety.md` — arming, watchdog, foreign schedules, grid-charge gating, failure modes, HIL checklist.
- `docs/deployment.md` — Docker, env, backup/restore, reverse proxy.
- `docs/adr/` — Go+embedded SPA, DP planner, schedule actuation, adapter model, Zaptec cloud first, energy-based EV plans, manual battery override.
- Attribution/licensing notes for Nord Pool, Open-Meteo (CC-BY), Zaptec, evcc inspiration, GPLv3.
