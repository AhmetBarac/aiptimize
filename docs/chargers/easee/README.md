# Easee charger adapter

Status: **under development** — not yet verified against a live installation. The app shows the same warning in Settings → EV.

Code: [`internal/ev/easee.go`](../../../internal/ev/easee.go) · Tests: [`internal/ev/easee_test.go`](../../../internal/ev/easee_test.go)

## Hardware

Easee chargers on a normal Easee account: **Easee Up**, Easee Home, Easee Charge. Aiptimize uses the **official Easee cloud API** — there is no supported local API on the charger — so the **installation needs internet**. The undocumented local web interface some community projects use is deliberately not used: it is firmware-dependent and can change without notice.

Requirements:

- A personal Easee account with **e-mail or phone + password**. Accounts created with Google SSO cannot obtain a token this way.
- The charger's **serial number** (`Charger ID`), or a single-charger account where it can be discovered.
- No schedule active in the Easee app, otherwise it fights Aiptimize's current limit.

## Protocol

HTTPS against `https://api.easee.com`:

| Purpose | Request |
|---|---|
| Login | `POST /api/accounts/login` `{userName, password}` |
| Refresh | `POST /api/accounts/refresh_token` `{accessToken, refreshToken}` |
| Chargers in account | `GET /api/chargers` |
| State | `GET /api/chargers/{serial}/state` (falls back to `/observations`) |
| Commands | `POST /api/chargers/{serial}/commands/{command}` |

## Authentication

OAuth2 bearer token, valid for **1 hour**. The adapter caches it 2 minutes short of expiry, refreshes with the refresh token, and falls back to a full login if the refresh fails. A `401` on any call triggers one re-login and a retry.

## Data read

The state endpoint returns observations; Aiptimize reads these ids:

| Our field | Observation | Meaning |
|---|---|---|
| `power_w` | `120` | total power, **kW** in the API, converted to W |
| `session_kwh` | `121` | session energy |
| `current_a` | `114` | current signalled to the car |
| `online` | `250` | connected to cloud |
| `connected` | `100` | pilot mode: `A` = no car, anything else = plugged in |
| `phases` | `38` | phase mode (1 or 3) |

Charging is derived from power > 50 W or pilot mode `D`/`E`; `E`/`F` are reported as an error state. Parsing accepts all three response shapes the API has used (`observations[]`, a bare array, or a flat id map).

## Control

| Action | Request | Notes |
|---|---|---|
| Set current | `POST /commands/set_dynamic_charger_current` `{amps, minutes}` | clamped 6–32 A, integer; `minutes: 0` = until changed |
| Pause | `POST /commands/stop_charging` | Easee has **no pause**, so pausing ends the session |
| Resume | `POST /commands/start_charging` | |
| Stop session | `POST /commands/stop_charging` | |

## Configuration in Aiptimize

Settings → EV → provider **Easee**:

| Field | Meaning |
|---|---|
| `easee_username` | Easee account e-mail or phone |
| `easee_password` | account password |
| `charger_id` | serial number; empty = the account's first charger |

## Capabilities

| | |
|---|---|
| Current range | 6–32 A |
| Phases | 3 |
| Current scope | **charger** (per charger, unlike Zaptec) |
| Pause | no — pause stops the session |
| Control latency | about 10 s |

## Limitations

- **No pause**: `Paused` in the UI means "session stopped by us", not a held session.
- **Volatile current limit**: the dynamic limit is cleared when the charger reboots or the cable is disconnected — good for safety, but Aiptimize has to set it again after either event.
- **Cloud only**: no control when the link is down; the charger then keeps whatever limit it had (or none).
- **Easee schedules conflict**: an active schedule in the Easee app can override the current limit.

## Tests

`internal/ev/easee_test.go` covers observation parsing, the idle state, current clamping, pause/resume mapping to stop/start, and re-authentication after token expiry, against a fake cloud.
