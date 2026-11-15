# Huawei SUN2000 + LUNA2000 register map (Modbus TCP)

Register map as implemented by the Python library `wlcrs/huawei-solar-lib`
(branch `develop`), read 2026-11-15 from
`src/huawei_solar/registers.py`, `register_definitions/*`, `register_values.py`,
`modbus_client.py`, `register_client.py`, `device/{sun2000,base,__init__}.py`
and the Home Assistant integration `wlcrs/huawei_solar` (`services.py`,
`number.py`, `switch.py`, `const.py`, wiki).

Everything below is what the library actually puts on the wire: register number
= Modbus PDU address (no 30001/40001 style offset), big-endian, **high word
first** for 32-bit values, all data in holding registers (FC03 read, FC06/FC16
write). Addresses are given as Huawei register number and hex.

## Using it in Aiptimize

Settings → Inverter → type **Huawei SUN2000 (Modbus TCP)**.

| Field | Value |
|---|---|
| Host | the SDongle's IP address; give it a DHCP reservation |
| Port | **502**, or **6607** on some firmware and plant setups |
| Unit id | **0** by default; **100** is the SDongle fast path; some plants answer on 1 |
| Serial | optional — the adapter reads it from the inverter |
| Sign overrides | use the grid override if import and export appear swapped (see below) |

The adapter tries the configured port first and **falls back to the other one**
of 502/6607 when the connection is refused, then keeps using whichever answered.
A read failure caused by an unreachable port is retried once on the alternative
instead of failing the poll.

What Aiptimize does with the inverter:

- **Reads** every telemetry interval: PV per string, DC input power, AC output,
  meter power, SOC, battery charge/discharge power and the daily energy counters.
- **Controls** through a *forcible charge or discharge*: the active plan slot is
  written as `mode + power + duration`, and the command is re-sent every ten
  minutes so it cannot lapse while the slot runs. If Aiptimize stops, the command
  expires within an hour and the battery goes idle — a bounded, safe stop.
- **Does not** write a time-of-use schedule yet, and never writes the telemetry
  or battery-configuration ranges. See sections 4 and 5 for the registers that
  would be involved if that changes.
- Everything passes the two write gates (`AIPTIMIZE_ENABLE_WRITES=auto` plus the
  UI arm switch).

## Getting the inverter ready

1. **Enable Modbus TCP for the dongle.** In the FusionSolar app: *Device →
   Communication → SDongle → Modbus TCP*, and make sure the inverter firmware is
   recent. Without this the port is closed and every poll fails.
2. **Give the dongle a fixed address** in your router. The host name is not
   resolved, only an IP literal works reliably.
3. **Allow only one Modbus client.** The SDongle serves a single connection at a
   time: close FusionSolar's own integrations, Home Assistant's Huawei
   integration, or anything else that polls it, or Aiptimize's polls will
   interleave and time out.
4. **For writes, log in as installer.** Writes need installer-level access; the
   default installer password is `00000a`. Without it the inverter answers with
   exception code `0x80`, "permission denied".
5. **Start read-only.** Leave the write gate on `auto` but keep the UI arm
   switch off until the first readings match FusionSolar, then arm.

## Ports and unit ids

- **Port 502** is the Modbus standard and the usual default.
- **Port 6607** appears on some firmware and plant configurations. If a poll
  fails with "connection refused", try 6607 before assuming the address is wrong;
  the adapter already does this for you.
- **Unit 0** is the library default, **100** the SDongle fast path, and some
  installations answer on **1**. The unit id goes in Settings; a wrong unit id
  produces timeouts rather than a clear error.

## Troubleshooting

| Symptom | Likely cause | What to do |
|---|---|---|
| Every poll times out | Modbus TCP not enabled, wrong IP, or another client is connected | Enable it for the dongle, fix the IP, stop the other client |
| Connection refused on 502 | Firmware uses 6607 | Set 6607, or let the fallback find it |
| Reads work, writes fail with `0x80` | Not logged in as installer | Log in with installer credentials in FusionSolar |
| Grid power shows the wrong sign | CT orientation differs per site | Flip the grid sign override in Settings |
| SOC reads as 0 | Invalid marker `0xFFFF` on that firmware | Check the battery is online in FusionSolar |
| Values differ from FusionSolar by small amounts | FusionSolar smooths and time-averages | Compare the same instant, not an average |
| First poll after start fails, later ones work | Dongle still opening the socket | Transient; the next tick recovers |

## 1. Transport and mechanics

