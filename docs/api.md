# HTTP API

Base path: `/api/v1`. Responses are JSON; timestamps are RFC3339 UTC. Auth uses
the HttpOnly `aiptimize_session` cookie. State-changing requests additionally
require the double-submit CSRF header.

## CSRF

Every response sets `aiptimize_csrf` when missing. Read that cookie and send it
back as `X-CSRF-Token` on `POST`, `PUT`, `PATCH` and `DELETE`:

```sh
curl -c cookies.txt http://localhost:8080/api/v1/meta
TOKEN=$(grep aiptimize_csrf cookies.txt | awk '{print $7}')
curl -b cookies.txt -H "X-CSRF-Token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' http://localhost:8080/api/v1/auth/login
```

An `Origin` header, when present, must match the request host.

## Errors

```json
{
  "error": {
    "code": "validation_failed",
    "message": "site.timezone: unknown IANA timezone",
    "details": { "section": "site", "field": "timezone" }
  }
}
```

| Status | Codes |
|---|---|
| 400 | `invalid_request` |
| 401 | `unauthorized`, `invalid_credentials` |
| 403 | `csrf_failed`, `origin_mismatch`, `setup_required` |
| 404 | `not_found` |
| 409 | `already_initialized` (and later revision/arming conflicts) |
| 422 | `validation_failed` |
| 429 | `rate_limited` (login: 5 attempts/minute/IP) |
| 5xx | `internal_error` |

