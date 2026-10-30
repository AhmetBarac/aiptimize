package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// RecordPlanSync stores one plan/inverter comparison snapshot.
func (s *Store) RecordPlanSync(ctx context.Context, sync domain.PlanSync) (domain.PlanSync, error) {
	if sync.TS.IsZero() {
		sync.TS = time.Now().UTC()
	}
	diff, err := json.Marshal(sync.Diff)
	if err != nil {
		return sync, err
	}
	if diff == nil || string(diff) == "null" {
		diff = []byte(`[]`)
	}
	var planID any
	if sync.PlanID != nil {
		planID = *sync.PlanID
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO plan_sync(ts_utc, day, plan_id, plan_revision, origin, status, match, diff_json, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(sync.TS), sync.Day, planID, sync.PlanRevision, sync.Origin, sync.Status,
		sync.Match, string(diff), sync.Source)
	if err != nil {
		return sync, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return sync, err
	}
	sync.ID = id
	return sync, nil
}

// LatestPlanSync returns the newest saved comparison for a local day. An empty
// day returns the newest comparison overall.
func (s *Store) LatestPlanSync(ctx context.Context, day string) (domain.PlanSync, error) {
	var (
		sync              domain.PlanSync
		ts, origin, state string
		diff              string
		planID            sql.NullInt64
	)
	query := `
		SELECT id, ts_utc, day, plan_id, plan_revision, origin, status, match, diff_json, source
		FROM plan_sync`
	args := []any{}
	if day != "" {
		query += ` WHERE day = ?`
		args = append(args, day)
	}
	query += ` ORDER BY id DESC LIMIT 1`
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&sync.ID, &ts, &sync.Day, &planID, &sync.PlanRevision, &origin, &state,
		&sync.Match, &diff, &sync.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PlanSync{}, ErrNotFound
	}
	if err != nil {
		return domain.PlanSync{}, err
	}
	if sync.TS, err = parseTime(ts); err != nil {
		return domain.PlanSync{}, err
	}
	sync.Origin = domain.PlanOrigin(origin)
	sync.Status = domain.PlanStatus(state)
	if planID.Valid {
		id := planID.Int64
		sync.PlanID = &id
	}
	if diff != "" && diff != "null" {
		if err := json.Unmarshal([]byte(diff), &sync.Diff); err != nil {
			return domain.PlanSync{}, err
		}
	}
	if sync.Diff == nil {
		sync.Diff = []string{}
	}
	return sync, nil
}

// ListPlanSyncs returns saved comparisons for a day, newest first.
func (s *Store) ListPlanSyncs(ctx context.Context, day string, limit int) ([]domain.PlanSync, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	query := `
		SELECT id, ts_utc, day, plan_id, plan_revision, origin, status, match, diff_json, source
		FROM plan_sync`
	args := []any{}
	if day != "" {
		query += ` WHERE day = ?`
		args = append(args, day)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.PlanSync{}
	for rows.Next() {
		var (
			sync              domain.PlanSync
			ts, origin, state string
			diff              string
			planID            sql.NullInt64
		)
		if err := rows.Scan(&sync.ID, &ts, &sync.Day, &planID, &sync.PlanRevision, &origin,
			&state, &sync.Match, &diff, &sync.Source); err != nil {
			return nil, err
		}
		if sync.TS, err = parseTime(ts); err != nil {
			return nil, err
		}
		sync.Origin = domain.PlanOrigin(origin)
		sync.Status = domain.PlanStatus(state)
		if planID.Valid {
			id := planID.Int64
			sync.PlanID = &id
		}
		if diff != "" && diff != "null" {
			if err := json.Unmarshal([]byte(diff), &sync.Diff); err != nil {
				return nil, err
			}
		}
		if sync.Diff == nil {
			sync.Diff = []string{}
		}
		out = append(out, sync)
	}
	return out, rows.Err()
}