| Item | Value |
|---|---|
| TCP port | 502 (library default), 6607 on some firmware/plant setups |
| Unit / slave id | library default `0`; SDongle detection default `100`; HA scans `0, 100, 1..16` |
| Unit id for battery / power meter | none — LUNA2000 (`37xxx`, `38xxx`, `47xxx`) and DTSU666 (`371xx`) registers are served **by the inverter itself** at the inverter's unit id. No separate slave for battery or meter is used anywhere in the library |
| Sub-devices | only for EMMA / SmartLogger / SCharger / meter-behind-SmartLogger: `client.for_unit_id(n)` on the same socket |
| Reads | FC03, contiguous span ≤ 64 registers, gaps < 16 registers get padded (`x` bytes) and are read in one request |
| Writes | 1 register → FC06 (`write_single_register`); >1 register (all U32) → FC16 (`write_struct_format`) |
| Byte/word order | big-endian bytes, high word first (e.g. 37765 raw `0x0000_0BB8` = 3000 W) |
| Timeout | 15 s per request (SDongle is slow), 5 s for scanning |
| Cooldown | 50 ms between requests (`DEFAULT_COOLDOWN_TIME = 0.05`) |
| Reconnect | 5 consecutive timeouts force reconnect; exponential backoff 1–10 s, give up after 60 s |
| Heartbeat | write `0x0001` to **49999** every 15 s to hold the session (needed when logged in) |
| Login | FC 0x41 sub 0x24 (challenge) then sub 0x25 (user/pass, default `installer` / `00000a`); re-login after reconnect |
| Write probe | read+write-back of `TIME_ZONE` (43006) is used as the "may I write?" test |
| Polling in HA | 30 s telemetry, 15 min configuration |
| Concurrency | the device accepts **one** Modbus connection at a time |

## 2. Read registers

### 2.1 Identity

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Model name | 30000 / 0x7530 | 15 | string | — | — | ASCII, NUL-terminated (cut at first NUL), 30 bytes |
| Serial number | 30015 / 0x753F | 10 | string | — | — | 20 bytes |
| Part number (PN) | 30025 / 0x7549 | 10 | string | — | — |  |
| Firmware version | 30035 / 0x7553 | 15 | string | — | — | e.g. `V200R022C00SPC…` |
| Software version | 30050 / 0x7562 | 15 | string | — | — | SUN2000 + SDongle |
| Modbus protocol version | 30068 / 0x7574 | 2 | uint32 | 1 | — |  |
| Model ID | 30070 / 0x7576 | 1 | uint16 | 1 | — | numeric model code |
| Number of PV strings | 30071 / 0x7577 | 1 | uint16 | 1 | — | 1–24, drives PV register selection |
| Number of MPP trackers | 30072 / 0x7578 | 1 | uint16 | 1 | — |  |
| Rated power (Pn) | 30073 / 0x7579 | 2 | uint32 | 1 | W |  |
| P_MAX | 30075 / 0x757B | 2 | uint32 | 1 | W | use as clamp for feed-in limits |
| S_MAX | 30077 / 0x757D | 2 | uint32 | 1 | VA |  |
| P_MAX_REAL | 30083 / 0x7583 | 2 | uint32 | 1 | W |  |
| Feature mask 1/2/3/4 | 30211/30213/30215/30217 | 2 each | uint32 | — | bitfield | gate optional registers, see §6 |
| Feature mask | 30308 / 30342 | 1 | uint16 | — | bitfield | 30308 = off-grid switch, 30342 = ESS low power |
| System time | 40000 / 0x9C40 | 2 | uint32 | 1 | epoch s | device local time, not UTC |
| Start-up / shut-down time | 32091 / 32093 | 2 | uint32 | 1 | epoch s |  |

### 2.2 PV strings and DC input

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| PV string *n* voltage | 32016 + 2(n−1) / 0x7D10 + 2(n−1) | 1 | int16 | ×0.1 | V | *n* = 1…24, interleaved V/I |
| PV string *n* current | 32017 + 2(n−1) | 1 | int16 | ×0.01 | A | **gain 100, not 10** |
| Input power (total DC) | 32064 / 0x7D40 | 2 | int32 | 1 | W | sum of all strings |
| Total DC input energy | 32108 / 0x7D6C | 2 | uint32 | ×0.01 | kWh | energy, not power |
| Cumulative DC energy, MPPT *n* | 32212 + 2(n−1) / 0x7DD4 + 2(n−1) | 2 | uint32 | ×0.01 | kWh | *n* = 1…10 |
| Number of optimizers / online | 37200 / 37201 | 1 | uint16 | 1 | — |  |

Per-string power is **not** a register — compute `V × I`.

