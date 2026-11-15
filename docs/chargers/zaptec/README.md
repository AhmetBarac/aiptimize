# Zaptec charger adapter

Status: **stable** — in production use.

Code: [`internal/ev/zaptec.go`](../../../internal/ev/zaptec.go) · Tests: [`internal/ev/zaptec_test.go`](../../../internal/ev/zaptec_test.go)

## Hardware

Any Zaptec charger reachable through [Zaptec's cloud](https://api.zaptec.com): Zaptec Pro, Zaptec Go and the Zaptec home range. Aiptimize talks to the Zaptec cloud, not to the charger, so **the installation needs internet** and the charger must be online in the Zaptec app.

Requirements:

- A Zaptec account with access to the installation (e-mail and password).
- The **installation id** and the **charger id** (GUIDs from the Zaptec portal).
- Charger firmware in normal mode: **eco mode blocks remote current control** (see Troubleshooting).

## Protocol

Everything is HTTPS against `https://api.zaptec.com`:

| Purpose | Request |
|---|---|
| Token | `POST /oauth/token` (form encoded, `grant_type=password`, `scope=openid`) |
| Chargers in account | `GET /api/chargers` |
| Live state | `GET /api/chargers/{id}/state` |
| Commands | `POST /api/chargers/{id}/sendCommand/{code}` |
| Current limit | `POST /api/installation/{installationId}/update` |

## Authentication

OAuth2 password grant. The token is cached and refreshed a minute before it expires; a `401` forces a new token. Credentials come from **Settings → EV** (stored server-side, never returned by the API).

## Data read

`GET /api/chargers/{id}/state` returns an observation list, which Aiptimize indexes by observation id:

| Our field | Observation | Meaning |
|---|---|---|
| `session_kwh` | `553` | energy in the current session |
| `power_w` | `513` | live charging power |
| `current_a` | `708` | actual current per phase |
| `phases` | `519` | phases in use (1–3, default 3) |
| `paused` | `718` | `FinalStopActive` — a final stop was issued |
| `connected` | `710` | `ChargerOperationMode`: `1` = disconnected, `2/3/5` = plugged in |

When the operation-mode observation is missing, the adapter falls back to "power or session energy present".

## Control

| Action | Request | Notes |
|---|---|---|
| Set current | `POST /api/installation/{id}/update` | **installation-wide**, not per charger |
| Pause | `sendCommand/506` | `StopChargingFinal` |
| Resume | `sendCommand/507` | `ResumeCharging` |
| Stop session | `sendCommand/506` | same command as pause |

## Configuration in Aiptimize

Settings → EV → provider **Zaptec**:

| Field | Meaning |
|---|---|
| `charger_id` | charger GUID; left empty, the account's first charger is used |
| `installation_id` | required for the current limit |
| `zaptec_username` / `zaptec_password` | account credentials |

## Capabilities

| | |
|---|---|
| Current range | 6–16 A |
| Phases | 3 |
| Current scope | **installation** (a limit affects every charger on it) |
| Pause | yes, a real pause that survives |
| Control latency | up to **15 minutes** (the cloud update rate limit) |

## Limitations and troubleshooting

- **Eco mode**: any current update returns `HTTP 500` with `{"Code":527,"Details":"Cannot update installation when using eco mode"}`. Aiptimize names this explicitly instead of reporting a generic failure. Switch the installation out of eco mode to control current.
- **Installation-wide current**: two chargers on one installation cannot have different limits.
- **Rate limit**: rapid successive updates are refused; the adapter surfaces the failure rather than retrying aggressively.
- **"Paused" vs "stopped"**: command 506 ends the session, so a paused car may need a physical re-plug depending on the vehicle.

## Tests

`internal/ev/zaptec_test.go` covers state decoding, the eco-mode error message and command dispatch against a fake Zaptec cloud.
