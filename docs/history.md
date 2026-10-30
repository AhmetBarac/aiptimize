# History and release

Phase 7 adds measured-history aggregates, PWA metadata and release operations.

## History API

`GET /api/v1/history?from=&to=&resolution=day|week` returns measured energy and
cost over at most 92 days. `from`/`to` accept `YYYY-MM-DD` in the site timezone
or RFC3339. Without dates it returns the last 14 local days.

Each bucket contains:

- PV, house load and EV energy (kWh);
- grid import/export kWh;
- battery charge/discharge kWh;
- the latest plan's battery energy mapped into the bucket;
- actual cost, no-battery baseline cost and savings;
- period label, start and end.

Energy is integrated from consecutive telemetry samples with a five-minute gap
cap. Cost uses the stored 30-minute tariff block containing each sample. The
no-battery baseline uses `load + EV - PV` with the same prices, so savings are
comparable to the planner's summary. Missing prices or telemetry produce
warnings rather than a hard failure.

## History UI

`/history` offers `7 days`, `30 days` and `12 weeks` ranges (daily or weekly
aggregation) with summary cards for PV, consumption, grid import and savings,
the self-powered share, an energy/cost chart and a collapsible breakdown table.
The page is reachable from the mobile More sheet and the desktop sidebar and
works at 360 px.

## PWA

- `manifest.webmanifest` with standalone display, theme colour and 192/512 px
  icons (including a maskable icon).
- 32 px favicon and 180 px Apple touch icon.
- No service worker in v1, deliberately: stale control data is worse than a
  network error for an energy controller.

## Operations

- `GET /api/v1/metrics` (authenticated) exposes uptime, telemetry/price/plan/EV
  availability and the latest plan savings/cost as Prometheus text.
- `GET /api/v1/settings/export` downloads all settings sections.
- `POST /api/v1/settings/import` validates and applies section objects
  transactionally per section; an invalid section returns `422` with the
  already-applied section list.
- `/readyz` checks the database and reports platform readiness; `/healthz` is
  the container liveness probe.
- Backups: stop the container or run a WAL checkpoint, then copy the volume;
  `docs/deployment.md` documents the exact commands.

## Release checklist

1. `gofmt -l cmd internal`, `go vet ./...`, `go test -race ./...`.
2. `npm test`, `npm run build`, Playwright at 390×844 and 1280×800.
3. Docker compose build/up; `/healthz` 200; non-root; data survives restart.
4. Check setup/login, dashboard, prices, plan editor, Car, History, settings.
5. Verify no secrets in logs or API responses, write/EV gates disarmed by default.
6. Tag `v1.0.0` after HIL sign-off.