### 2.3 AC / grid (inverter side)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Grid voltage A-B (= phase A on 1-phase) | 32066 / 0x7D42 | 1 | uint16 | ×0.1 | V |  |
| Grid voltage B-C / C-A | 32067 / 32068 | 1 | uint16 | ×0.1 | V | 0 on 1-phase |
| Phase A / B / C voltage | 32069 / 32070 / 32071 | 1 | uint16 | ×0.1 | V |  |
| Phase A current | 32072 / 0x7D48 | 2 | int32 | ×0.001 | A | mA resolution |
| Phase B / C current | 32074 / 32076 | 2 | int32 | ×0.001 | A |  |
| Day peak active power | 32078 / 0x7D4E | 2 | int32 | 1 | W |  |
| Active power (AC output) | 32080 / 0x7D50 | 2 | int32 | 1 | W | signed; + = export to grid |
| Active power (fast) | 32095 / 0x7D5F | 2 | int32 | 1 | W | less filtered |
| Reactive power | 32082 / 0x7D52 | 2 | int32 | 1 | var | signed |
| Power factor | 32084 / 0x7D54 | 1 | int16 | ×0.001 | — |  |
| Grid frequency | 32085 / 0x7D55 | 1 | uint16 | ×0.01 | Hz |  |
| Efficiency | 32086 / 0x7D56 | 1 | uint16 | ×0.01 | % |  |
| Internal temperature | 32087 / 0x7D57 | 1 | int16 | ×0.1 | °C |  |
| Insulation resistance | 32088 / 0x7D58 | 1 | uint16 | ×0.001 | MΩ |  |
| **Device status** | **32089 / 0x7D59** | 1 | uint16 | 1 | — | enum, §3.1 |
| Fault code | 32090 / 0x7D5A | 1 | uint16 | 1 | — | 0 = none |
| State 1 / 2 / 3 | 32000 / 32002 / 32003 | 1 / 1 / 2 | bitfield | — | — | §3.2 |
| Alarm 1 / 2 / 3 | 32008 / 32009 / 32010 | 1 | bitfield | — | — | 0xFFFF is a *valid* alarm word |

### 2.4 Energy counters (inverter)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Total PV yield (AC) | 32106 / 0x7D6A | 2 | uint32 | ×0.01 | kWh | includes battery discharge, see §6 |
| Hourly yield | 32112 / 0x7D70 | 2 | uint32 | ×0.01 | kWh |  |
| Daily yield | 32114 / 0x7D72 | 2 | uint32 | ×0.01 | kWh |  |
| Monthly / yearly yield | 32116 / 32118 | 2 | uint32 | ×0.01 | kWh |  |
| Statistics timestamp | 32110 / 0x7D6E | 2 | uint32 | 1 | epoch s |  |

There is **no** daily grid import/export and **no** house-consumption counter on
the inverter; only the meter's cumulative pair (§2.5).

### 2.5 Power meter (DTSU666 / Smart Power Sensor)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Meter status | 37100 / 0x90EC | 1 | uint16 | 1 | — | 0 offline, 1 normal |
| Phase A / B / C voltage | 37101 / 37103 / 37105 | 2 | int32 | ×0.1 | V | int32, not uint16 |
| Phase A / B / C current | 37107 / 37109 / 37111 | 2 | int32 | ×0.01 | A |  |
| **Meter active power** | **37113 / 0x90F9** | 2 | int32 | 1 | W | signed, see §6 |
| Meter reactive power | 37115 / 0x90FB | 2 | int32 | 1 | var |  |
| Meter power factor | 37117 / 0x90FD | 1 | int16 | ×0.001 | — |  |
| Meter frequency | 37118 / 0x90FE | 1 | int16 | ×0.01 | Hz | signed int16 here |
| Meter energy "exported" | 37119 / 0x90FF | 2 | int32 | ×0.01 | kWh | library returns **abs()** |
| Meter energy "accumulated" | 37121 / 0x9101 | 2 | int32 | ×0.01 | kWh |  |
| Meter reactive energy | 37123 / 0x9103 | 2 | int32 | ×0.01 | kvarh |  |
| Meter type | 37125 / 0x9105 | 1 | uint16 | — | — | 0 = 1-phase, 1 = 3-phase |
| Line voltage A-B / B-C / C-A | 37126 / 37128 / 37130 | 2 | int32 | ×0.1 | V |  |
| Phase A / B / C active power | 37132 / 37134 / 37136 | 2 | int32 | 1 | W |  |
| Meter type check | 37138 / 0x9112 | 1 | uint16 | — | — | 0 recognizing, 1 match, 2 mismatch |

The whole 371xx block throws IllegalDataAddress / closes the socket when the
meter is offline (power outage); the library filters those registers out instead
of retrying.

### 2.6 Battery — aggregate view (all units)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Rated capacity | 37758 / 0x937E | 2 | uint32 | 1 | Wh |  |
| **State of charge** | **37760 / 0x9380** | 1 | uint16 | ×0.1 | % | 955 = 95.5 % |
| **Running status** | **37762 / 0x9382** | 1 | uint16 | — | — | enum, §3.3 |
| Bus voltage | 37763 / 0x9383 | 1 | uint16 | ×0.1 | V |  |
| Bus current | 37764 / 0x9384 | 1 | int16 | ×0.1 | A | signed |
| **Charge/discharge power** | **37765 / 0x9385** | 2 | int32 | 1 | W | **+ charging, − discharging** |
| Total charge / discharge | 37780 / 37782 | 2 | uint32 | ×0.01 | kWh | lifetime |
| Charge / discharge today | 37784 / 37786 | 2 | uint32 | ×0.01 | kWh |  |
| Product model | 47000 / 0xB798 | 1 | uint16 | — | — | 0 none, 1 LG RESU, 2 LUNA2000 |
| Unit 1 / 2 connect status | 37994 / 37995 | 1 | uint16 | — | — | enum |
| Heating status | 39354 / 0x99BA | 1 | uint16 | — | — |  |

