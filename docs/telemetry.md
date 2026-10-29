# Telemetry

Phase 2 adds read-only inverter telemetry, a stored time series and the live
energy-flow dashboard.

## Polling

`internal/service/telemetry.go` polls the inverter immediately on startup and
then every `inverter.telemetry_interval_s` (default 30 s, allowed 5–3600 s).

A poll reads, in order:

1. identity/config (cached after the first successful poll),
2. inverter realtime (`device=2`),
3. grid meter (`device=3`),
4. battery realtime (`device=4`).

The client uses an 8-second timeout per request. A failed poll keeps the last
successful sample, increments a failure counter and logs the first failure and
every tenth one. After three consecutive failures an `telemetry_unavailable`
event is recorded. Recovery resets the counter.

Changing the host, port, scheme, serial, sign overrides, PV configuration or
timezone causes the poller to close the old reader and build a new one on the
next poll. The connectivity test always uses the current saved settings.

## Sample conventions

| Field | Meaning |
|---|---|
| `soc` | Battery state of charge, % |
| `pv_w` | PV generation, W |
| `pv_strings` | Per-MPPT generation, W |
| `grid_w` | Grid power, positive = import |
| `battery_w` | Battery power, positive = charging |
| `load_w` | Derived house load, `pv + grid − battery`, clamped at 0 |
| `inverter_mode` | Selected work mode (`mod_r`) |
| `extra` | SOH, temperatures, voltages, daily energies, device time |

Sign overrides in `inverter.sign_overrides` are applied before the sample is
stored. The energy-flow card colours charging/export green and
discharging/import orange.

## Storage

Every successful sample is written to `telemetry` (`ts_utc` primary key).
Retention removes samples older than 90 days during the daily prune. The latest
sample is also kept in memory for `/status` and SSE, so a database problem does
not hide live data.

## API

`GET /api/v1/status` includes:

```json
"telemetry": {
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

A sample is stale when it is older than `max(90 s, 3 × interval)`. The status
also reports `checks.inverter` as `ok` or `unavailable`.

`POST /api/v1/settings/test/inverter` performs one read-only check with the
saved settings and returns the identity and a fresh sample. It requires a
session and CSRF token but **no hardware arm gate**, because it only reads.
Errors return `502 inverter_unreachable` with the underlying message.

## Simulator

`AIPTIMIZE_SIMULATOR=true` builds an in-process inverter instead of a dongle.
It simulates a PV curve, a house-load profile, battery charge/discharge with
efficiency and SOC limits, and exposes the same HTTP shapes through
`simulator.Handler()` for integration tests. Write endpoints answer `405`.
It is used by the Playwright smoke test and can be used for demos without a
dongle.

## UI

- Dashboard: live energy flow, PV string breakdown, SOC ring, battery state,
  freshness, inverter check and source status.
- Settings → Inverter: host/scheme/port/serial, limits, capacity, SOC,
  efficiencies, telemetry interval, sign calibration, and **Test connection**
  showing the model, firmware, serial and a live sample.

## Tests

- Solplanet parser: captured payloads, units, sign overrides, serial mismatch,
  404 handling, schedule decode and golden codec vectors.
- Simulator: day/night behaviour, SOC evolution, energy balance, HTTP round trip
  with the real client, write rejection.
- Poller: storage, latest snapshot, event publishing, settings-driven reader
  rebuild, failure keeps the last sample, Test does not store.
- Store: insert/upsert/latest/between.
- API: status shape, inverter test success/failure and auth.
- HIL: optional read-only test against a real dongle (see
  [solplanet.md](solplanet.md)).
