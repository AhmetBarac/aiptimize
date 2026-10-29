# HTTP API

Base path: `/api/v1`. Responses are JSON; timestamps are RFC3339 UTC. Auth uses
the HttpOnly `aiptimize_session` cookie. State-changing requests additionally
require the double-submit CSRF header.

## CSRF

Every response sets `aiptimize_csrf` when missing. Read that cookie and send it
back as `X-CSRF-Token` on `POST`, `PUT`, `PATCH` and `DELETE`:

```sh
curl -c cookies.txt http://localhost:8080/api/v1/meta
TOKEN=$(grep aiptimize_csrf cookies.txt | awk '{print $7}')
curl -b cookies.txt -H "X-CSRF-Token: $TOKEN" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' http://localhost:8080/api/v1/auth/login
```

An `Origin` header, when present, must match the request host.

## Errors

```json
{
  "error": {
    "code": "validation_failed",
    "message": "site.timezone: unknown IANA timezone",
    "details": { "section": "site", "field": "timezone" }
  }
}
```

| Status | Codes |
|---|---|
| 400 | `invalid_request` |
| 401 | `unauthorized`, `invalid_credentials` |
| 403 | `csrf_failed`, `origin_mismatch`, `setup_required` |
| 404 | `not_found` |
| 409 | `already_initialized` (and later revision/arming conflicts) |
| 422 | `validation_failed` |
| 429 | `rate_limited` (login: 5 attempts/minute/IP) |
| 5xx | `internal_error` |

## Endpoints implemented in phase 0

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/healthz`, `/readyz` | no | Liveness / readiness |
| GET | `/api/v1/healthz`, `/readyz` | no | Same under the API prefix |
| GET | `/api/v1/meta` | no | Version, timezone, setup state, gates, features |
| POST | `/api/v1/setup` | no | Create the only admin user (first run) and log in |
| POST | `/api/v1/auth/login` | no | Log in |
| POST | `/api/v1/auth/logout` | yes | Revoke the session |
| GET | `/api/v1/auth/me` | session | Current user or `401` |
| GET | `/api/v1/status` | yes | Snapshot used by the dashboard |
| GET | `/api/v1/settings` | yes | All settings sections |
| PATCH | `/api/v1/settings` | yes | Patch one section |
| GET | `/api/v1/events` | yes | Audit/warning feed (`?since=RFC3339`) |
| GET | `/api/v1/stream` | yes | SSE: `status`, `settings`, later `plan`/`event` |

### Setup

```json
POST /api/v1/setup
{
  "username": "admin",
  "password": "at-least-8-characters",
  "site": {
    "name": "Home",
    "timezone": "Europe/Stockholm",
    "latitude": 59.3293,
    "longitude": 18.0686
  }
}
```

Returns `201` and sets the session cookie. Once a user exists it returns `409
already_initialized`. If `AIPTIMIZE_ADMIN_PASSWORD` is set, the user is seeded at
startup and this endpoint is closed.

### Settings patch

```json
PATCH /api/v1/settings
{
  "section": "site",
  "values": { "max_import_power_w": 17250 }
}
```

Unknown fields, wrong types and out-of-range values are rejected without
changing anything. The response contains the merged section:

```json
{ "section": "site", "values": { "name": "Home", "timezone": "Europe/Stockholm", … } }
```

### Status

`GET /api/v1/status` returns `version`, `now`, `uptime_s`, `site`, `gates`,
`telemetry` (starts `{available:false, stale:true}`), `prices`, `plan`, `ev`,
`schedule` and `checks`. Later phases fill these objects without changing the
envelope.

### SSE

`GET /api/v1/stream` emits named events with JSON data:

```text
event: status
data: {"version":"dev", ...}
```

A `status` event is sent every 15 seconds, an initial snapshot on connect, and
`: ping` heartbeats every 20 seconds. The browser reconnects with
`retry: 5000`.

## Planned endpoints

See §12 of [DEVELOPMENT_PLAN.MD](../DEVELOPMENT_PLAN.MD) for prices, plan
editing, schedule, charger and history endpoints. They are added in phases 1–7
and documented here as they land.