### 2.7 Battery — storage unit 1 (LUNA2000)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Running status | 37000 / 0x9088 | 1 | uint16 | — | — | enum, §3.3 |
| Charge/discharge power | 37001 / 0x9089 | 2 | int32 | 1 | W | + charging |
| Bus voltage | 37003 / 0x908B | 1 | uint16 | ×0.1 | V |  |
| State of charge | 37004 / 0x908C | 1 | uint16 | ×0.1 | % |  |
| Working mode (read-back) | 37006 / 0x908E | 1 | uint16 | — | — | enum, §3.4 |
| Rated charge power | 37007 / 0x908F | 2 | uint32 | 1 | W | hardware limit |
| Rated discharge power | 37009 / 0x9091 | 2 | uint32 | 1 | W | hardware limit |
| Fault ID | 37014 / 0x9096 | 1 | uint16 | — | — |  |
| Charge / discharge today | 37015 / 37017 | 2 | uint32 | ×0.01 | kWh |  |
| Bus current | 37021 / 0x909D | 1 | int16 | ×0.1 | A |  |
| Battery temperature | 37022 / 0x909E | 1 | int16 | ×0.1 | °C |  |
| Remaining charge/discharge time | 37025 / 0x90A1 | 1 | uint16 | 1 | min |  |
| DCDC / BMS version | 37026 / 37036 | 10 | string | — | — |  |
| Max charge / discharge power | 37046 / 37048 | 2 | uint32 | 1 | W | read-only hardware rating; clamp writes to it |
| Serial number | 37052 / 0x90BC | 10 | string | — | — |  |
| Total charge / discharge | 37066 / 37068 | 2 | uint32 | ×0.01 | kWh |  |

Unit 2 mirrors this at 37700 (SN), 37738 (SOC), 37741 (status), 37743 (power),
37746/37748 (today), 37750/37751 (bus), 37752 (temp), 37753/37755 (total),
37799 / 37814 (software versions).

Battery packs (per pack, up to 4 per unit): SN at 38200 + 42·k (10 words),
firmware 38210 + 42·k (15), working status 38228 + 42·k, SOC 38229 + 42·k
(×0.1 %), power 38233 + 42·k (int32 W), voltage 38235 + 42·k (×0.1 V), current
38236 + 42·k (int16 ×0.1 A), total charge/discharge 38238/38240 + 42·k; pack 4 of
unit 1 lives at 39300…39338; max/min pack temperatures at 38452…38463 (int16
×0.1 °C); pack model names at 39003 + 15·k.

### 2.8 Battery — settings you also need to read back

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Working mode A | 47004 / 0xB79C | 1 | int16 | — | — | enum, §3.4 |
| Working mode settings (B) | 47086 / 0xB7EE | 1 | uint16 | — | — | RW, enum §3.5 |
| Max charging power | 47075 / 0xB7E3 | 2 | uint32 | 1 | W | RW |
| Max discharging power | 47077 / 0xB7E5 | 2 | uint32 | 1 | W | RW |
| Charging cutoff capacity | 47081 / 0xB7E9 | 1 | uint16 | ×0.1 | % | RW, "end of charge SOC" |
| Discharging cutoff capacity | 47082 / 0xB7EA | 1 | uint16 | ×0.1 | % | RW, "end of discharge SOC" |
| Forced charge/discharge period | 47083 / 0xB7EB | 1 | uint16 | 1 | min | RW |
| Forced charge/discharge power (read-back) | 47084 / 0xB7EC | 2 | int32 | 1 | W | **read-only in this library** |
| Charge from grid function | 47087 / 0xB7EF | 1 | uint16 | — | 0/1 | RW |
| Grid charge cutoff SOC | 47088 / 0xB7F0 | 1 | uint16 | ×0.1 | % | RW |
| Power of charge from grid | 47242 / 0xB88A | 2 | uint32 | 1 | W | RW |
| Max power of charge from grid | 47244 / 0xB88C | 2 | uint32 | 1 | W | RW |
| Forcible setting mode | 47246 / 0xB88E | 1 | uint16 | — | — | RW, 0 = time, 1 = SOC |
| Forcible charge power | 47247 / 0xB88F | 2 | uint32 | 1 | W | RW |
| Forcible discharge power | 47249 / 0xB891 | 2 | uint32 | 1 | W | RW |
| Forcible charge/discharge command | 47100 / 0xB7FC | 1 | uint16 | — | — | RW, 0 stop / 1 charge / 2 discharge |
| Forcible target SOC | 47101 / 0xB7FD | 1 | uint16 | ×0.1 | % | RW |
| Backup power SOC | 47102 / 0xB7FE | 1 | uint16 | ×0.1 | % | RW, backup reserve |
| Excess PV in TOU | 47299 / 0xB8C3 | 1 | uint16 | — | — | RW, 0 = to grid, 1 = charge |
| Storage power limit at grid-tied point | 47079 / 0xB7E7 | 2 | int32 | 1 | W | read-only |
| LCOE | 47069 / 0xB7DD | 2 | uint32 | ×0.001 | — |  |
| Remote charge/discharge control mode | 47589 / 0xB9E5 | 1 | int16 | — | — | RW |
| Plant max charge-from-grid power | 47590 / 0xB9E6 | 2 | uint32 | 1 | W | RW, needs fw feature |
| Max grid power during discharge | 47592 / 0xB9E8 | 1 | uint16 | 1 | W | RW |
| Adjustment deadband | 47593 / 0xB9E9 | 1 | uint16 | 1 | W | RW |
| ESS low power | 47842 / 0xBAE2 | 1 | uint16 | — | 0/1 | RW |
| Recharge signal / target SOC | 47780 / 0xBA9C, 47783 / 0xBAA7 | 1 | uint16 | ×0.1 | %, — | RW, maintenance recharge |

