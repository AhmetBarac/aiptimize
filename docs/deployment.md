# Deployment

## Docker Compose

```sh
cp deploy/.env.example .env
# edit .env: set AIPTIMIZE_ADMIN_PASSWORD or use the setup screen,
# keep AIPTIMIZE_ENABLE_WRITES=false until commissioning.
docker compose up -d --build
```

The root `compose.yaml` includes `deploy/compose.yaml`. Both build from the
repository root. The container:

- runs as uid/gid `10001` (non-root);
- has a read-only root filesystem with a small `/tmp` tmpfs;
- stores all state in the named volume `aiptimize-v1-data` mounted at `/data`;
- exposes port `8080`;
- has a Docker healthcheck running `aiptimize healthcheck`;
- stops with a 30 s grace period so the WAL can be checkpointed.

The volume name is explicit (`aiptimize-v1-data`) so it cannot collide with a
leftover volume from another application in the same Docker project.

### Data

`/data/aiptimize.db` (plus WAL) holds users, sessions, settings and — in later
phases — prices, plans and telemetry. Back up by stopping the container and
copying the volume, or by running `sqlite3 /data/aiptimize.db ".backup …"`.
The binary checkpoints the WAL on graceful shutdown.

### Environment

| Variable | Default | Notes |
|---|---|---|
| `AIPTIMIZE_LISTEN` | `0.0.0.0:8080` | Listen address |
| `AIPTIMIZE_DATA_DIR` | `/data` | Database directory |
| `AIPTIMIZE_DATABASE_PATH` | `<data>/aiptimize.db` | Override the database file |
| `AIPTIMIZE_TZ` | `Europe/Stockholm` | Site timezone fallback (the setting wins) |
| `AIPTIMIZE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `AIPTIMIZE_LOG_FORMAT` | `json` | `json` or `text` |
| `AIPTIMIZE_ADMIN_USERNAME` | `admin` | Seeded user name |
| `AIPTIMIZE_ADMIN_PASSWORD` | — | If set and no user exists, seed the admin and close setup |
| `AIPTIMIZE_SESSION_SECRET` | — | Reserved; 32+ characters |
| `AIPTIMIZE_COOKIE_SECURE` | `auto` | `auto`, `always`, `never` |
| `AIPTIMIZE_TRUST_PROXY` | `false` | Trust `X-Forwarded-Proto`/`Host` |
| `AIPTIMIZE_ENABLE_WRITES` | `false` | Environment write gate |
| `AIPTIMIZE_ENABLE_EV_CONTROL` | `false` | Environment EV gate |
| `AIPTIMIZE_SIMULATOR` | `false` | Use the deterministic price and inverter simulators instead of the network/hardware |
| `AIPTIMIZE_NORDPOOL_BASE_URL` | Data Portal endpoint | Override only for a local fixture server |
| `AIPTIMIZE_OPENMETEO_BASE_URL` | Open-Meteo forecast endpoint | Override only for a local fixture server |
| `AIPTIMIZE_ZAPTEC_USERNAME` | — | Zaptec cloud service account |
| `AIPTIMIZE_ZAPTEC_PASSWORD` | — | Zaptec cloud password |
| `AIPTIMIZE_ZAPTEC_INSTALLATION_ID` | — | Zaptec installation id |

Secrets are read only from the environment. They are never returned by the API
and never written to the database.

## Reverse proxy and TLS

Terminate TLS at a proxy (Caddy, nginx, Traefik) and forward to port 8080. Set
`AIPTIMIZE_TRUST_PROXY=true` and `AIPTIMIZE_COOKIE_SECURE=always` when the proxy
terminates TLS. Example Caddyfile:

```caddy
aiptimize.example.net {
    reverse_proxy 127.0.0.1:8080
}
```

Example nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name aiptimize.example.net;
    # ssl_certificate …;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_buffering off;       # SSE
        proxy_read_timeout 3600s;  # SSE
    }
}
```

Without a proxy the app must be reachable over plain HTTP on the LAN; cookies
then omit `Secure` automatically (`AIPTIMIZE_COOKIE_SECURE=auto`).

## First boot

Complete setup on a trusted LAN. If `AIPTIMIZE_ADMIN_PASSWORD` is set, the admin
is created on first start and the setup endpoint returns `409`.

After setup, open **Settings → Inverter** and set the dongle host, optional
serial and sign calibration, then use **Test connection**. Telemetry starts on
the next poll (at most one interval, 30 s by default). Under **Settings →
Solar**, add the MPPT strings (name, kWp, tilt, compass azimuth) and save; the
forecast refreshes within the configured interval. **Settings → Load** sets the
daily house energy and measured-profile blending. **Settings → Plan** sets the
optimiser horizon, slot budget, setpoints and reserve policy. The planner
computes a suggestion automatically 8 s after startup and every 5 minutes.
Drafts can be edited and applied from the Plan screen. Applying requires
`AIPTIMIZE_ENABLE_WRITES=true` **and** the UI arm switch, so leave writes
disarmed until the HIL checklist is complete. Set
`AIPTIMIZE_SIMULATOR=true` to run without a dongle or internet weather.

## Upgrades

```sh
git pull
docker compose up -d --build
```

Migrations run automatically at startup and are idempotent. Back up `/data`
before major upgrades.

## Troubleshooting

- **`/healthz` fails** — check container logs; the data directory must be
  writable by uid 10001.
- **401 after login behind a proxy** — set `AIPTIMIZE_TRUST_PROXY=true` and
  terminate TLS at the proxy.
- **`docker compose` cannot find `.env`** — the compose file uses
  `required: false`; copy `deploy/.env.example` to `.env` for configuration.
- **Setup screen keeps appearing** — no user exists yet; complete setup or set
  `AIPTIMIZE_ADMIN_PASSWORD`.
