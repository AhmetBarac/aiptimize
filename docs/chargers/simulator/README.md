# EV charger simulator

Status: **stable** — used for demos and tests.

Code: [`internal/ev/simulator.go`](../../../internal/ev/simulator.go)

## Purpose

A deterministic stand-in for a charger, so the whole EV path (status, current limit, pause/resume, session energy, plan preview) can be exercised without hardware and without an internet connection.

Selected when the app runs with the simulator flag (`AIPTIMIZE_SIMULATOR` / `--simulator`) or when the EV provider is set to `simulator`.

## Behaviour

- Starts **connected** (configurable) and reports realistic power from the configured phases × voltage × current.
- **Integrates session energy** from elapsed time, so a running session gains `kWh` at the rate implied by the current setting.
- `SetCurrent` clamps to the configured minimum and maximum.
- `Pause` stops the energy integration and reports `paused`; `Resume` continues the same session.
- Time is injected, so tests advance the clock instead of sleeping.

## Configuration

| Field | Meaning |
|---|---|
| `ID` | identifier reported by the adapter (default `sim-ev-1`) |
| `Phases` | phases used for the power calculation (default 3) |
| `VoltageV` | per-phase voltage (default 230) |
| `MinCurrent` / `MaxCurrent` | current clamp |
| `Connected` | whether a car is plugged in at start |
| `Now` | clock, injectable for tests |

## Limitations

- No failures are simulated: it never reports an error state, a dropped cloud link or a refused command.
- Energy accounting is linear — no charging curve, no taper near full.
- Not a load model: the simulated charger does not respond to the house battery or PV, it just draws the current it was told to.

## Tests

Covered indirectly through `internal/ev/ev_test.go` and the service-level EV tests, which use the simulator to drive charging sessions.