### 2.9 Active-power / grid-code control (writable)

| Quantity | Register | Words | Type | Scale | Unit | Notes |
|---|---|---|---|---|---|---|
| Active power control mode | 47415 / 0xB937 | 1 | uint16 | — | — | 0 unlimited, 1 DI, 5 zero-export, 6 W-limit, 7 %-limit |
| Max feed-in power (W) | 47416 / 0xB938 | 2 | int32 | 1 | W |  |
| Max feed-in power (%) | 47418 / 0xB93A | 1 | int16 | ×0.1 | % |  |
| Active power limitation mode | 47419 / 0xB93B | 1 | uint16 | — | — |  |
| Fail-safe threshold | 47424 / 0xB940 | 1 | uint16 | ×0.1 | % |  |
| Default max feed-in power | 47675 / 0xBA3B | 2 | int32 | 1 | W |  |
| Grid code | 42000 / 0xA410 | 1 | uint16 | — | — | lookup table in library |
| Grid output mode | 42001 / 0xA411 | 1 | uint16 | — | — | RW |
| Islanding protection | 42002 / 0xA412 | 1 | uint16 | — | 0/1 | RW |

## 3. Enumerations

### 3.1 Device status (32089)

| Value | Meaning |
|---|---|
| 0x0000 | Standby: initializing |
| 0x0001 | Standby: detecting insulation resistance |
| 0x0002 | Standby: detecting irradiation |
| 0x0003 | Standby: grid detecting |
| 0x0100 | Starting |
| 0x0200 | On-grid |
| 0x0201 | Grid connection: power limited |
| 0x0202 | Grid connection: self-derating |
| 0x0203 | Off-grid mode: running |
| 0x0300–0x0308 | Shutdown: fault / command / OVGR / comm. disconnected / power limited / manual start required / DC switches disconnected / rapid cutoff / input underpowered |
| 0x030C | Shutdown: ESS end-of-discharge |
| 0x0401–0x0405 | Grid scheduling: cosphi-P / Q-U / PF-U / dry contact / Q-P |
| 0x0500 / 0x0501 | Spot-check ready / spot-checking |
| 0x0600 | Inspecting |
| 0x0700 | AFCI self check |
| 0x0800 | I-V scanning |
| 0x0900 | DC input detection |
| 0x0A00 | Running: off-grid charging |
| 0xA000 | Standby: no irradiation |

### 3.2 State bitfields

| Reg | Bits |
|---|---|
| 32000 | 0 standby, 1 grid-connected, 2 grid-connected normally, 3 derating (power rationing), 4 derating (internal), 5 normal stop, 6 stop due to fault, 7 stop due to power rationing, 8 shutdown, 9 spot check |
| 32002 | bit0 locked(0)/unlocked(1), bit1 PV disconnected/connected, bit2 no/yes DSP data collection |
| 32003 | bit0 on-grid(0)/off-grid(1), bit1 off-grid switch disabled/enabled |

### 3.3 Storage status (37000, 37741, 37762)

| Value | Meaning |
|---|---|
| 0 | Offline |
| 1 | Standby |
| 2 | Running |
| 3 | Fault |
| 4 | Sleep mode |

### 3.4 Battery working mode — read-back values

`37006` (`StorageWorkingModesB`): 0 none, 1 forcible charge/discharge, 2 TOU
(LG RESU), 3 fixed charge/discharge, 4 maximise self-consumption, 5 fully fed to
grid, 6 TOU (LUNA2000), 7 remote max self-use, 8 remote full feed-in, 9 remote
TOU, 10 AI energy management, 11 remote AI control.

`47004` (`StorageWorkingModesA`, int16): 0 unlimited, 1 grid connection with zero
power, 2 grid connection with limited power.

### 3.5 Working mode to write — 47086 (`StorageWorkingModesC`)

| Value | Meaning |
|---|---|
| 0 | Adaptive |
| 1 | Fixed charge/discharge (uses 47200 periods) |
| 2 | Maximise self-consumption |
| 3 | TOU, LG RESU (uses 47028) |
| 4 | Fully fed to grid |
| 5 | TOU, LUNA2000 (uses 47255) |
| 6 | Third-party control |

### 3.6 Other enums used by the write path

