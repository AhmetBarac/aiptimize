-- Persist the last comparisons between the effective plan and the inverter
-- schedule so the UI can show when the two last matched and what differed.
CREATE TABLE IF NOT EXISTS plan_sync (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_utc TEXT NOT NULL,
    day TEXT NOT NULL,
    plan_id INTEGER,
    plan_revision INTEGER NOT NULL DEFAULT 0,
    origin TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    match INTEGER NOT NULL DEFAULT 0,
    diff_json TEXT NOT NULL DEFAULT '[]',
    source TEXT NOT NULL DEFAULT 'check'
);

CREATE INDEX IF NOT EXISTS idx_plan_sync_day ON plan_sync(day, id DESC);
