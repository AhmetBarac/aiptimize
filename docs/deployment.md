# Deployment

## Container image

The application host does not build anything. Images are built on a workstation
with Docker Desktop and published to Docker Hub; the server only pulls.

```sh
# on the workstation, from the repository root
./deploy/build.ps1 -Push        # linux/arm64; pushes 0.12.041, dev and sha-<sha>
```

`deploy/build.ps1` reads the minor version from `VERSION` (`0.12`) and the
counter from `.buildnumber`, increments the counter, and tags the image three
ways:

| Tag | Meaning |
|---|---|
| `0.12.041` | immutable build |
| `dev` | moving tag the server follows |
| `sha-abc1234` | the commit that produced it, for rollback |

The binary reports `dev-0.12.041`: the code stays on the dev line, and every
image remains uniquely identifiable. Bump `VERSION` for a milestone; the build
number is automatic.

Builder stages run natively and cross-compile (`GOOS`/`GOARCH`), so a
`linux/arm64` image is produced on an amd64 workstation without QEMU.

The runtime container:

- runs as uid/gid `10001` (non-root);
- has a read-only root filesystem with a small `/tmp` tmpfs;
- stores all state in the named volume `aiptimize-v1-data` mounted at `/data`;
- exposes port `8080`;
- has a Docker healthcheck running `aiptimize healthcheck`;
- stops with a 30 s grace period so the WAL can be checkpointed.

The volume name is explicit (`aiptimize-v1-data`) so it cannot collide with a
leftover volume from another application.

## Server installation

Two files are the whole installation: `deploy/server/aiptimize.service` and
`deploy/server/aiptimize-update.sh`.

```sh
mkdir -p /opt/aiptimize
install -m 644 deploy/server/aiptimize.service /etc/systemd/system/aiptimize.service
install -m 755 deploy/server/aiptimize-update.sh /usr/local/bin/aiptimize-update
printf 'AIPTIMIZE_ENABLE_WRITES=false\n' > /opt/aiptimize/app.env
systemctl daemon-reload
systemctl enable --now aiptimize
```

The unit recreates the container from `docker.io/ahmetbarac/aiptimize:dev` on
every start and keeps the named volume, so restarts and upgrades take the same
path. Docker Compose is not required.

The registry repository is private. `aiptimize-update` logs in with the license
stored in **Settings → Site → License** (falling back to
`AIPTIMIZE_REGISTRY_TOKEN`), so the token is rotated in one place and the host's
`/root/.docker/config.json` follows automatically. A manual `docker login` is
only needed for ad-hoc pulls on a fresh host before the first start.

### Data

`/data/aiptimize.db` (plus WAL) holds users, sessions, settings, prices,
telemetry, plans, revisions, schedule writes and EV data. Back up by stopping
the service and copying the volume, or by running
`sqlite3 /data/aiptimize.db ".backup /data/backup.db"`. The binary checkpoints
the WAL on graceful shutdown. Settings can also be exported/imported as JSON
from Settings → System.

### Backup and restore

```sh
systemctl stop aiptimize
docker run --rm -v aiptimize-v1-data:/data -v "$PWD":/backup alpine \
  cp /data/aiptimize.db /backup/aiptimize-$(date +%F).db
systemctl start aiptimize
```

Restore by stopping the service, replacing `aiptimize.db` in the volume and
starting again. Migrations run on startup. Keep the database and the matching
`app.env` together; secrets are not included in settings export.

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
| `AIPTIMIZE_REGISTRY_REPO` | `docker.io/ahmetbarac/aiptimize` | Repository the update check queries |
| `AIPTIMIZE_REGISTRY_USERNAME` | — | Registry user (needed for a private repository) |
| `AIPTIMIZE_REGISTRY_TOKEN` | — | Fallback token when no license is stored in settings |
| `AIPTIMIZE_UPDATE_REQUEST_FILE` | `/var/lib/aiptimize/update-request` | Trigger file watched by systemd |
| `AIPTIMIZE_DISCOVERY` | `true` | Answer LAN discovery probes from the companion app |
| `AIPTIMIZE_DISCOVERY_PORT` | `8099` | UDP port for the discovery responder |

