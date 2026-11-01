# EV charging

Phase 6 adds EV charging plans, a smart controller and a Zaptec Go cloud
adapter, with a deterministic simulator for demos and tests.

## Adapters

The `ev.Charger` contract is:

```go
ID() string
Type() string
Capabilities() Capabilities
Status(ctx) (domain.ChargerStatus, error)
SetCurrent(ctx, amps float64) error
Pause(ctx) error
Resume(ctx) error
Close() error
```

- **`simulator`** is connected by default in simulator mode. It clamps current
  to 6–16 A, accumulates session energy and supports pause/resume.
- **`zaptec_cloud`** uses OAuth ROPC (`/oauth/token`), reads
  `/api/chargers/{id}/state`, writes `AvailableCurrent` through
  `/api/installation/{id}/update` and pauses/resumes with commands 506/507.
  Current changes are rate-limited to one installation-wide update per 15
  minutes. It observes session energy (`553`), power (`513`), current (`708`)
  and phases (`519`); it cannot observe vehicle SOC.
- Credentials are environment-only: `AIPTIMIZE_ZAPTEC_USERNAME`,
  `AIPTIMIZE_ZAPTEC_PASSWORD`, `AIPTIMIZE_ZAPTEC_INSTALLATION_ID`,
  `AIPTIMIZE_ZAPTEC_CHARGER_ID`, optional `AIPTIMIZE_ZAPTEC_BASE_URL`.
  A real adapter is built when simulator mode is off and credentials are set.

## Configuration

Settings → EV stores mode (`off`/`now`/`smart`), min/max current, phases,
voltage, solar share, smart cost limit, feed-in priority limit, always-charge,
house-battery floor, grid-charging acknowledgment and plan defaults. Validation
enforces 6–32 A, min ≤ max, 1–3 phases, 100–300 V and 0–1 solar share.

`pv`/`minpv` are normalized to `smart`. Grid charging is disabled by default;
enabling it is an explicit acknowledgment that the house battery may be drained.

`allow_ev_battery_charging` controls whether the EV may draw from the house
battery. When it is off, the EV controller switches the inverter to reserve
mode while the charger reports `charging`, and restores the previous work mode
when charging stops. This interlock requires both inverter write gates to be
armed; it is skipped for an off-grid work mode.

## Plans

A plan is either static (`deadline_local`, target kWh) or repeating
(`weekdays`, `HH:MM`, target kWh). The planner uses 15-minute slots:

```text
Pmax = min(config max current, adapter max, site import / (phases * voltage))
      * phases * voltage
remaining = max(0, target_kwh - (session_energy - offset))
required_slots = ceil(remaining / (Pmax * 0.25 / 1000))
```

- `cheapest`: rank eligible slots by effective import price and select the
  required count; merge contiguous windows; prefer later slots on ties.
- `continuous`: choose the cheapest contiguous run.
- With `grid_charge_allowed=false`, only slots whose PV surplus covers the
  minimum charging power are eligible; if the target cannot be met the plan is
  partial and marked `unreachable`.
- The final window is shortened so the plan does not over-deliver energy.
- Every run is stored in `ev_plan_runs` with windows, duration, status and
  warnings.

States: `none`, `scheduled`, `active`, `target_reached`, `overrun`,
`unreachable`, `expired`, `cleared_on_unplug`, `mode_mismatch`.

## Controller

The controller evaluates every 15 seconds. It only sends commands when
`AIPTIMIZE_ENABLE_EV_CONTROL=true` and the UI EV arm switch are both on.
- `off`: pause.
- `now`: resume and set maximum current.
- `smart`: resume inside a plan window, pause outside; PV windows use minimum
  current, grid windows the selected current. Target reached pauses and marks
  the plan.
- Current writes are limited to one per 15 minutes.

The EV plan's expected load is passed to the battery planner per 30-minute
block. Battery discharge slots are rejected if they overlap a grid-sourced EV
window, and the combined import is clamped by the site limit. Recomputing the
battery plan after an EV plan change is left to the next planner cycle.

## API

| Method | Path |
|---|---|
| GET | `/api/v1/chargers`, `/chargers/{id}`, `/chargers/{id}/status` |
| PATCH | `/chargers/{id}/config` |
| POST | `/chargers/{id}/mode`, `/current`, `/pause`, `/resume` |
| GET | `/chargers/{id}/plan`, `/chargers/{id}/session` |
| POST | `/chargers/{id}/plan/static`, `/plan/preview` |
| DELETE | `/chargers/{id}/plan/static?id=` |
| PUT | `/chargers/{id}/plans/repeating` |

Mode/current/pause/resume return `409 ev_control_disarmed` unless armed.
`/status.ev` carries the live charger status and latest plan for the dashboard.

## UI

The Car page shows charger state, power, current, session energy and mode as
metric cards, three descriptive mode cards (Off / Now / Smart), the current/
pause controls, a charging-plan card with target, remaining, duration, windows
and warnings, and a static-plan form (target/deadline/strategy/precondition).
The home energy flow includes the car node when a charger is configured.
Settings → EV edits the charger configuration.

## Tests

Planner cheapest/continuous/precondition/partial/target/expiry, target trimming,
simulator status/pause, Zaptec token/state/update/command fixture, API
charger/plan/gate tests, battery/EV discharge exclusion and mobile Playwright
smoke tests.

## Limitations

- Zaptec cannot observe vehicle SOC; plans target energy.
- Pause/resume command preconditions are best effort; rejected commands are not
  retried blindly.
- Real Zaptec control requires HIL verification and credentials; the demo uses
  the simulator and EV control remains disarmed by default.
