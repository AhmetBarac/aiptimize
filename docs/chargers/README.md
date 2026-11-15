# Charger adapters

One page per charger brand, describing the protocol Aiptimize speaks, what it reads, what it can control, how to configure it and where the sharp edges are.

| Charger | Status | Summary |
|---|---|---|
| [Zaptec](zaptec/README.md) | stable | Zaptec cloud, OAuth password grant, installation-wide current limit, real pause, 15 min control latency |
| [Easee](easee/README.md) | under development | Easee cloud, 1 h bearer token, per-charger volatile current limit, no pause |
| [Wallbox](wallbox/README.md) | under development | Wallbox cloud, 60 s JWT, per-charger current limit, real pause, no Google SSO |
| [Simulator](simulator/README.md) | stable | deterministic fake charger for demos and tests |

All adapters implement the same interface in [`internal/ev/ev.go`](../../internal/ev/ev.go) (`Status`, `SetCurrent`, `Pause`, `Resume`, optional `StopSession`), so the planner, the EV page and the API are provider-agnostic. Every command still passes the ARM gate: `AIPTIMIZE_ENABLE_WRITES` **and** the UI arming flag.

Background material lives in [docs/ev.md](../../ev.md); the inverter side is in [docs/solplanet.md](../../solplanet.md).
