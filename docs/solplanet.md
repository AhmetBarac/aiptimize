# Solplanet dongle (read-only)

This document records the verified AI-Dongle HTTP interface used by Aiptimize.
Phase 2 implements **reads only**: identity, live telemetry and the weekly
schedule. No `setting.cgi` call exists in the client.

## Endpoints and units

| Endpoint | Fields used | Units / notes |
|---|---|---|
| `GET /getdev.cgi?device=2` | `inv[0].isn`, `model`, `msw`, `rate`, `add`, `safety` | serial, model, firmware, rated W, phases |
| `GET /getdev.cgi?device=4&sn=` | `mod_r`, `type`, `discharge_max`, `charge_max`, `battery.capacity` | work mode, min/max SOC %, capacity Wh |
| `GET /getdevdata.cgi?device=2&sn=` | `vpv[]`, `ipv[]`, `pac`, `tmp`, `vac[]`, `tim` | `vpv` 0.1 V, `ipv` 0.01 A, powers W, temperature 0.1 °C |
| `GET /getdevdata.cgi?device=3` | `pac`, `itd`, `otd`, `iet`, `oet` | grid W (positive import), today's energy 0.01 kWh, totals 0.1 kWh |
| `GET /getdevdata.cgi?device=4&sn=` | `ppv`, `soc`, `soh`, `pb`, `bst`, `vb`, `tb`, `etdpv` | PV W, SOC/SOH %, battery W, state, voltage 0.01 V, temp 0.1 °C, energy 0.1 kWh |
| `GET /getdefine.cgi` | `Mon`…`Sun` arrays, `Pin`, `Pout` | weekly schedule (packed codes) |

The serial field is `isn`. `getdevdata.cgi` returns `404` when the `sn`
parameter does not match `isn` exactly, so the client discovers the serial from
`getdev.cgi?device=2` when none is configured and verifies it when one is.

## Normalized sample

```text
pv_w       = ppv                      (fallback: Σ vpv/10 × ipv/100)
pv_strings = per MPPT vpv/10 × ipv/100
grid_w     = meter.pac                (positive = import)
battery_w  = −battery.pb              (positive = charging; wire positive is discharging)
load_w     = pv_w + grid_w − battery_w
```

Each of PV, grid and battery is multiplied by its sign override
(1 or −1) from `inverter.sign_overrides`. The default conventions were checked
against the read-only capture: positive `pb` was observed while the battery was
discharging, and positive meter `pac` while the house imported.

Other verified fields are kept in `extra`: `bst`, `cst`, `soh`, battery voltage
and temperature, inverter temperature, PV AC power, today's import/export/PV
energy, phase count and the device clock string.

`tim` is `YYYYMMDDhhmmss` in the device's local time. Aiptimize stamps samples
with the server receive time in UTC and keeps `device_time` only as diagnostic
data.

## Schedule codec (decode)

Each slot is one 32-bit integer:

```text
slot = (hour << 24) | (minute << 16) | (durationMinutes << 8) | 0x02 | discharge
```

- hour `0..23`, minute `0` or `30`, duration `30..240` in 30-minute steps;
- bit 1 is the marker (`0x02`); bit 0 is `1` for discharge;
- day keys are exactly `Mon, Tus, Wen, Thu, Fri, Sat, Sun` (`Tus`/`Wen` are the
  firmware's spelling and must not be corrected);
- `0` means an empty slot; the dongle has no delete action, so clearing writes
  explicit zeros.

Golden vectors (all decoded in tests):

| Intent | Code |
|---|---|
| 30 min charge from 00:00 | `0x1E02` |
| 30 min charge from 01:30 | `0x011E1E02` |
| 60 min charge from 02:00 | `0x02003C02` |
| 120 min charge from 04:00 | `0x04007802` |
| 180 min charge from 02:00 | `0x0200B402` |
| 60 min charge from 14:30 | `0x0E1E3C02` |
| 180 min charge from 11:30 | `0x0B1EB402` |
| 60 min discharge from 19:00 | `0x13003C03` |
| empty | `0` |

Invalid entries (bad hour/minute/duration or marker) are skipped and reported as
warnings so a foreign or corrupt schedule can still be inspected. The client
does not write or validate slot overlaps yet; that arrives with the schedule
writer in phase 5.

## Verified read-only on hardware (2026-10-29)

ASW08kH-T2, AI-Dongle at the site LAN, firmware `V610-05001-07.000`:

| Check | Result |
|---|---|
| Identity | `ASW08kH-T2`, serial present, rated 8000 W, 3 phases |
| Battery config | `mod_r=4` (Custom), capacity 12.812 kWh, SOC limits 10–100 % |
| Live sample | SOC 10 %, PV ~1 W at night, grid import 2456 W, battery 0 W, load 2457 W |
| Extra | SOH 98 %, battery 307.3 V / 20.6 °C, 44.96 kWh imported today |
| Schedule | readable, all seven days empty at the time |
| Writes | none — the client only issues GETs |

Run the optional hardware smoke test with:

```sh
AIPTIMIZE_HIL_HOST=<dongle-ip> go test ./internal/inverter/solplanet -run Live -v
```

It is skipped in normal test runs and CI.

## Fixtures

`testdata/solplanet/` contains the captured payload shapes. Identity fixtures
have documentation serials (`SIM…`) and IPs; telemetry captures contain no
serials. The live capture is the source of the parser units and is described in
`testdata/solplanet/README.md`.

## Caveats

- `pb` sign is confirmed by capture, but verify it during commissioning for your
  firmware and use `sign_overrides` if needed.
- Firmware may add unknown fields; the parser ignores them.
- 210/240-minute slots decode correctly but are not yet written by this app.
- The weekly schedule repeats; phase 5 bounds it to today and tomorrow.
