# Aiptimize

Aiptimize is a self-hosted home-battery and EV energy scheduler for a
Solplanet ASW08kH-T2 inverter (AI-Dongle) and a Zaptec Go charger. It ingests
Nord Pool day-ahead prices, forecasts PV and house load, plans up to four
battery slots per local day, writes them to the inverter, and lets you edit and
resubmit the plan — all from a mobile-first web UI.

The full design and delivery plan live in
[DEVELOPMENT_PLAN.MD](DEVELOPMENT_PLAN.MD).

> **Status: phase 0 (foundation) complete.** The server, database, settings,
> authentication and the mobile web shell are running and tested. Price
> ingestion, telemetry, forecasting, planning, actuation and EV control are
> added in phases 1–6. Nothing in this build writes to hardware.
>
> This software controls mains equipment. Read [docs/safety.md](docs/safety.md)
> before enabling writes.

## Quick start

### Docker (recommended)

```sh
cp deploy/.env.example .env       # optional; configure secrets and gates
docker compose up -d --build
```

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
directory, because it creates the administrator account:

```sh
AIPTIMIZE_DATA_DIR=$(mktemp -d) AIPTIMIZE_LISTEN=127.0.0.1:8099 go run ./cmd/aiptimize serve
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

## Repository layout

```text
cmd/aiptimize/       main, subcommands (serve, status, healthcheck, …)
internal/config/     environment loading and validation
internal/store/      SQLite, embedded migrations, repositories
internal/settings/   typed, validated settings sections
internal/auth/       PBKDF2-SHA256, session and CSRF tokens
internal/api/        HTTP API, auth/CSRF middleware, SSE, embedded SPA
internal/webui/      go:embed of the built Vue app
internal/domain/     shared value types
web/                 Vue 3 + TypeScript + Vite + Bootstrap + ECharts
deploy/              Dockerfile, compose.yaml, .env.example
docs/                architecture, API, safety, deployment, ADRs
```

## Documentation

- [Architecture](docs/architecture.md)
- [HTTP API](docs/api.md)
- [Safety](docs/safety.md)
- [Deployment](docs/deployment.md)
- [Development plan](DEVELOPMENT_PLAN.MD)

## License

GPL-3.0. See [LICENSE](LICENSE).

Spot prices are fetched from the Nord Pool Data Portal. The endpoint is the
website backend, intended for personal, low-volume use; data is not
redistributed by this project. PV forecasts come from Open-Meteo (CC-BY 4.0).
