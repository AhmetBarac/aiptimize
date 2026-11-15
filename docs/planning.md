# Battery planning

Phase 4 adds a deterministic battery schedule suggestion. It is a dry run: no
schedule is written to the inverter until phase 5.

## Inputs

- 30-minute import/export price blocks (tariff applied) from now to local
  midnight + `plan.horizon_days` (1–2).
- PV and house-load forecast points. PV is multiplied by `pv.confidence`
  (default 0.7) for planning; the raw forecast remains in the curve.
- EV planned load (`ev_w`); it is zero until the EV planner lands in phase 6.
- Current SOC from telemetry. It must be at most 5 minutes old.
- Battery capacity, min/max SOC, reserve SOC, charge/discharge efficiency,
  cycle cost, configured setpoints and hardware limits.
- Site import limit, slot budget, maximum slot length, preserve/end-SOC policy
  and minimum daily saving.

The exact input object is stored in `plans.inputs_json` with every plan.

## Hard constraints

- At most `plan.max_slots_per_day` charge/discharge slots per local day, both
  directions sharing the budget.
- Slot length 1–8 blocks (30–240 minutes) and starts on a half-hour boundary.
- No overlap, no direction change inside a slot, no slot crossing local
  midnight.
- SOC within `[min_soc, max_soc]`; discharge never goes below
  `max(min_soc, reserve_soc)`.
- Charge power is clamped so `load + EV + charge` does not exceed
  `site.max_import_power_w`; PV reduces the site draw.
- Charge/discharge power is clamped to the configured plan setpoint and the
  inverter maximum.

## Objective

For each block:

```text
netW = loadW + EVW + chargeW - dischargeW - pvPlanW
cost = max(netW, 0) * 0.5/1000 * importPrice
     - max(-netW, 0) * 0.5/1000 * exportPrice
     + cycleCost * dischargeKWh
```

`savings = noBatteryCost - planCost`. A no-battery baseline uses the same PV
and load with battery power = 0.

## Dynamic program

The optimiser uses SOC steps of 0.5 %. A state is `(block, socStep,
usedSlotsToday)`; the value is the minimum cost from that state to the end.
Active slot direction is not part of the state because a slot is atomic: from
an idle state the solver either idles one block or starts a charge/discharge
slot of `d = 1..min(maxSlotBlocks, blocksUntilMidnight)` and jumps `d` blocks.
At local midnight `usedSlotsToday` resets to zero.

```text
solve(i, soc, used):
  if i == end: require soc >= min(endTarget, startSOC) when preserve
  best = idleCost + solve(i+1, soc, used)
  if used < budget:
    for d in 1..maxDuration:
      best = min(best, slotCost(i, d, charge) + 1e-6 + solve(i+d, soc', used+1))
      best = min(best, slotCost(i, d, discharge) + ...)
```

The tiny per-slot penalty makes ties prefer fewer slots; durations are tried in
ascending order, so equal-cost plans are deterministic. A 96-block horizon with
~171 SOC steps solves in a few milliseconds.

## Policy and fallbacks

- If savings are below `plan.min_daily_saving`, the stored plan is empty with a
  self-consumption reason.
- Missing SOC, stale SOC (>5 minutes) or no price blocks prevents a new plan;
  the previous plan is kept.
- A price gap truncates the horizon at the gap and adds a warning. If tomorrow
  is not published, the plan covers today only.
- A forecast older than 3 hours is planned with PV = 0 and a warning; a stale
  forecast is flagged.
- The automatic planner runs 8 seconds after startup and every 5 minutes.
  `POST /plan/recompute` recomputes immediately.

## Output

`domain.Plan` carries:

- horizon, status `suggestion`, origin `auto`, revision 1 and inputs hash;
- summary: cost, no-battery cost, savings, charge/discharge kWh, equivalent
  cycles, slot count, currency;
- slots: local day, start/end, direction, power, energy, average import price,
  expected saving and a short reason;
- per-block curve: price, raw and planning PV, load, EV, battery power, SOC and
  block cost;
- warnings and a no-plan reason.

## API

- `GET /api/v1/plan` — latest stored plan (404 until the first plan).
- `POST /api/v1/plan/recompute` — compute, store and return a suggestion.
  Errors: `409 soc_stale`, `503 price_stale`, `422 plan_infeasible`.
- `/api/v1/status` includes `plan.available`, summary, slots and the next
  upcoming slot.

There is no apply endpoint in phase 4. The UI labels the plan as a dry run.

