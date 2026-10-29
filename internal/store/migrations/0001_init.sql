-- 0001_init.sql — initial schema for Aiptimize v1.
-- Timestamps are RFC3339 UTC text; local-day keys are YYYY-MM-DD in the site timezone.

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    created_at    TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    last_seen  TEXT    NOT NULL,
    ip         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS prices (
    area           TEXT    NOT NULL,
    currency       TEXT    NOT NULL,
    delivery_start TEXT    NOT NULL,
    delivery_end   TEXT    NOT NULL,
    price_mwh      REAL    NOT NULL,
    state          TEXT    NOT NULL DEFAULT 'Final',
    fetched_at     TEXT    NOT NULL,
    PRIMARY KEY (area, currency, delivery_start)
);
CREATE INDEX IF NOT EXISTS idx_prices_start ON prices(delivery_start);

CREATE TABLE IF NOT EXISTS plans (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at    TEXT    NOT NULL,
    horizon_start TEXT    NOT NULL,
    horizon_end   TEXT    NOT NULL,
    timezone      TEXT    NOT NULL,
    status        TEXT    NOT NULL DEFAULT 'suggestion',
    inputs_hash   TEXT    NOT NULL DEFAULT '',
    summary_json  TEXT    NOT NULL DEFAULT '{}',
    inputs_json   TEXT    NOT NULL DEFAULT '{}',
    origin        TEXT    NOT NULL DEFAULT 'auto',
    revision      INTEGER NOT NULL DEFAULT 1,
    locked_day    TEXT,
    note          TEXT    NOT NULL DEFAULT '',
    applied_at    TEXT,
    applied_by    TEXT NOT NULL DEFAULT '',
    parent_plan_id INTEGER REFERENCES plans(id)
);
CREATE INDEX IF NOT EXISTS idx_plans_created ON plans(created_at);
CREATE INDEX IF NOT EXISTS idx_plans_status ON plans(status);
CREATE INDEX IF NOT EXISTS idx_plans_locked_day ON plans(locked_day);

CREATE TABLE IF NOT EXISTS plan_slots (
    plan_id          INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    idx              INTEGER NOT NULL,
    day_local        TEXT    NOT NULL,
    start_utc        TEXT    NOT NULL,
    end_utc          TEXT    NOT NULL,
    direction        TEXT    NOT NULL,
    power_w          REAL    NOT NULL,
    energy_kwh       REAL    NOT NULL,
    avg_import_price REAL    NOT NULL DEFAULT 0,
    expected_saving  REAL    NOT NULL DEFAULT 0,
    reason           TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (plan_id, idx)
);
CREATE INDEX IF NOT EXISTS idx_plan_slots_plan ON plan_slots(plan_id);

CREATE TABLE IF NOT EXISTS plan_revisions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id      INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    revision     INTEGER NOT NULL,
    created_at   TEXT    NOT NULL,
    actor        TEXT    NOT NULL,
    action       TEXT    NOT NULL,
    slots_json   TEXT    NOT NULL DEFAULT '[]',
    summary_json TEXT    NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_plan_revisions_plan ON plan_revisions(plan_id);

CREATE TABLE IF NOT EXISTS schedule_writes (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_utc         TEXT    NOT NULL,
    kind           TEXT    NOT NULL,
    payload_json   TEXT    NOT NULL DEFAULT '{}',
    response       TEXT    NOT NULL DEFAULT '',
    readback_json  TEXT    NOT NULL DEFAULT '',
    ok             INTEGER NOT NULL DEFAULT 0,
    plan_id        INTEGER,
    plan_revision  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_schedule_writes_ts ON schedule_writes(ts_utc);

CREATE TABLE IF NOT EXISTS telemetry (
    ts_utc        TEXT PRIMARY KEY,
    soc           REAL NOT NULL,
    pv_w          REAL NOT NULL,
    pv_strings_json TEXT NOT NULL DEFAULT '[]',
    grid_w        REAL NOT NULL,
    battery_w     REAL NOT NULL,
    load_w        REAL NOT NULL,
    ev_w          REAL NOT NULL,
    inverter_mode INTEGER NOT NULL DEFAULT 0,
    extra_json    TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_telemetry_ts ON telemetry(ts_utc);

CREATE TABLE IF NOT EXISTS ev_configs (
    charger_id  TEXT PRIMARY KEY,
    config_json TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ev_plans (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    charger_id      TEXT    NOT NULL,
    kind            TEXT    NOT NULL DEFAULT 'static',
    weekdays_json   TEXT    NOT NULL DEFAULT '[]',
    deadline_local  TEXT    NOT NULL DEFAULT '',
    target_kind     TEXT    NOT NULL DEFAULT 'energy',
    target_energy_kwh REAL  NOT NULL DEFAULT 0,
    target_soc_pct  REAL    NOT NULL DEFAULT 0,
    precondition_min INTEGER NOT NULL DEFAULT 0,
    strategy        TEXT    NOT NULL DEFAULT 'cheapest',
    active          INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ev_plans_charger ON ev_plans(charger_id);

CREATE TABLE IF NOT EXISTS ev_plan_runs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id      INTEGER,
    computed_at  TEXT    NOT NULL,
    deadline_utc TEXT    NOT NULL,
    required_kwh REAL    NOT NULL,
    duration_min INTEGER NOT NULL,
    windows_json TEXT    NOT NULL DEFAULT '[]',
    status       TEXT    NOT NULL,
    warnings_json TEXT   NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_ev_plan_runs_plan ON ev_plan_runs(plan_id);

CREATE TABLE IF NOT EXISTS ev_sessions (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    charger_id       TEXT    NOT NULL,
    started_at       TEXT    NOT NULL,
    ended_at         TEXT,
    energy_kwh       REAL    NOT NULL DEFAULT 0,
    cost             REAL    NOT NULL DEFAULT 0,
    mode             TEXT    NOT NULL DEFAULT '',
    plan_id          INTEGER,
    target_kwh       REAL    NOT NULL DEFAULT 0,
    energy_offset_kwh REAL   NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ev_sessions_charger ON ev_sessions(charger_id);

CREATE TABLE IF NOT EXISTS ev_commands (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_utc      TEXT    NOT NULL,
    charger_id  TEXT    NOT NULL,
    command     TEXT    NOT NULL,
    params_json TEXT    NOT NULL DEFAULT '{}',
    result      TEXT    NOT NULL DEFAULT '',
    error       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ev_commands_ts ON ev_commands(ts_utc);

CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_utc      TEXT    NOT NULL,
    severity    TEXT    NOT NULL,
    code        TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    details_json TEXT   NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts_utc);
