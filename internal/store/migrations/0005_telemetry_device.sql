-- Record which inverter produced each sample, so replacing hardware leaves a
-- visible boundary in the history instead of silently merging two devices.
ALTER TABLE telemetry ADD COLUMN device_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_telemetry_device ON telemetry(device_id, ts_utc);