| Register | Values |
|---|---|
| 47100 | 0 stop, 1 charge, 2 discharge |
| 47246 | 0 target = time, 1 target = SOC |
| 47299 | 0 excess PV to grid, 1 excess PV charges battery |
| 47415 | 0 unlimited, 1 DI scheduling, 5 zero power, 6 W-limited, 7 %-limited |
| 47000 | 0 none, 1 LG RESU, 2 LUNA2000 |
| 37100 | 0 offline, 1 normal |
| 37125 | 0 single-phase, 1 three-phase |
| 47842 / 47087 | 0 disabled, 1 enabled |

## 4. Write registers

All writes go to holding registers; FC06 for one-word values, FC16 for two-word
(U32) values. `bool` values are written as 0/1; scaled values are
`round(value × gain)` as an integer.

| Meaning | Register | Words | FC | Encoding | Notes |
|---|---|---|---|---|---|
| Max charging power | 47075 / 0xB7E3 | 2 | 16 | uint32 watts | clamp to 37046; feature bit `FeatureMask2.BATTERY_CHARGE_DISCHARGE_LIMITS` (30213 bit 28) |
| Max discharging power | 47077 / 0xB7E5 | 2 | 16 | uint32 watts | clamp to `37048` |
| Charge from grid enable | 47087 / 0xB7EF | 1 | 6 | 0 / 1 | must be 1 before 47242 has effect |
| Power limit when charging from grid | 47242 / 0xB88A | 2 | 16 | uint32 watts | ≤ 47244; gate `FeatureMask3.SECONDARY_GRID_TIED_CHARGE_POWER_LIMIT` (30215 bit 9) |
| Max power of charge from grid | 47244 / 0xB88C | 2 | 16 | uint32 watts | ceiling for 47242; the library marks it writable too |
| Grid-charge cutoff SOC | 47088 / 0xB7F0 | 1 | 6 | % ×10 | 200…1000 |
| End-of-charge SOC | 47081 / 0xB7E9 | 1 | 6 | % ×10 | 900…1000 |
| End-of-discharge SOC | 47082 / 0xB7EA | 1 | 6 | % ×10 | 120…200 |
| Battery working mode | 47086 / 0xB7EE | 1 | 6 | enum §3.5 |  |
| Forcible charge/discharge command | 47100 / 0xB7FC | 1 | 6 | 0 / 1 / 2 | **trigger — write last** |
| Forcible target SOC | 47101 / 0xB7FD | 1 | 6 | % ×10 | mode 47246 = 1 |
| Forcible duration | 47083 / 0xB7EB | 1 | 6 | minutes | mode 47246 = 0, 1…1440 |
| Forcible target mode | 47246 / 0xB88E | 1 | 6 | 0 time / 1 SOC |  |
| Forcible charge power | 47247 / 0xB88F | 2 | 16 | uint32 watts | ≤ 37046 |
| Forcible discharge power | 47249 / 0xB891 | 2 | 16 | uint32 watts | ≤ 37048 |
| Fixed charge/discharge periods | 47200 / 0xB860 | 41 | 16 | period struct §5.2 | mode 47086 = 1 |
| LUNA2000 TOU periods | 47255 / 0xB897 | 43 | 16 | period struct §5.1 | mode 47086 = 5 |
| LG RESU TOU price periods | 47028 / 0xB7B4 | 41 | 16 | period struct §5.3 | mode 47086 = 3 |
| Excess PV use in TOU | 47299 / 0xB8C3 | 1 | 6 | 0 / 1 |  |
| Capacity-control mode | 47954 / 0xBB52 | 1 | 6 | enum | gate `FeatureMask3.CAPACITY_CONTROL` (30215 bit 30) |
| Capacity-control peak-shaving SOC | 47955 / 0xBB53 | 1 | 6 | % ×10 |  |
| Capacity-control periods | 47956 / 0xBB54 | 64 | 16 | period struct §5.4 | needs `charge from grid` == 1 |
| Recharge target SOC | 47783 / 0xBAA7 | 1 | 6 | % ×10 | 400…600; then write 47780 = 1 |
| Recharge signal | 47780 / 0xBA9C | 1 | 6 | 1 | maintenance recharge |
| Backup power SOC | 47102 / 0xB7FE | 1 | 6 | % ×10 | changes reserve |
| Remote charge/discharge mode | 47589 / 0xB9E5 | 1 | 6 | int16 enum |  |
| Plant max grid charge power | 47590 / 0xB9E6 | 2 | 16 | uint32 watts | dongle-level limit |
| Max grid power during discharge | 47592 / 0xB9E8 | 1 | 6 | uint16 watts |  |
| Active power control mode | 47415 / 0xB937 | 1 | 6 | enum |  |
| Max feed-in power (W) | 47416 / 0xB938 | 2 | 16 | int32 watts |  |
| Max feed-in power (%) | 47418 / 0xB93A | 1 | 6 | % ×10 |  |
| Time zone | 43006 / 0xA7FE | 1 | 6 | int16 minutes | also used as the write-permission probe |
| Daylight saving time | 42900 / 0xA794 | 1 | 6 | 0 / 1 |  |
| Start / shut down | 40200 / 40201 | 1 | 6 | write 1 | write-only registers |
| Clear alarm | 42726 / 0xA6E6 | 1 | 6 | write 1 | write-only |
| Clear historical generation | 40239 / 0x9D2F | 1 | 6 | write 1 | write-only |
| Restore default settings | 43676 / 0xAA9C | 1 | 6 | write 1 | dangerous |
| Clear performance data | 43677 / 0xAA9D | 1 | 6 | write 1 | write-only |
| Heartbeat | 49999 / 0xC34F | 1 | 6 | write 1 | every 15 s, only value 1 |

