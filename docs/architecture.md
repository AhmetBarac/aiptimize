# Architecture

This document describes the running system; the roadmap for later phases is in
[DEVELOPMENT_PLAN.MD](../DEVELOPMENT_PLAN.MD).

## Shape of the deployment

```text
browser ──HTTP──► Go server ──► SQLite (/data/aiptimize.db)
  │                 │
  │                 ├── embedded Vue SPA (internal/webui)
  │                 └── SSE stream (/api/v1/stream)
  │
  └── (later phases) Nord Pool, Open-Meteo, Solplanet dongle, Zaptec cloud
```

One Go binary serves the API and the SPA. The SPA is built by Vite and embedded
with `//go:embed`; there is no separate web server and no CORS. SQLite uses the
CGO-free `modernc.org/sqlite` driver with WAL, `busy_timeout=5000` and foreign
keys enabled, and a single pooled connection.

## Implemented in phase 0

| Area | Implementation |
|---|---|
| Configuration | `internal/config`, environment only, strict validation |
| Persistence | `internal/store`, embedded migrations, full v1 schema in `0001_init.sql` |
| Settings | Typed sections (`site`, `inverter`, `pv`, `price`, `plan`, `load`, `ev`, `safety`), strict PATCH with unknown-field rejection, seeded defaults |
| Auth | Single admin, PBKDF2-SHA256 (600k iterations, random salt), server-side sessions (SHA-256 token hashes), 30-day idle expiry, HttpOnly/SameSite=Lax cookies |
| CSRF | Double-submit cookie plus `Origin` check on all state-changing API requests |
| API | `/api/v1` JSON, stable error envelope, request IDs, security headers, login rate limiting |
| Streaming | SSE broker, heartbeat and status events |
| SPA | Vue 3 + TypeScript + Vite, Bootstrap 5, ECharts, vue-router, vue-i18n (en/sv), mobile bottom tabs and desktop sidebar |
| Ops | Multi-stage Dockerfile, non-root uid 10001, healthcheck, compose with read-only root and `/data` volume |

## Phase 1 — prices

| Area | Implementation |
|---|---|
| Client | `internal/prices/client.go`, Nord Pool Data Portal, descriptive user agent, 1 req/s self-limit, typed HTTP errors, `ErrNotPublished` / `ErrNoData` |
| Poller | `internal/service/pricepoller.go`, today on startup and every 6 h, tomorrow from 13:02 with 15-minute retries until 23:00, preliminary re-publication every 6 h |
| Storage | `prices` table keyed by area/currency/delivery start, transactional day replacement, `fetched_at` and `state` retained |
| Aggregation | `internal/prices/aggregate.go`, UTC-instant grouping into 30-minute blocks, missing-block gaps, DST-safe 92/96/100 point days |
| Tariff | `(spot + grid fee + energy tax + markup) × (1 + VAT)`, export `spot − export fee`, negative prices preserved |
| API | `GET /prices`, `POST /prices/refresh`, price fields in `/status` and the SSE stream |
| UI | `/prices`: day tabs, ECharts 15-minute bars with a now marker, min/max/average, cheapest 2-hour windows, block table, tariff breakdown, attribution |
| Simulator | Deterministic price generator with `AIPTIMIZE_SIMULATOR=true` |

## Phase 2 — telemetry (read-only)

| Area | Implementation |
|---|---|
| Contract | `internal/inverter`: `Reader` (identity, read, schedule), normalized sample conventions |
| Solplanet | `internal/inverter/solplanet`: `getdev.cgi`, `getdevdata.cgi` devices 2/3/4 and `getdefine.cgi`; units per [solplanet.md](solplanet.md) |
| Codec | Schedule decode with day-key, marker, half-hour and duration validation; golden vectors |
| Simulator | `internal/inverter/simulator`: deterministic PV/load/battery model plus an HTTP fixture server; writes rejected |
| Poller | `internal/service/telemetry.go`: immediate poll, configurable 5–3600 s interval, reader rebuild on settings change, last-sample retention |
| Storage | `telemetry` table, 90-day retention, newest sample also kept in memory |
| API | telemetry block in `/status`, `POST /settings/test/inverter`, `telemetry` SSE events |
| UI | live energy-flow tiles, PV strings, SOC ring, freshness, inverter check; inverter settings and connection test |
| Safety | GET-only client and a write-rejecting simulator; no arm gate involved |

## Phase 3 — forecast

| Area | Implementation |
|---|---|
| PV client | `internal/forecast/pv`, Open-Meteo tilted irradiance per distinct tilt/azimuth, null-safe parsing, typed errors |
| PV model | Per-string kWp/PR/temperature model, 30-minute interpolation, string max and inverter AC clipping, deterministic simulator |
| Load | `internal/forecast/load`, configured 24-hour profile with 14-day weekday/hour median blending and EV exclusion |
| Service | `internal/service/forecast.go`, 30-minute refresh, in-memory 48-hour snapshot, three-hour zero-PV staleness, previous-snapshot fallback |
| Calibration | Seven-day forecast history vs measured PV, clamped 0.5–1.5, feature flag |
| API | `GET /forecast`, `POST /forecast/refresh`, `GET /solar`, forecast fields in `/status`, SSE `forecast` events |
| UI | dashboard 24/48-hour PV/load chart and today's solar sparkline; full-page solar production/forecast/load chart; Solar/Load settings |

