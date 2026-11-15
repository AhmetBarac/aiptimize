# System requirements

What Aiptimize needs to run smoothly, and where the numbers come from. Figures
marked **measured** were taken from a production installation; the rest are
derived from those.

Aiptimize is a single Go binary with an embedded web UI and an embedded SQLite
database. There is no external database, no message broker, no cache server and
no GPU to provision.

## Reference installation (measured)

| | Value |
|---|---|
| Host | Debian 11, aarch64, **4 cores**, **1.9 GB RAM** (Raspberry Pi 4 class) |
| Container CPU | **1.4 %** average |
| Container memory | **12.6 MiB** |
| Image size | **24 MB** (compressed pull, `linux/arm64`) |
| Database | 30.8 MB for 11 748 samples over 4 days ≈ **7.7 MB/day** |
| Telemetry interval | 30 s |
| Uptime covered | continuous polling, prices, forecast, planner |

## Hardware

| Profile | CPU | RAM | Disk | Suitable for |
|---|---|---|---|---|
| **Minimum** | 1 vCPU | 512 MB | 4 GB free | one installation, telemetry and plans only |
| **Recommended** | 2 vCPU | 1 GB | 8 GB free, SSD | planning, forecasting, EV control, cloud chargers |
| **Comfortable** | 2–4 vCPU | 2 GB | 16 GB SSD | years of events, log headroom, update headroom |

- **Architecture**: `linux/amd64` and `linux/arm64` are both first class. Raspberry
  Pi 4 (2 GB) or newer, any x86 mini PC, NAS with Docker, or a small VPS.
- **CPU** is not the constraint: 1.4 % average on four cores leaves the planner and
  forecast a lot of room. One core is enough for a single installation.
- **RAM**: 12.6 MiB resident. The 512 MB minimum is not about steady state but
  about not swapping while an update pulls a new image, SQLite checkpoints and
  the planner runs at the same time.
- **Nothing else is required**: no Java, no Node, no Python, no database server.

## Storage

| | |
|---|---|
| Image | ~24 MB per tag; two tags on disk during an update |
| Database growth | **~7.7 MB/day** at a 30 s telemetry interval |
| Telemetry retention | **90 days** (pruned daily) |
| Event retention | **1 year** |
| Steady-state database | **~0.7 GB** |

Sizing rules:

- **4 GB free** is the practical floor, **8 GB** is comfortable.
- Reserve room for **two images** during an update: the new one is pulled before
  the container switches.
- **Use an SSD or a USB disk, not the SD card, as Docker's data root.** SQLite's
  WAL checkpoints every few minutes, which is exactly the write pattern that wears
  out cheap cards. Mount the data volume on the larger disk.
- Changing `telemetry_interval_s` scales storage linearly: 15 s doubles growth to
  ~1.4 GB steady state, 60 s halves it.

## Network

| Direction | Port | Purpose |
|---|---|---|
| Inbound | **8080/tcp** (configurable) | web UI and API; put a TLS reverse proxy in front for remote access |
| Inbound | **8099/udp** (optional) | discovery for the Android companion app |
| Outbound | **443/tcp** | Nord Pool prices, Open-Meteo forecast, Zaptec/Easee/Wallbox clouds, update checks, Docker Hub |
| LAN | **502/tcp**, **6607/tcp** | Modbus TCP to SAJ and Huawei inverters |
| LAN | **8484/tcp** (Solplanet default) | Solplanet AI-Dongle HTTP API |

- The inverter or dongle needs a **fixed address**: DHCP reservation, not mDNS.
- Internet is required for prices and forecasting and for every cloud charger. If
  it drops, the app keeps running with the last known prices and shows their age.
- **One Modbus client at a time**: the SAJ and Huawei adapters need exclusive
  access to port 502/6607. Home Assistant or an installer tool polling the same
  inverter causes timeouts, not a clean error.

## Software

| | Requirement |
|---|---|
| Host OS | Any Linux with Docker; Debian, Ubuntu, Raspberry Pi OS and most NAS distributions are known to work |
| Docker | **20.10 or newer** (uses `--env-file` and standard volume mounts) |
| Container runtime | 24 MB image, no privileged mode, no host networking required |
| Time zone data | Not needed in the image — Go bundles it. Set `TZ` only for readable log timestamps |
| systemd | Optional but recommended for the `aiptimize.service` unit and the `aiptimize-update` path unit |

## Clients

| Client | Requirement |
|---|---|
| Browser | Any current Chrome, Edge, Firefox or Safari; mobile-first layout, works from 320 px wide |
| PWA | Installable from Chrome or Safari ("Add to Home Screen"); needs HTTPS for full offline behaviour |
| Android companion | **Android 8.0 (API 26)** or newer; it is a WebView shell that auto-discovers servers on the LAN |
| iOS | Via PWA only; there is no native app |

## Power and availability

- A small **UPS** is worth it. SQLite survives a hard cut through its WAL, but the
  inverter can be left mid-slot with whatever command it last accepted.
- Inverters controlled through *live following* (SAJ, Huawei) need Aiptimize
  reachable: the command is refreshed every ten minutes and expires within an hour
  if the server stops. A Solplanet installation keeps running its written schedule
  without the server.

## Verifying your own installation

```sh
docker stats --no-stream aiptimize                     # CPU and memory
df -h /var/lib/docker                                  # disk headroom
du -sh /var/lib/docker/volumes/aiptimize-v1-data       # database size
```

A healthy installation sits near 1–3 % CPU, under 100 MB RSS, and grows the
database by single-digit megabytes per day.

## What is deliberately not required

- No PostgreSQL, Redis, RabbitMQ or any other service.
- No external object storage: the UI and its assets are compiled into the binary.
- No GPU, no JVM, no runtime to install — the container is self-contained.
- No multi-tenant database: one installation per instance, so adding a second home
  means a second container, not a bigger machine.
