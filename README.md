# Aiptimize

Aiptimize is a self-hosted home-battery and EV energy scheduler for a
Solplanet ASW08kH-T2 inverter (AI-Dongle) and a Zaptec Go charger. It ingests
Nord Pool day-ahead prices, forecasts PV and house load, plans up to four
battery slots per local day, writes them to the inverter, and lets you edit and
resubmit the plan — all from a mobile-first web UI.

The full design and delivery plan live in
[DEVELOPMENT_PLAN.MD](DEVELOPMENT_PLAN.MD).

> **Status: phase 7 (release) complete — v1.0.** Prices, telemetry, forecasting,
> battery planning/editing, gated actuation, EV charging and measured history
> are implemented. PWA metadata, settings export/import and authenticated
> metrics are included. Real inverter/Zaptec writes remain HIL-gated; the demo
> runs on simulators with battery writes disarmed.
>
> This software controls mains equipment. Read [docs/safety.md](docs/safety.md)
> before enabling writes.

## Quick start

### Docker

Images are built on a workstation and published to Docker Hub; hosts only pull.

```sh
./deploy/build.ps1 -Push          # build linux/arm64 and push 0.12.041, dev, sha-<sha>
docker run -d --name aiptimize -p 8080:8080 \
  --env-file .env -v aiptimize-v1-data:/data \
  docker.io/ahmetbarac/aiptimize:dev
```

The application host runs that container from
`deploy/server/aiptimize.service` under systemd, and `aiptimize-update` pulls a
new build and restarts only when the image digest changed. See
[docs/deployment.md](docs/deployment.md).

Open <http://localhost:8080> and complete the first-run setup. If
`AIPTIMIZE_ADMIN_PASSWORD` is set, the admin user is seeded at startup and the
setup screen is skipped.

### Local development

Requirements: Go 1.27+, Node 24+.

```sh
cd web && npm ci && npm run build && cd ..
go run ./cmd/aiptimize serve
```

The server listens on `0.0.0.0:8080` and stores its database in `/data` by
default. Set `AIPTIMIZE_DATA_DIR=./data` for a local run:

```sh
AIPTIMIZE_DATA_DIR=./data go run ./cmd/aiptimize serve
```

For frontend development with hot reload:

```sh
cd web && npm run dev        # http://localhost:5173, proxies /api to :8080
```

### Tests

```sh
go vet ./...
go test -race ./...
cd web && npm test && npm run build
```

The Playwright mobile smoke test needs a running server with a **fresh** data
directory, because it creates the administrator account. Run it with the price
simulator so the run does not depend on Nord Pool availability:

```sh
AIPTIMIZE_SIMULATOR=true AIPTIMIZE_DATA_DIR=$(mktemp -d) AIPTIMIZE_LISTEN=127.0.0.1:8099 go run ./cmd/aiptimize serve
# in a second shell
cd web && AIPTIMIZE_E2E_URL=http://127.0.0.1:8099 npm run test:e2e
```

## Configuration

Everything is configured through `AIPTIMIZE_*` environment variables. See
[deploy/.env.example](deploy/.env.example) and [docs/deployment.md](docs/deployment.md).

Two independent gates protect hardware writes:

1. `AIPTIMIZE_ENABLE_WRITES=true` (environment), and
2. the in-app arming switch in **Settings → Safety**.

Both are required. The same pair exists separately for EV control
(`AIPTIMIZE_ENABLE_EV_CONTROL`). Without both, plans can still be computed and
previewed but nothing is written.

Set `AIPTIMIZE_SIMULATOR=true` to replace Nord Pool and (in later phases) the
hardware with deterministic in-process simulators — useful for demos and tests.

## Features

- Nord Pool day-ahead prices: 15-minute points, 30-minute blocks, DST-safe day
  handling, provisional/final states, negative prices.
- Configurable import/export tariff shown in the UI and stored with plans later.
- Cheapest contiguous 2-hour windows.
- Automatic poll schedule (startup, 13:02 for tomorrow with retries, 6-hour
  refresh) plus a manual refresh.