## Phase 4 — battery planner

| Area | Implementation |
|---|---|
| Optimiser | `internal/planner`, pure Go DP over 30-minute blocks and 0.5 % SOC steps, slot-atomic transitions, deterministic tie-breaking |
| Constraints | 4 slots/day default, 30–240 min slots, half-hour starts, no midnight crossing, reserve SOC, site import limit, efficiency and cycle cost |
| Inputs | prices, PV at confidence, load, EV hook, fresh SOC, battery limits and plan settings; exact inputs stored with the plan |
| Policy | no-battery baseline, minimum daily saving, price-gap truncation, stale SOC/price blocking, stale forecast PV = 0 |
| Persistence | `plans` + `plan_slots`, curve/warnings in migration 0002, inputs hash |
| Service | startup + 5-minute automatic recompute, `POST /plan/recompute`, keeps the previous plan on failure |
| API | `GET /plan`, `POST /plan/recompute`, plan summary/slots in `/status`, SSE `plan` event |
| UI | plan summary, combined price/PV/load/battery/SOC chart, slot table, dry-run label, Plan settings |

## Phase 5 — plan editing and actuation

| Area | Implementation |
|---|---|
| Editing | manual drafts per local day, structural/SOC/site validation, cost preview, optimistic revisions |
| Locking | manual day lock; auto suggestions continue but never overwrite a draft/applied manual plan |
| Revisions | `plan_revisions` audit on every draft/apply; `schedule_writes` linked to plan/revision |
| Actuation | seven-day `setdefine` builder, simulator and Solplanet writer, readback and diff |
| Safety | writes armed (env + UI) and confirmation, active-slot protection, 60 s write rate limit, foreign-schedule refusal, clear/takeover |
| API | plan view, drafts/preview/apply/discard, auto apply, revisions, schedule read/clear/takeover |
| UI | day tabs, editable slot form, validation, draft/applied/locked/foreign states, resubmit/reset |

## Request lifecycle

1. `withRecovery` converts panics into `500 internal_error`.
2. `withRequestID` assigns/echoes `X-Request-ID`.
3. `withRequestLog` logs method, path, status, duration, IP and user.
4. `withSecurityHeaders` sets CSP, nosniff, frame and referrer policies.
5. `withCSRF` issues the `aiptimize_csrf` cookie; for POST/PUT/PATCH/DELETE
   under `/api/` it checks the `X-CSRF-Token` header and the request `Origin`.
6. `withAuth` resolves `aiptimize_session` into the request context, sliding the
   idle expiry at most once every five minutes.
7. Protected handlers require that context; setup/login are public but gated by
   the first-run state.

The root catch-all serves the SPA with history fallback; `/api/*` never falls
back to HTML.

## Data model

The complete v1 schema (prices, plans, plan slots and revisions, schedule
writes, telemetry, EV configs/plans/runs/sessions/commands, events) is created
by the first migration. Phases 0–5 actively use `users`, `sessions`, `settings`,
`events`, `prices`, `telemetry`, `plans`, `plan_slots`, `plan_revisions` and
`schedule_writes`; forecasts are cached in memory and the exact planner inputs
are stored with each plan. Timestamps are RFC3339 UTC text; local-day keys are
`YYYY-MM-DD` in the site timezone.

Retention (daily, from `RunBackground`): telemetry 90 days, events 1 year,
expired sessions removed. WAL is checkpointed on graceful shutdown.

## Settings

Each section is a typed Go struct stored as one JSON document. `PATCH
/api/v1/settings` merges a partial object, re-decodes with unknown-field
rejection and validates ranges; a rejected patch changes nothing and returns
`422 validation_failed` with `section` and `field`. Secrets are never part of
settings.

Gates are derived: `effective = environment gate && in-app arming switch`.
Phase 0 exposes them read-only through `/meta` and `/status`.

## Deliberate omissions after phase 5

EV charging, history and PWA work are phase 6–7. `/status` reports
`ev_charging` as `false`; the UI shows the phase in which it arrives. Real
hardware schedule writes remain HIL-gated and are not exercised by the test
suite.

## Phase roadmap

| Phase | Adds |
|---|---|
| 1 ✅ | Nord Pool client, cache, 15→30-minute aggregation, `/prices` UI |
| 2 ✅ | Solplanet client, codec, simulator, 30 s telemetry, energy-flow dashboard |
| 3 ✅ | Open-Meteo PV model per MPPT, load profile, forecast UI |
| 4 ✅ | Deterministic DP battery planner, plan API and chart |
| 5 ✅ | Plan drafts/validation/revisions, schedule actuation and GUI editor |
| 5 | Schedule writer, reconciler, watchdog, plan drafts/editing/resubmit |
| 6 | Charger interface, Zaptec cloud adapter, EV plans and controller |
| 7 | History, PWA, backups, Playwright, security review, v1.0 |