Secrets are read only from the environment. They are never returned by the API
and never written to the database.

## LAN discovery

The server answers a UDP broadcast probe so the companion app can find it
without an address:

```
→ 255.255.255.255:8099   {"aiptimize":"discover","v":1}
← unicast               {"aiptimize":"server","v":1,"instance_id":"…","name":"BaracHome",
                         "version":"dev-0.13.0","api":"http://192.168.0.154:8080","setup_required":false}
```

`GET /api/v1/instance` returns the same identity over HTTP and is what the app's
"test address" button validates. The instance id is generated once, stored in
the database and shown under **Settings → System**, so a server keeps its
identity when DHCP changes its address. Discovery is LAN-only; nothing is
exposed beyond the local broadcast domain.

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
disarmed until the HIL checklist is complete. EV control is separate:
`AIPTIMIZE_ENABLE_EV_CONTROL=true` plus the EV arm switch. The simulator is
used for demos; real Zaptec control additionally needs
`AIPTIMIZE_ZAPTEC_USERNAME`, `AIPTIMIZE_ZAPTEC_PASSWORD`,
`AIPTIMIZE_ZAPTEC_INSTALLATION_ID` and `AIPTIMIZE_ZAPTEC_CHARGER_ID`. Set
`AIPTIMIZE_SIMULATOR=true` to run without a dongle, charger or internet weather.

## Upgrades

```sh
aiptimize-update          # pull :dev, restart only when the digest changed
```

The script prints the health response and the running image. Because every build
is also tagged with its version and commit, a rollback is one command:

```sh
docker pull docker.io/ahmetbarac/aiptimize:sha-abc1234
docker tag docker.io/ahmetbarac/aiptimize:sha-abc1234 docker.io/ahmetbarac/aiptimize:dev
systemctl restart aiptimize
```

### In-app updates

**Settings → System → Updates** shows the running build and a *Check for
updates* button. The check reads the registry directly and compares the newest
`0.xx.yyy` tag with the running version; it also reads the image labels, so the
new build can describe itself before it is pulled:

| Label | Shown as |
|---|---|
| `org.opencontainers.image.version` | version |
| `org.opencontainers.image.revision` | commit |
| `org.opencontainers.image.created` | build date |
| `com.aiptimize.notes` | one-line build note (commit subject) |

`deploy/build.ps1` fills those from `VERSION`, `.buildnumber`, git and
`-Notes "..."` (default: the commit subject).

The registry credential is entered in the UI under **Settings → Site →
License**: a Docker Hub personal access token with read access to the
repository. It is stored with the other settings (SQLite) and never returned by
the API — the settings response only says whether one is configured.
`aiptimize-update` reads it to log in before pulling, so rotating the license is
enough; `AIPTIMIZE_REGISTRY_TOKEN` stays as a fallback for headless installs.

The container cannot replace itself, so *Update now* writes
`/var/lib/aiptimize/update-request`, which `aiptimize-update.path` watches; the
one-shot `aiptimize-update-trigger.service` runs `aiptimize-update` and the unit
restarts on the new image. The SPA polls `/api/v1/meta` and reloads when the
version changes. Without those two units the button is hidden — the API answers
`503 update_unavailable`.

For a host without systemd, the same check is available from the CLI:

```sh
docker exec aiptimize /app/aiptimize update-check
```

Migrations run automatically at startup and are idempotent. Back up `/data`
before major upgrades.

## Troubleshooting

- **`/healthz` fails** — `journalctl -u aiptimize -n 50`; the data directory
  must be writable by uid 10001.
- **401 after login behind a proxy** — set `AIPTIMIZE_TRUST_PROXY=true` and
  terminate TLS at the proxy.
- **`aiptimize-update` reports a digest but the app did not change** — the
  running container is pinned to `:dev`; check `docker inspect aiptimize`.
- **Setup screen keeps appearing** — no user exists yet; complete setup or set
  `AIPTIMIZE_ADMIN_PASSWORD` in `/opt/aiptimize/app.env`.