`47084` (forced charge/discharge power) looks like the obvious write target but
is **read-only** in this library; the writable copies are 47247 / 47249.

## 5. Period block encodings

Time-of-use / fixed periods are byte-packed structs inside one large register
block. Word order is big-endian throughout. Day masks: bit0 = Sunday … bit6 =
Saturday.

### 5.1 LUNA2000 TOU — 47255, 43 registers (0xB897)

| Offset | Field |
|---|---|
| 47255 +0 | number of periods (0…14) |
| period *i* at 47256 + 3·i, +0 | start time, uint16, minutes since midnight (0…1440) |
| +1 | end time, uint16, minutes |
| +2 | **high byte = charge flag (0 charge, 1 discharge); low byte = weekday mask** |

Zero-filled slots end the list; `start_time >= end_time` and overlapping periods
(per weekday) are rejected by the library. The full block is
`0x0000_0000`-padded for unused periods.

### 5.2 Fixed charge/discharge — 47200, 41 registers (0xB860)

| Offset | Field |
|---|---|
| 47200 +0 | number of periods (0…10) |
| period *i* at 47201 + 4·i, +0 | start time, uint16, minutes |
| +1 | end time, uint16, minutes |
| +2…+3 | power, int32 watts (high word first, 2 registers) |

10 slots × 4 registers. Used with working mode 47086 = 1.

### 5.3 LG RESU TOU price — 47028, 41 registers (0xB7B4)

| Offset | Field |
|---|---|
| 47028 +0 | number of periods (0…10) |
| period *i* at 47029 + 4·i, +0 | start time, uint16, minutes |
| +1 | end time, uint16, minutes |
| +2…+3 | electricity price, int32, ÷1000 |

### 5.4 Capacity-control periods — 47956, 64 registers (0xBB54)

**Byte-packed with 9 bytes per period — period boundaries are not register
aligned.** Read/write the whole 128-byte block and unpack at byte offsets
`2 + 9·i`:

| Byte offset | Field |
|---|---|
| 0…1 | number of periods (0…14) |
| +0…1 | start time, uint16, minutes |
| +2…3 | end time, uint16, minutes |
| +4…7 | power, uint32 watts |
| +8 | weekday mask (1 byte) |

Validation: every weekday must be covered from 00:00 to 24:00 by adjacent
periods without gaps or overlap.

## 6. Caveats and traps

**Scaling**

- Only PV current uses ×0.01 A; AC/meter/battery currents use ×0.001, ×0.01 and
  ×0.1 respectively. Getting these three mixed up is the classic bug.
- All energy counters are ×0.01 kWh (gain 100), including the meter's.
- SOC and all cutoff SOCs are ×0.1 % — 100 % is raw 1000.
- `INPUT_POWER` (32064) is gain 1 in **watts**, although several public register
  maps list it as kW ×0.001.
- Power factor is ×0.001 (int16).

**Invalid markers**

| Type | "Invalid" raw value | Effect |
|---|---|---|
| uint16 | 0xFFFF | decoded as None |
| int16 | 0x7FFF | decoded as None |
| uint32 | 0xFFFFFFFF | decoded as None |
| int32 | 0x7FFFFFFF | decoded as None |

Check these before scaling, otherwise 0xFFFF becomes 6553.5 %. Exception:
alarm words 32009 / 32010 are read with `ignore_invalid=True`, so 0xFFFF there is
a valid set of alarm bits.

**Signedness**

- Signed: 32064, 32072/32074/32076, 32078, 32080, 32082, 32095, 32087, 37101…
  37123, 37132…37136, 37001, 37021, 37022, 37743, 37764, 37765, 47079, 47416,
  47675, 47589.
- Unsigned: voltages 32066-32071, 32085/32086/32088, 32106-32118, SOC/status/SOC
  cutoffs, 37007/37009/37046/37048, 37758/37780-37786, 47075/47077/47242-47249.

**Sign conventions**