## UI

The Plan screen shows a savings hero (plan and baseline cost), slots, cycles and
charge energy, a day timeline with charge/discharge lanes, a combined chart
(import price, PV, house load, battery bars, SOC) with shaded slot windows, a
responsive schedule table, editor and warnings. The home plan card shows the
same timeline and slot list, expected savings, the next slot and the write-gate
state. Settings → Plan edits the slot budget, maximum slot length, horizon,
minimum saving, setpoints, end SOC and reserve policy.

## Worked example

For a 10 kWh battery at 50 % SOC, low prices at night and high prices in the
morning, the optimiser charges during the cheap blocks, discharges during the
expensive blocks, and must end at or above `min(endTarget, startSOC)`. A
typical day produces four slots and several SEK of savings; the exact numbers
are in the golden tests and the stored plan.

## Editing, drafts and resubmission

A manual plan locks one local day (`locked_day`). The auto planner keeps
producing suggestions but never overwrites a draft or applied manual plan for
that day. The effective plan is the applied manual plan, otherwise the applied
auto plan, otherwise the latest auto suggestion. A draft is not effective until
resubmitted, so the view can return a pending draft together with the applied
plan that the inverter should still be running.

- `POST /api/v1/plan/drafts` validates and stores a draft. The request carries
  `date`, `revision`, `base_plan_id`, `slots` (`start`, `end`, `direction`,
  `power_w`) and an optional note.
- Structural validation covers the slot budget, 30–240-minute duration,
  `:00`/`:30` starts, overlap, local midnight, plan horizon, SOC/reserve,
  site import and end SOC. Invalid drafts return `422 draft_invalid` with the
  validation errors.
- Revisions are optimistic: `revision` and `base_plan_id` must match the plan
  that is currently editable for the day (draft, otherwise applied). A stale
  pair returns `409 revision_conflict`; regenerated automatic suggestions do not
  count as another editor. Every save/apply writes a `plan_revisions` audit row.
- Every comparison between the effective plan and the inverter schedule is saved
  as a `plan_sync` snapshot (time, plan/revision, match, diff, source) so the UI
  can show when the two last matched. Unchanged fresh snapshots are reused; the
  rows are pruned after 90 days.
- Active slots may be kept but not changed; a changed/removed active slot
  returns `409 active_slot`. Ended slots are rejected.
- `POST /plan/drafts/{id}/preview` revalidates without saving.
- `DELETE /plan/drafts/{id}` discards a draft.
- `POST /plan/drafts/{id}/apply` requires `confirm: true`, writes armed and a
  matching revision; it revalidates against current SOC/prices/forecast, builds
  the schedule, writes, reads back and marks the plan applied.
- `POST /plan/auto/apply` applies the automatic suggestion and clears the manual
  day lock.
- `POST /plan/download` writes the effective plan (applied manual otherwise
  automatic) to the inverter on demand; it keeps the manual lock and records a
  `download` audit row.
- `GET /plan/revisions?date=` returns the audit history.

## Schedule and inverter writes

The schedule builder emits the full seven-day `setdefine` payload. Only the
manual day and the adjacent automatic day are populated; all other days are
explicitly zeroed. Pin/Pout come from the plan settings. Slots are encoded with
the dongle's packed format and validated for duration, overlap and midnight.

Writes require the environment gate (`AIPTIMIZE_ENABLE_WRITES`, automatic by
 default once the inverter answers) **and** the UI arm switch, plus
`confirm: true`. A write is skipped when the inverter already matches. Writes
are rate-limited to one per 60 seconds. The readback is stored in
`schedule_writes` and shown in the UI; a mismatch is a warning, not a failed
write.

A foreign schedule (one this app did not write) blocks apply until the user
presses **Take control**. `POST /schedule/takeover` replaces it; `POST
/schedule/clear` writes an empty seven-day schedule. Both require the gates and
confirmation. Manual plans are marked superseded when the automatic plan is
applied.

## Safety

- No draft or edit touches the inverter until **Resubmit to inverter**.
- Active-slot protection, optimistic revisions and audit rows prevent silent
  overwrites.
- HIL verification of 210/240-minute slots, dongle write variants and real
  hardware sign conventions is still required before production use; tests in
  this phase use the simulator only.

## Tests

Planner preview tests, codec golden/round-trip tests, simulator setdefine
tests, draft/apply API tests, schedule diff/lock/revision assertions, UI mobile
smoke tests, and the full Go race suite.