## Endpoints implemented in phase 0

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/healthz`, `/readyz` | no | Liveness / readiness |
| GET | `/api/v1/healthz`, `/readyz` | no | Same under the API prefix |
| GET | `/api/v1/meta` | no | Version, timezone, setup state, gates, features |
| POST | `/api/v1/setup` | no | Create the only admin user (first run) and log in |
| POST | `/api/v1/auth/login` | no | Log in |
| POST | `/api/v1/auth/logout` | yes | Revoke the session |
| GET | `/api/v1/auth/me` | session | Current user or `401` |
| GET | `/api/v1/status` | yes | Snapshot used by the dashboard |
| GET | `/api/v1/prices` | yes | 15-minute points, 30-minute blocks, tariff, summary |
| POST | `/api/v1/prices/refresh` | yes | Fetch today/tomorrow (or `date`) from the provider |
| GET | `/api/v1/forecast` | yes | 48-hour PV/load snapshot |
| POST | `/api/v1/forecast/refresh` | yes | Refresh the forecast from Open-Meteo |
| GET | `/api/v1/solar` | yes | Today's measured PV, forecast band and load |
| POST | `/api/v1/settings/test/inverter` | yes | Read-only dongle identity and live sample |
| GET | `/api/v1/settings` | yes | All settings sections |
| PATCH | `/api/v1/settings` | yes | Patch one section |
| GET | `/api/v1/events` | yes | Audit/warning feed (`?since=RFC3339`) |
| GET | `/api/v1/stream` | yes | SSE: `status`, `settings`, later `plan`/`event` |

### Setup

```json
POST /api/v1/setup
{
  "username": "admin",
  "password": "at-least-8-characters",
  "site": {
    "name": "Home",
    "timezone": "Europe/Stockholm",
    "latitude": 59.3293,
    "longitude": 18.0686
  }
}
```

Returns `201` and sets the session cookie. Once a user exists it returns `409
already_initialized`. If `AIPTIMIZE_ADMIN_PASSWORD` is set, the user is seeded at
startup and this endpoint is closed.

### Settings patch

```json
PATCH /api/v1/settings
{
  "section": "site",
  "values": { "max_import_power_w": 17250 }
}
```

Unknown fields, wrong types and out-of-range values are rejected without
changing anything. The response contains the merged section:

```json
{ "section": "site", "values": { "name": "Home", "timezone": "Europe/Stockholm", … } }
```

### Status

`GET /api/v1/status` returns `version`, `now`, `uptime_s`, `site`, `gates`,
`telemetry`, `prices`, `forecast`, `plan`, `ev`, `schedule` and `checks`. Later
phases fill these objects without changing the envelope.

`telemetry`:

```json
{
  "available": true,
  "stale": false,
  "age_s": 3,
  "updated_at": "2026-10-29T21:40:36Z",
  "sample": {
    "ts": "2026-10-29T21:40:33Z",
    "soc": 10,
    "pv_w": 1,
    "pv_strings": [0.4, 0.8],
    "grid_w": 2456,
    "battery_w": 0,
    "load_w": 2457,
    "ev_w": 0,
    "inverter_mode": 4,
    "extra": { "soh": 98, "bst": 1 }
  }
}
```

A sample is stale after `max(90 s, 3 × telemetry interval)`. `checks.inverter`
reports `ok` or `unavailable`.

### Inverter connectivity test

```json
POST /api/v1/settings/test/inverter
{}
```

```json
{
  "ok": true,
  "identity": {
    "serial": "SIM000000000001",
    "model": "ASW08kH-T2",
    "firmware": "V610-05001-07.000",
    "rated_power_w": 8000,
    "capacity_kwh": 12.812,
    "work_mode": 4
  },
  "telemetry": { "soc": 10, "pv_w": 1, "grid_w": 2456, "battery_w": 0, "load_w": 2457 }
}
```

The test only issues GET requests and does not require the hardware arm gate.
It returns `502 inverter_unreachable` when the dongle cannot be read. The serial
in this example comes from the live HIL check; redact it in public reports.

`prices` contains `available`, `points`, `blocks`, `state`, `area`, `currency`,
`stale`, `updated_at`, `now_import`, `now_spot`, `next_import`, `next_start`
and `cheapest_window` once data has been fetched.

### Prices

`GET /api/v1/prices?date=2026-10-29` or
`GET /api/v1/prices?from=…&to=…` (RFC3339, max 7 days). The default range is
today plus tomorrow in the site timezone.

```json
{
  "area": "SE4",
  "currency": "SEK",
  "timezone": "Europe/Stockholm",
  "from": "2026-10-28T23:00:00Z",
  "to": "2026-10-30T23:00:00Z",
  "points": [
    { "delivery_start": "2026-10-28T23:00:00Z", "price_mwh": 2682.93, "price_kwh": 2.68293, "state": "Final" }
  ],
  "blocks": [
    { "start": "2026-10-28T23:00:00Z", "end": "2026-10-28T23:30:00Z", "price_mwh": 2659.76,
      "price_kwh": 2.65976, "import_price": 3.3247, "export_price": 2.65976,
      "state": "Final", "missing": false, "points": 2 }
  ],
  "tariff": { "grid_fee": 0, "energy_tax": 0, "markup": 0, "vat": 25, "export_fee": 0 },
  "summary": {
    "min_import": 2.653, "max_import": 7.241, "avg_import": 3.816,
    "points": 192, "blocks": 96,
    "cheapest_windows": [
      { "start": "2026-10-30T00:00:00Z", "end": "2026-10-30T02:00:00Z",
        "avg_import_price": 2.836, "avg_export_price": 2.269, "blocks": 4 }
    ]
  },
  "state": "Final",
  "fetched_at": "2026-10-29T21:24:46Z",
  "source": "Nord Pool Data Portal",
  "attribution": "Spot prices: Nord Pool Data Portal",
  "stale": false
}
```

`POST /api/v1/prices/refresh` accepts an optional `{"date":"YYYY-MM-DD"}` and
returns:

```json
{
  "results": [
    { "date": "2026-10-29", "ok": true, "points": 96, "state": "Final" },
    { "date": "2026-10-30", "ok": true, "points": 96, "state": "Preliminary" }
  ]
}
```

A day that is not published yields `ok:false` with `code:"price_not_published"`
and HTTP `200`; when every requested day fails with a provider error the
endpoint returns `503` and `code:"price_refresh_failed"` in each result. Price
refresh only touches the local cache, so it needs authentication and CSRF but
not the hardware arm gate.

### SSE

`GET /api/v1/stream` emits named events with JSON data:

```text
event: status
data: {"version":"dev", ...}
```

A `status` event is sent every 15 seconds, an initial snapshot on connect, and
`: ping` heartbeats every 20 seconds. The browser reconnects with
`retry: 5000`.

### Forecast

`GET /api/v1/forecast` returns the latest 48-hour combined snapshot:
`generated_at`, `horizon_start`, `horizon_end`, `timezone`, `confidence`,
`calibration_factor`, `calibration_samples`, `stale`, `pv_total_kwh`,
`load_total_kwh`, `strings`, `warnings`, `source` and `points`. Each point has
`start`, `end`, `pv_w`, `pv_strings` and `load_w`.

`POST /api/v1/forecast/refresh` refreshes from Open-Meteo and returns the same
shape. Both require a session; refresh also requires CSRF. They are read-only
and need no arm gate. The full model is documented in
[forecast.md](forecast.md).

`forecast` in `/status` contains `available`, `stale`, `generated_at`,
`pv_total_kwh`, `load_total_kwh` and `source`.

`GET /api/v1/solar` returns today's production and forecast:

```json
{
  "date": "2026-10-29",
  "timezone": "Europe/Stockholm",
  "now": "2026-10-29T22:07:00Z",
  "current_w": 0,
  "actual_kwh": 0,
  "expected_kwh": 19.17,
  "min_kwh": 13.42,
  "max_kwh": 24.93,
  "load_kwh": 12,
  "has_forecast": true,
  "stale": false,
  "generated_at": "2026-10-29T22:07:00Z",
  "points": [
    { "start": "…", "end": "…", "actual_w": null, "expected_w": 0,
      "min_w": 0, "max_w": 0, "load_w": 420.1 }
  ],
  "warnings": []
}
```

The measured value is the 30-minute average of stored `pv_w` samples; it is
`null` before measurement. The expected band comes from `pv.confidence` and is
clamped to the inverter AC limit.

## Planned endpoints

See §12 of [DEVELOPMENT_PLAN.MD](../DEVELOPMENT_PLAN.MD) for schedule, plan
editing, charger and history endpoints. They are added in phases 2–7 and
documented here as they land.
