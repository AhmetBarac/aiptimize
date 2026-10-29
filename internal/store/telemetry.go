package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// InsertTelemetry stores one sample. A repeated timestamp replaces the row.
func (s *Store) InsertTelemetry(ctx context.Context, t domain.Telemetry) error {
	if t.TS.IsZero() {
		t.TS = time.Now()
	}
	pvStrings, err := json.Marshal(t.PVStrings)
	if err != nil {
		return err
	}
	extra := t.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	extraJSON, err := json.Marshal(extra)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO telemetry(ts_utc, soc, pv_w, pv_strings_json, grid_w, battery_w, load_w, ev_w, inverter_mode, extra_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts_utc) DO UPDATE SET
			soc = excluded.soc, pv_w = excluded.pv_w, pv_strings_json = excluded.pv_strings_json,
			grid_w = excluded.grid_w, battery_w = excluded.battery_w, load_w = excluded.load_w,
			ev_w = excluded.ev_w, inverter_mode = excluded.inverter_mode, extra_json = excluded.extra_json`,
		formatTime(t.TS), t.SOC, t.PVW, string(pvStrings), t.GridW, t.BatteryW, t.LoadW,
		t.EVW, t.InverterMode, string(extraJSON))
	return err
}

// LatestTelemetry returns the newest stored sample.
func (s *Store) LatestTelemetry(ctx context.Context) (domain.Telemetry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ts_utc, soc, pv_w, pv_strings_json, grid_w, battery_w, load_w, ev_w, inverter_mode, extra_json
		FROM telemetry ORDER BY ts_utc DESC LIMIT 1`)
	t, err := scanTelemetry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Telemetry{}, ErrNotFound
	}
	return t, err
}

// TelemetryBetween returns samples in [from, to) ordered oldest first.
func (s *Store) TelemetryBetween(ctx context.Context, from, to time.Time, limit int) ([]domain.Telemetry, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts_utc, soc, pv_w, pv_strings_json, grid_w, battery_w, load_w, ev_w, inverter_mode, extra_json
		FROM telemetry WHERE ts_utc >= ? AND ts_utc < ?
		ORDER BY ts_utc LIMIT ?`,
		formatTime(from), formatTime(to), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Telemetry
	for rows.Next() {
		t, err := scanTelemetry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

// LoadSample is the minimal row needed for the measured load profile.
type LoadSample struct {
	TS    time.Time
	LoadW float64
	EVW   float64
}

// LoadSamplesBetween returns load samples in [from, to).
func (s *Store) LoadSamplesBetween(ctx context.Context, from, to time.Time) ([]LoadSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts_utc, load_w, ev_w FROM telemetry WHERE ts_utc >= ? AND ts_utc < ? ORDER BY ts_utc`,
		formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LoadSample
	for rows.Next() {
		var ts string
		var sample LoadSample
		if err := rows.Scan(&ts, &sample.LoadW, &sample.EVW); err != nil {
			return nil, err
		}
		t, err := parseTime(ts)
		if err != nil {
			return nil, err
		}
		sample.TS = t
		out = append(out, sample)
	}
	return out, rows.Err()
}

// PVSample is the minimal row needed for PV calibration.
type PVSample struct {
	TS  time.Time
	PVW float64
}

// PVSamplesBetween returns PV samples in [from, to).
func (s *Store) PVSamplesBetween(ctx context.Context, from, to time.Time) ([]PVSample, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts_utc, pv_w FROM telemetry WHERE ts_utc >= ? AND ts_utc < ? ORDER BY ts_utc`,
		formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PVSample
	for rows.Next() {
		var ts string
		var sample PVSample
		if err := rows.Scan(&ts, &sample.PVW); err != nil {
			return nil, err
		}
		t, err := parseTime(ts)
		if err != nil {
			return nil, err
		}
		sample.TS = t
		out = append(out, sample)
	}
	return out, rows.Err()
}

func scanTelemetry(row rowScanner) (domain.Telemetry, error) {
	var t domain.Telemetry
	var ts, pvStrings, extra string
	if err := row.Scan(&ts, &t.SOC, &t.PVW, &pvStrings, &t.GridW, &t.BatteryW, &t.LoadW, &t.EVW, &t.InverterMode, &extra); err != nil {
		return domain.Telemetry{}, err
	}
	parsed, err := parseTime(ts)
	if err != nil {
		return domain.Telemetry{}, err
	}
	t.TS = parsed
	if pvStrings != "" && pvStrings != "null" {
		if err := json.Unmarshal([]byte(pvStrings), &t.PVStrings); err != nil {
			return domain.Telemetry{}, err
		}
	}
	if extra != "" && extra != "null" && extra != "{}" {
		if err := json.Unmarshal([]byte(extra), &t.Extra); err != nil {
			return domain.Telemetry{}, err
		}
	}
	return t, nil
}
