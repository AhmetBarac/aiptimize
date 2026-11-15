# Inverter adapters

One page per inverter brand: what it supports, how to commission it, what Aiptimize reads and controls, and the sharp edges.

| Inverter | Status | Control style |
|---|---|---|
| [Solplanet](solplanet.md) | stable | weekly slot schedule written to the dongle |
| [Huawei SUN2000](huawei.md) | under development | forcible charge/discharge, renewed while the slot runs |
| [SAJ H1](saj.md) | under development | passive mode, renewed while the slot runs |
| Simulator | stable | built into the app, no hardware |

## What they share

- **Modbus TCP** for Huawei and SAJ: port **502**, or **6607** on Huawei plants that use it. The Huawei adapter tries both.
- **A unit id** to select the device: SAJ answers on **1**; Huawei on **0**, **100** (SDongle fast path) or **1** depending on the plant.
- **One client at a time.** The dongle or inverter serves a single Modbus connection; a second poller causes timeouts, not a clean error.
- **A write gate**: `AIPTIMIZE_ENABLE_WRITES=auto` opens once the inverter answers, and the UI arm switch is a separate, user-controlled block.
- **A live-following controller** for inverters without a slot table: the plan is applied slot by slot, re-sent every ten minutes, so a command with an expiry cannot lapse mid-slot. If Aiptimize stops, the command expires and the battery idles — a bounded stop rather than an unattended schedule.

The Solplanet dongle is different: it accepts a weekly slot schedule, so the plan survives a server outage. That is why it is the only adapter marked stable.
