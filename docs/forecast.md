# Forecast

Phase 3 adds a 48-hour PV and house-load forecast, cached in memory and shown
on the dashboard.

## PV model

Each configured MPPT string (`pv.strings`, 1–3) has `kWp`, `tilt`, compass
`azimuth`, `performance_ratio`, `temp_coefficient` and an optional `max_w`.
Open-Meteo is queried once per distinct tilt/azimuth pair:

```http
GET https://api.open-meteo.com/v1/forecast
  ?latitude=…&longitude=…&timezone=UTC&forecast_days=2
  &hourly=global_tilted_irradiance,temperature_2m
  &tilt=<tilt>&azimuth=<openMeteoAzimuth>
```

Open-Meteo azimuth is `0 = south`, `-90 = east`, `90 = west`, `±180 = north`, so
the client converts compass `azimuth - 180`, wrapped to ±180.

Irradiance and air temperature are linearly interpolated at each 30-minute
block midpoint, then per string:

```text
Tcell      = Tair + (GTI / 800) * 25
tempFactor = 1 + tempCoefficient * (Tcell - 25)
P          = clamp(kWp * 1000 * (GTI / 1000) * PR * tempFactor, 0, MaxW)
```

Strings are summed and clipped to `pv.inverter_ac_limit_w`; if clipping occurs,
the per-string values are scaled proportionally so the chart remains
consistent.

The forecast horizon starts at the current half-hour and spans 48 hours. The
`pv.confidence` value is exposed to the planner; it is not multiplied into the
forecast itself.

## Load model

`load.daily_kwh` is distributed over the 24-value normalized profile:

```text
loadW(hour) = dailyKWh * 1000 * profile[hour]
```

When `use_measured_profile` is enabled, telemetry from the last 14 days is
grouped into 7 × 24 weekday/hour buckets. EV energy (`ev_w`) is subtracted from
measured load. Buckets with at least four samples contribute their median; the
blend weight grows from 0 to a maximum of 70 % as the sample count rises, and
the configured profile remains the anchor. If fewer than 500 samples, fewer
than three days, or fewer than half the buckets are populated, the configured
profile is used and a warning is returned.

## Polling, cache and staleness

`ForecastService` refreshes immediately at startup and every
`pv.refresh_minutes` (default 30, minimum 5). The latest snapshot is held in
memory together with a seven-day map of past forecast values used for
calibration.

- A provider error keeps the previous snapshot and marks it `stale`.
- After three hours without a successful refresh, PV is set to zero and a
  warning is added; load remains visible.
- With no PV strings, the forecast is load-only with an explicit warning.
- `POST /api/v1/forecast/refresh` refreshes on demand.

No forecast table is added to SQLite in this phase; the planner will persist the
exact input snapshot with each plan in phase 4.

## Calibration

When `pv.calibration_enabled` is on, the service compares past forecast values
(recorded in memory as snapshots are produced) with measured `pv_w` samples
from the last seven days at matching 30-minute blocks. At least 20 matching
samples are required. The factor is `measured / forecast`, clamped to
0.5–1.5, applied to future PV values and reported in the snapshot. Forecast
history is in memory, so calibration starts fresh after a restart; the UI shows
the factor and sample count.

## API

`GET /api/v1/forecast` returns:

```json
{
  "generated_at": "2026-10-29T21:56:00Z",
  "horizon_start": "2026-10-29T21:30:00Z",
  "horizon_end": "2026-10-31T21:30:00Z",
  "timezone": "Europe/Stockholm",
  "confidence": 0.7,
  "calibration_factor": 1,
  "calibration_samples": 0,
  "stale": false,
  "pv_total_kwh": 34.66,
  "load_total_kwh": 24.0,
  "strings": [{ "name": "East", "kwp": 4.2, "tilt": 35, "azimuth": 120 }],
  "points": [
    { "start": "…", "end": "…", "pv_w": 0, "pv_strings": [0, 0], "load_w": 504.1 }
  ],
  "warnings": [],
  "source": "Open-Meteo"
}
```

`POST /api/v1/forecast/refresh` returns the same snapshot. Both require a
session; refresh also needs the CSRF token. They do not need the hardware arm
gate.

`/status` exposes `forecast.available`, `stale`, `generated_at`,
`pv_total_kwh`, `load_total_kwh` and `source`.

## Simulator

With `AIPTIMIZE_SIMULATOR=true` a deterministic seasonal solar curve replaces
Open-Meteo, so demos and Playwright do not depend on the internet. The response
shape and load model are unchanged; `source` is `Simulator`.

## UI

The dashboard card “Solar & load forecast” shows 24/48-hour totals, confidence,
a dual-axis chart (PV area, house-load line), source, update time, warnings and
a manual refresh button. Settings → Solar edits the 1–3 strings and forecast
parameters; Settings → Load edits daily energy, the measured-profile switch and
the 24-hour profile.

## Tests

- Open-Meteo client: query parameters, azimuth conversion, null values, HTTP and
  JSON errors.
- PV model: formula, interpolation, per-string clipping, AC limit, simulator.
- Load: configured profile, measured blending, EV exclusion, not-ready warning.
- Forecast service: 48-hour points, totals, fallback/staleness, no-string mode,
  simulator, calibration disabled.
- API: refresh/read, status, auth and no-string mode.
- UI: dashboard card/chart, 24/48-hour toggle, solar/load settings and mobile
  layout.
