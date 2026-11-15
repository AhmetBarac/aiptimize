# SAJ H1 inverter adapter

Status: **under development** — implemented and tested against a fake Modbus device, not yet proven on a live installation. Settings shows the same warning.

Code: [`internal/inverter/saj`](../internal/inverter/saj) · Register map: [`registers.go`](../internal/inverter/saj/registers.go) · Tests: [`client_test.go`](../internal/inverter/saj/client_test.go)

## Hardware

SAJ **H1** series hybrid inverters with a battery, reachable over **Modbus TCP**. The adapter follows the register map used by the Home Assistant integration [h3llrais3r/ha-saj-h1-mqtt](https://github.com/h3llrais3r/ha-saj-h1-mqtt).

Requirements:

- Modbus TCP reachable on the inverter (or its dongle); port **502**, unit id **1**.
- A battery connected and reporting a state of charge.
- Only one Modbus client at a time — keep Home Assistant or the installer tool closed while Aiptimize polls.

## Using it in Aiptimize

Settings → Inverter → type **SAJ H1 (Modbus TCP)**.

| Field | Value |
|---|---|
| Host | inverter or dongle IP |
| Port | `502` |
| Unit id | `1` (the H1's fixed station address) |
| Serial | optional; read from the inverter |

What it does:

- **Reads** PV per string and total, grid, battery power, load, SOC and the daily energy counters, normalised so grid is positive on import and battery positive on charge (the inverter reports both the other way round).
- **Controls** through *passive mode*: app mode `3`, then a charge or discharge power, then the direction. The active plan slot is applied live, re-sent every ten minutes.
- **No slot table.** The H1 has no weekly schedule to write, so `WriteSchedule` reports that explicitly instead of pretending to apply a plan.
- Writes pass the two gates: `AIPTIMIZE_ENABLE_WRITES=auto` and the UI arm switch.

## Registers that matter

| Purpose | Register |
|---|---|
| App mode (0 self use, 1 time of use, 2 backup, 3 passive) | `0x3247` |
| Passive mode (0 standby, 1 discharge, 2 charge) | `0x3636` |
| Passive charge / discharge power | `0x3637` / `0x3638` |
| Grid charge / feed limits | `0x3248` / `0x3249` |
| SOC backup / high / low | `0x3271` / `0x3273` / `0x3274` |
| Working mode, SOC, battery and PV power | `0x4004`, `0x406F`, `0x40A6`, `0x40A5` |
| Identity (serial, firmware) | `0x8F03`, `0x8F18` |

Note the inverter **swaps** the documented passive direction values: `1` discharges and `2` charges.

## Limitations

- **Live-only control.** Nothing survives an Aiptimize outage beyond the current command; there is no schedule in the inverter to fall back on.
- **Single client.** Two pollers will time out against each other.
- **No time-of-use programming** — the H1 does have TOU periods, but the layout is not extracted yet.
- **Firmware differences** in the SOC register: some builds report whole percent instead of ×0.01; the adapter detects this.
- Untested on hardware, hence the in-app warning.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| Timeouts on every poll | Another Modbus client connected | Close HA, the installer app or a previous session |
| Undefined register errors | Firmware lacks a block | Report it; the adapter reads optional blocks individually |
| Battery does not follow the plan | Write gate closed | Check `AIPTIMIZE_ENABLE_WRITES` and the UI arm switch |
| SOC reads as 0 or 100 always | Register scale differs on that firmware | Report the raw value from `/api/v1/status` |
