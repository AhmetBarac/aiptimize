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
by the first migration. Phases 0–1 actively use `users`, `sessions`, `settings`,
`events` and `prices`. Timestamps are RFC3339 UTC text; local-day keys are
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

## Deliberate omissions after phase 1

Forecasts, telemetry, planning, actuation and EV control are stubs by design.
`/status` reports their availability as `false`; the UI shows the phase in which
each arrives. No code path writes to hardware.

## Phase roadmap

| Phase | Adds |
|---|---|
| 1 ✅ | Nord Pool client, cache, 15→30-minute aggregation, `/prices` UI |
| 2 | Solplanet client, codec, simulator, 30 s telemetry, energy-flow dashboard |
| 3 | Open-Meteo PV model per MPPT, load profile, forecast UI |
| 4 | Deterministic DP battery planner, plan API and chart |
| 5 | Schedule writer, reconciler, watchdog, plan drafts/editing/resubmit |
| 6 | Charger interface, Zaptec cloud adapter, EV plans and controller |
| 7 | History, PWA, backups, Playwright, security review, v1.0 |
