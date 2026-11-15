# Wallbox charger adapter

Status: **under development** — not yet verified against a live installation. The app shows the same warning in Settings → EV.

Code: [`internal/ev/wallbox.go`](../../../internal/ev/wallbox.go) · Tests: [`internal/ev/wallbox_test.go`](../../../internal/ev/wallbox_test.go)

## Hardware

Wallbox chargers on a Wallbox account: **Pulsar Plus**, Pulsar Max, Copper SB, Commander 2. Aiptimize uses the **Wallbox cloud API**, so the **installation needs internet** and the charger must be online in the myWallbox app.

Requirements:

- A Wallbox account registered with **e-mail + password**. Accounts created via **Google SSO cannot obtain a token** and will fail at login.
- The charger id = its **serial number**.
- No schedule running in the myWallbox app.

## Protocol

HTTPS against `https://api.wall-box.com`:

| Purpose | Request |
|---|---|
| Token | `POST /auth/token/user` with `Authorization: Basic base64(email:password)` |
| Extended status | `GET /chargers/status/{id}` |
| Basic data / settings | `PUT /v2/charger/{id}` |
| Remote actions | `POST /v3/chargers/{id}/remote-action` |
| Eco-smart modes | `PUT /v4/chargers/{id}/eco-smart` (not used yet) |

## Authentication

Basic auth against the token endpoint, returning a **JWT valid for roughly 60 seconds**. The adapter caches it for 45 seconds and refreshes on expiry or on any `401` (one retry). This short lifetime is why every call goes through a single token helper rather than reusing a long-lived client.

## Data read

From `GET /chargers/status/{id}`:

| Our field | Source | Meaning |
|---|---|---|
| `power_w` | `charging_power` × 1000 | live power, kW in the API |
| `session_kwh` | `added_energy` | energy added in the session |
| `current_a` | `config_data.max_available_current` | current available to the car |
| `mode` / `connected` / `charging` | `status_id` | see the state table below |

State code mapping:

| Codes | Our state |
|---|---|
| 193, 194, 195, 196 | charging (196 is discharging) |
| 178, 182 | paused |
| 0, 163 | idle (car unplugged) |
| 14, 15 | error |
| 161–165, 177–189, 209, 210 | connected (ready, scheduled, waiting for car, power sharing, locked) |

`config_data.locked = 1` while connected is reported as `locked`.

## Control

| Action | Request | Notes |
|---|---|---|
| Set current | `PUT /v2/charger/{id}` `{maxChargingCurrent}` | clamped 6–32 A |
| Lock / unlock cable | `PUT /v2/charger/{id}` `{locked: 0|1}` | implemented, not yet exposed in the UI |
| Pause | `POST /v3/chargers/{id}/remote-action` `{action: 2}` | |
| Resume | `POST /v3/chargers/{id}/remote-action` `{action: 1}` | |
| Stop session | remote action 2 | pause is how a session is stopped remotely |

## Configuration in Aiptimize

Settings → EV → provider **Wallbox**:

| Field | Meaning |
|---|---|
| `wallbox_email` | Wallbox account e-mail |
| `wallbox_password` | account password |
| `charger_id` | charger serial number |

## Capabilities

| | |
|---|---|
| Current range | 6–32 A |
| Phases | 3 |
| Current scope | **charger** |
| Pause | **yes**, a real pause that survives |
| Control latency | about 15 s |

## Limitations

- **Google SSO accounts are unsupported** — the token endpoint needs a password.
- **One-minute tokens**: every burst of calls may require a new token; a slow network can make a control round-trip noticeably slower.
- **Cloud only**: no control while the charger is offline.
- **Local control** would require the community MQTT bridge; not implemented.

## Tests

`internal/ev/wallbox_test.go` covers the state mapping (charging, idle, paused), current clamping and the pause/resume remote actions against a fake Wallbox cloud.
