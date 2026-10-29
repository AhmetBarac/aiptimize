package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// GetSetting loads one settings section as raw JSON.
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

// SetSetting upserts one settings section.
func (s *Store) SetSetting(ctx context.Context, key string, value json.RawMessage) error {
	if !json.Valid(value) {
		return fmt.Errorf("setting %q: invalid JSON", key)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		key, string(value), formatTime(time.Now()))
	return err
}

// AllSettings returns every stored section.
func (s *Store) AllSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value_json FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]json.RawMessage)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// Event is an audit or warning entry.
type Event struct {
	ID       int64           `json:"id"`
	TS       time.Time       `json:"ts"`
	Severity string          `json:"severity"`
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Details  json.RawMessage `json:"details,omitempty"`
}

// InsertEvent appends an event.
func (s *Store) InsertEvent(ctx context.Context, ev Event) error {
	if ev.TS.IsZero() {
		ev.TS = time.Now()
	}
	details := ev.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(ts_utc, severity, code, message, details_json) VALUES (?, ?, ?, ?, ?)`,
		formatTime(ev.TS), ev.Severity, ev.Code, ev.Message, string(details))
	return err
}

// ListEvents returns events strictly after since (zero means from the start),
// newest first.
func (s *Store) ListEvents(ctx context.Context, since time.Time, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, ts_utc, severity, code, message, details_json FROM events`
	args := []any{}
	if !since.IsZero() {
		query += ` WHERE ts_utc > ?`
		args = append(args, formatTime(since))
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var ev Event
		var ts, details string
		if err := rows.Scan(&ev.ID, &ts, &ev.Severity, &ev.Code, &ev.Message, &details); err != nil {
			return nil, err
		}
		if ev.TS, err = parseTime(ts); err != nil {
			return nil, err
		}
		ev.Details = json.RawMessage(details)
		out = append(out, ev)
	}
	return out, rows.Err()
}
