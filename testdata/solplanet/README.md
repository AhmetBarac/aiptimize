# Solplanet fixtures

- `getdev_device2.json` — inverter identity. The serial and host are replaced
  with documentation values (`SIM…`, `192.0.2.10`).
- `getdev_device4.json` — battery configuration. Serial and module serials are
  replaced with documentation values; capacity, SOC limits and `mod_r` are kept
  from the verified capture.
- `getdevdata_device2.json`, `getdevdata_device3.json`,
  `getdevdata_device4.json` — live telemetry captured read-only from an
  ASW08kH-T2 + AI-Dongle (2026-07-27). They contain no serial numbers.
- `getdefine.json` — weekly schedule captured from the same installation.

The payload shapes and units are documented in `docs/solplanet.md`. The
telemetry captures are small factual measurements used for parser tests; no
credentials or identifiers are included.