- Read-only Solplanet telemetry every 30 s: SOC, PV per MPPT, grid, battery and
  derived house load, with sign calibration and a connection test.
- 48-hour PV forecast per MPPT string from Open-Meteo tilted irradiance and a
  temperature model, with optional measured-PV calibration.
- Deterministic 30-minute battery planner: price/PV/load inputs, SOC and reserve
  constraints, slot budget, import limit, savings baseline and a plan.
- Manual plan drafts with structural/SOC/site validation, optimistic revisions,
  one-day locks, automatic suggestions, and gated resubmission to the inverter
  with readback and schedule diff.- Seven-day Solplanet schedule codec, simulator writes, foreign-schedule
  detection, clear and takeover paths.
- EV charging: simulator and Zaptec Go adapters, smart/now/off modes, energy
  plans with cheapest/continuous windows, preconditioning, PV-only gating and a
  gated controller with a 15-minute current cadence.
- EV load and grid windows feed the battery planner so battery discharge does
  not overlap EV grid charging.
- Daily/weekly history with PV, load, grid, battery, cost, baseline and savings;
  PWA manifest and icons; authenticated metrics; settings export/import.
- House-load forecast from a configurable profile, blended with 14 days of
  measured load when available.
- Solar overview card with today's sparkline and a full-page production chart
  showing measured output, expected/ min/max forecast and estimated house load.
- Mobile-first, dark-first GUI inspired by the EcoFlow app family: a live
  energy-flow diagram with a tap-through supply mix, today's solar/consumption/
  grid/savings widgets, a battery-plan timeline, forecast and price charts,
  large tabular values, five-slot navigation with a primary Plan action,
  light/dark theme, English and Swedish and SSE updates.
- Single admin account, sessions, CSRF protection, login rate limiting.
- Docker image with non-root user, healthcheck and a persistent `/data` volume.

## Repository layout

```text
cmd/aiptimize/       main, subcommands (serve, status, healthcheck, …)
internal/config/     environment loading and validation
internal/store/      SQLite, embedded migrations, repositories
internal/settings/   typed, validated settings sections
internal/auth/       PBKDF2-SHA256, session and CSRF tokens
internal/prices/     Nord Pool client, aggregation, tariffs, simulator
internal/inverter/   read-only inverter contract, Solplanet client, codec, simulator
internal/forecast/   Open-Meteo client, PV model, load profile
internal/planner/    deterministic battery DP optimiser
internal/ev/         charger adapters, EV planner and Zaptec cloud client
internal/service/    background loops (price, telemetry, forecast, planner)
internal/api/        HTTP API, auth/CSRF middleware, SSE, embedded SPA
internal/webui/      go:embed of the built Vue app
internal/domain/     shared value types
web/                 Vue 3 + TypeScript + Vite + Bootstrap + ECharts
testdata/nordpool/   synthetic provider-schema fixtures (96 and 100 points)
testdata/solplanet/  captured dongle payloads (sanitized) and schedule
deploy/              Dockerfile, compose.yaml, .env.example
docs/                architecture, prices, telemetry, solplanet, API, safety, deployment
```

## Documentation

- [Architecture](docs/architecture.md)
- [User interface](docs/ui.md)
- [Prices](docs/prices.md)
- [Forecast](docs/forecast.md)
- [Battery planning](docs/planning.md)
- [EV charging](docs/ev.md)
- [History and release](docs/history.md)
- [Security review](docs/security.md)
- [Telemetry](docs/telemetry.md)
- [Solplanet dongle](docs/solplanet.md)
- [HTTP API](docs/api.md)
- [Safety](docs/safety.md)
- [Deployment](docs/deployment.md)
- [Development plan](DEVELOPMENT_PLAN.MD)

## License

GPL-3.0. See [LICENSE](LICENSE).

Spot prices are fetched from the Nord Pool Data Portal. The endpoint is the
website backend, intended for personal, low-volume use; data is not
redistributed by this project. PV forecasts come from Open-Meteo (CC-BY 4.0).