- Battery power (37001, 37765): **positive = charging, negative = discharging**.
  Confirmed in the library by the SDongle aggregate comment ("positive is
  charging"). This matches Aiptimize's normalized `battery_w`.
- Inverter AC active power (32080): positive = export/output, negative = import.
- Meter active power (37113): signed, and **the observed polarity depends on CT
  orientation and firmware** — issue #54 "Grid Active Power and Grid Exported are
  inverted (negative)" shows both 37113 and 37119 reporting negative on some
  installations. The library only defends against this for 37119, by returning
  `abs()`. Treat 37113 as needing a per-site sign override; do not hard-code it
   as positive = import.
- 37119 / 37121: the library names 37119 `grid_exported_energy` (HA entity
  "Exported") and 37121 `grid_accumulated_energy` (HA entity "Consumption"), and
  a third-party map agrees. Both are cumulative and signed. Verify the pair
  against FusionSolar once during commissioning before wiring the energy
  dashboard.

**Behavioural limits**

- `INPUT_POWER` / `yield` counters are inverter **output**, so "yield" includes
  battery discharge and misses charge energy. FusionSolar derives PV input as
  `daily_yield − battery_day_discharge + battery_day_charge` (same for totals);
  that is wrong when charging from grid.
- There is no house-consumption or load-power register on a SUN2000. Compute it:
  `load_w = input_power − battery_w − grid_w` (with `battery_w` positive
  charging and `grid_w` positive import). Only EMMA (30356) and the SDongle
  (37500, kW ×0.001) expose a load-power register.
- No daily grid import/export counters on the inverter — only the meter's
  cumulative 37119/37121. EMMA has per-day values.
- One TCP connection only; a FusionSolar app / SCharger / Solarman connection
  kicks you off mid-poll. The library force-reconnects after 5 consecutive
  timeouts.
- The meter block disappears (IllegalDataAddress, socket close) when the meter is
  offline; night-time inverters can also refuse everything. The library filters
  the 371xx registers out rather than retrying the batch.
- SDongle: "Modbus TCP" must be enabled in the inverter/dongle communication
  settings; firmware from 2023 onwards; port is 502 on most units and 6607 on
  some firmwares/plants (can change after a firmware update). Unit id is usually
  0, some dongle firmwares answer on 1, and the library's own SDongle fast path
  probes 100.
- Writes need permission. Huawei answers with the **non-standard Modbus exception
  code 0x80** (permission denied) or IllegalDataAddress (0x02) on a write; the
  library maps both to `PermissionDeniedError`. Without installer login
  (`installer` / `00000a` via FC 0x41), writes are typically rejected.
- Registers guarded by feature bits (30213 bit 28, 30215 bit 9 / bit 30, 30342
  bit 4) must be probed first; reading them on unsupported firmware gives
  IllegalDataAddress.
- Never write: 30000-32999 (identity and live telemetry), 37000-39354 (battery
  and meter telemetry), 40000-49999 except the documented settings. The library
  enforces this with `writeable=False` and refuses the write locally.
- Multi-write atomicity does not exist: each `set()` is one Modbus request. A
  forcible charge is 3-4 separate writes, so a timeout in the middle leaves a
  half-applied configuration.

**Forcible charge/discharge sequencing (from the HA service code)**

1. write power (47247 charge / 47249 discharge), validated against 37046 / 37048;
2. write duration (47083) *or* target SOC (47101);
3. write target mode (47246) — 0 for duration, 1 for SOC;
4. write command 47100 last (1 charge, 2 discharge) as the trigger.

Stop: 47100 = 0, then 47249 = 0, then 47083 = 0, then 47246 = 0.

Charge and discharge are mutually exclusive by construction — a single command
register decides direction, and the library never writes 1 and 2 without an
intervening 0. The two power registers may both hold non-zero values; only the
command register selects which one is used. With 47087 (charge from grid) = 1 a
forcible charge also pulls from the grid up to 47242; with 0 it only diverts PV.

## 7. Mapping to Aiptimize's normalized telemetry

| Normalized | Huawei source |
|---|---|
| `pv_w` | 32064 (int32 W) |
| `pv_strings` | 32016+2n × 0.1 V, 32017+2n × 0.01 A |
| `grid_w` | 37113 (int32 W), positive = import, sign override per site |
| `battery_w` | 37765 (int32 W), positive = charging — no inversion needed |
| `load_w` | `pv_w − battery_w − grid_w` (no register) |
| `soc` | 37760 × 0.1 % |
| `battery_temp` | 37022 × 0.1 °C |
| `battery_voltage` | 37763 × 0.1 V |
| `inverter_temp` | 32087 × 0.1 °C |
| `today_pv_kwh` | 32114 × 0.01 kWh (AC yield, not PV input) |
| `total_pv_kwh` | 32106 × 0.01 kWh |
| `battery_charged_today` | 37784 × 0.01 kWh |
| `battery_discharged_today` | 37786 × 0.01 kWh |
| `grid_imported_total` | 37121 × 0.01 kWh (verify polarity) |
| `grid_exported_total` | 37119 × 0.01 kWh, library applies abs() |

Compared with the Solplanet adapter, Huawei needs no sign flip for battery power
and does need a configurable flip for the meter.

## 8. Open items to verify on hardware

1. Raw sign of 37113 and 37119/37121 against FusionSolar on the target install.
2. Whether the SDongle at the site answers on unit id 0 or 1 (probe both).
3. LUNA2000 TOU byte order in register `47256 + 3i + 2` (library reads charge
   flag in the high byte, weekday mask in the low byte).
4. Behaviour of `47083` when a forcible charge is interrupted by a reconnect.
