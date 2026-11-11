# Safety

Aiptimize controls a home battery and, later, an EV charger. This document
describes the safety model and what phase 0 actually does.

> **Phase 2 is read-only.** The Solplanet client only issues GET requests for
> identity, telemetry and the weekly schedule. The schedule writer and EV
> controller do not exist yet, and the simulator rejects write endpoints with
> `405`.

## Telemetry safety

- The poller keeps the last valid sample when the dongle is unreachable; it does
  not create or apply plans from stale data.
- Stale telemetry is flagged in `/status` and on the dashboard.
- Sign calibration (`inverter.sign_overrides`) changes interpretation only; it
  never changes hardware state.
- The connection test is a read-only diagnostic and is available without the
  write arm gate.

## Two independent gates

Hardware writes require **both**:

1. the environment gate, e.g. `AIPTIMIZE_ENABLE_WRITES=true`, and
2. the in-app arming switch in **Settings → Safety** (`safety.writes_armed_ui`).

The effective state shown by `/meta` and `/status` is
`environment && in-app`. EV control has a separate pair:
`AIPTIMIZE_ENABLE_EV_CONTROL` plus `safety.ev_control_armed_ui`.

Restarting with the environment gate off disarms writes regardless of the
stored in-app switch. The in-app switch is not a secret and does not bypass the
environment.

## Fail-safe defaults

- The resting state of the inverter is **self-consumption**: no schedule slot
  means the battery balances house load. An empty schedule is safe.
- The battery planner (phase 4) rejects plans below the reserve SOC and never
  schedules a discharge that crosses local midnight.
- A watchdog (phase 5) clears the schedule when telemetry or the plan goes stale
  (`safety.watchdog_clear_after_s`, default 1800 s).
- A foreign schedule written by the vendor app or another controller is detected
  and is **not** overwritten until the user chooses *Take control*.
- Manual edits are drafts until **Resubmit to inverter** is confirmed; the active
  slot is immutable.

## Planned hardware guard rails (phases 5–6)

| Guard | Behaviour |
|---|---|
| Write rate limit | At most one write per 60 s; no rewrite while a slot is active unless the plan changed |
| Readback | Every write is read back and stored in `schedule_writes`; a mismatch is a warning, not a failed write |
| Stale telemetry | No apply when the newest telemetry is older than 5 minutes |
| SOC reserve | Discharge slots are removed when measured SOC is at or below the reserve at apply/reconcile time |
| Site import limit | Battery + EV + house import is clamped to `site.max_import_power_w` |
| Grid charging | EV grid charging is opt-in and requires an explicit acknowledgment; v1 defaults to PV-only |
| Raw registers | The app never writes raw register `41104`; it only writes the weekly schedule and selects Custom mode |

## Commissioning checklist (before the first real write)

1. Read the Solplanet schedule reference (`solplanet-schedule.md` in the evcc
   research workspace) and the HIL checklist in §9 of the development plan.
2. Confirm read-only telemetry for at least 24 hours.
3. Confirm that no raw register writes occur.

Keep a way to release the battery (vendor app or dongle UI) available during
commissioning.

## Reporting a safety issue

Open an issue with the software version, the schedule payload (redact serial
numbers) and the observed inverter state. Do not include credentials.
