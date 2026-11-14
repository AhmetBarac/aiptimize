package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/AhmetBarac/aiptimize/internal/domain"
)

// SaveEVPlan inserts or updates a charging plan.
func (s *Store) SaveEVPlan(ctx context.Context, plan domain.EVPlan) (domain.EVPlan, error) {
	now := time.Now()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = now
	}
	plan.UpdatedAt = now
	weekdays, err := json.Marshal(plan.Weekdays)
	if err != nil {
		return plan, err
	}
	if plan.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO ev_plans(charger_id, kind, weekdays_json, deadline_local, target_kind,
				target_energy_kwh, precondition_min, strategy, active, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'energy', ?, ?, ?, ?, ?, ?)`,
			plan.ChargerID, plan.Kind, string(weekdays), plan.DeadlineLocal, plan.TargetKWh,
			plan.PreconditionMin, plan.Strategy, plan.Active, formatTime(plan.CreatedAt), formatTime(plan.UpdatedAt))
		if err != nil {
			return plan, err
		}
		plan.ID, err = res.LastInsertId()
		return plan, err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE ev_plans SET kind = ?, weekdays_json = ?, deadline_local = ?, target_energy_kwh = ?,
			precondition_min = ?, strategy = ?, active = ?, updated_at = ? WHERE id = ?`,
		plan.Kind, string(weekdays), plan.DeadlineLocal, plan.TargetKWh,
		plan.PreconditionMin, plan.Strategy, plan.Active, formatTime(plan.UpdatedAt), plan.ID)
	return plan, err
}

// ListEVPlans returns plans for a charger.
func (s *Store) ListEVPlans(ctx context.Context, chargerID string) ([]domain.EVPlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, charger_id, kind, weekdays_json, deadline_local, target_energy_kwh,
			precondition_min, strategy, active, created_at, updated_at
		FROM ev_plans WHERE charger_id = ? ORDER BY id`, chargerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EVPlan
	for rows.Next() {
		plan, err := scanEVPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

// DeleteEVPlan removes a plan.
func (s *Store) DeleteEVPlan(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ev_plans WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActivateEVPlan marks one plan active and every other plan of the same charger
// inactive, so exactly one schedule is in force.
func (s *Store) ActivateEVPlan(ctx context.Context, chargerID string, id int64) error {
	now := formatTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE ev_plans SET active = 0, updated_at = ? WHERE charger_id = ? AND id <> ?`,
		now, chargerID, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE ev_plans SET active = 1, updated_at = ? WHERE charger_id = ? AND id = ?`,
		now, chargerID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SaveEVPlanRun stores a computed/previewed run for audit and UI.
func (s *Store) SaveEVPlanRun(ctx context.Context, planID *int64, preview domain.EVPlanPreview) error {
	windows, err := json.Marshal(preview.Windows)
	if err != nil {
		return err
	}
	warnings, err := json.Marshal(preview.Warnings)
	if err != nil {
		return err
	}
	var id any
	if planID != nil {
		id = *planID
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO ev_plan_runs(plan_id, computed_at, deadline_utc, required_kwh, duration_min,
			windows_json, status, warnings_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, formatTime(preview.ComputedAt), formatTime(preview.Deadline), preview.RemainingKWh,
		preview.RequiredMin, string(windows), preview.State, string(warnings))
	return err
}

// LatestEVPlanRun returns the newest run for a charger's plans.
func (s *Store) LatestEVPlanRun(ctx context.Context, chargerID string) (domain.EVPlanPreview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT r.computed_at, r.deadline_utc, p.target_energy_kwh, r.required_kwh,
			r.duration_min, r.windows_json, r.status, r.warnings_json
		FROM ev_plan_runs r LEFT JOIN ev_plans p ON p.id = r.plan_id
		WHERE p.charger_id = ? ORDER BY r.id DESC LIMIT 1`, chargerID)
	var preview domain.EVPlanPreview
	var computed, deadline, windows, warnings string
	if err := row.Scan(&computed, &deadline, &preview.TargetKWh, &preview.RemainingKWh,
		&preview.RequiredMin, &windows, &preview.State, &warnings); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EVPlanPreview{}, ErrNotFound
		}
		return domain.EVPlanPreview{}, err
	}
	var err error
	if preview.ComputedAt, err = parseTime(computed); err != nil {
		return preview, err
	}
	if preview.Deadline, err = parseTime(deadline); err != nil {
		return preview, err
	}
	if err := json.Unmarshal([]byte(windows), &preview.Windows); err != nil {
		return preview, err
	}
	if err := json.Unmarshal([]byte(warnings), &preview.Warnings); err != nil {
		return preview, err
	}
	preview.ChargerID = chargerID
	return preview, nil
}

// SaveEVSession inserts or updates a session.
func (s *Store) SaveEVSession(ctx context.Context, session domain.EVSession) (domain.EVSession, error) {
	var ended any
	if session.EndedAt != nil {
		ended = formatTime(*session.EndedAt)
	}
	var planID any
	if session.PlanID != nil {
		planID = *session.PlanID
	}
	if session.ID == 0 {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO ev_sessions(charger_id, started_at, ended_at, energy_kwh, cost, mode,
				plan_id, target_kwh, energy_offset_kwh)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.ChargerID, formatTime(session.StartedAt), ended, session.EnergyKWh,
			session.Cost, session.Mode, planID, session.TargetKWh, session.EnergyOffsetKWh)
		if err != nil {
			return session, err
		}
		session.ID, err = res.LastInsertId()
		return session, err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE ev_sessions SET ended_at = ?, energy_kwh = ?, cost = ?, mode = ?,
			plan_id = ?, target_kwh = ?, energy_offset_kwh = ? WHERE id = ?`,
		ended, session.EnergyKWh, session.Cost, session.Mode, planID, session.TargetKWh,
		session.EnergyOffsetKWh, session.ID)
	return session, err
}

// LatestEVSession returns the newest session for a charger.
func (s *Store) LatestEVSession(ctx context.Context, chargerID string) (domain.EVSession, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, charger_id, started_at, ended_at, energy_kwh, cost, mode, plan_id,
			target_kwh, energy_offset_kwh
		FROM ev_sessions WHERE charger_id = ? ORDER BY id DESC LIMIT 1`, chargerID)
	var session domain.EVSession
	var started string
	var ended sql.NullString
	var planID sql.NullInt64
	if err := row.Scan(&session.ID, &session.ChargerID, &started, &ended, &session.EnergyKWh,
		&session.Cost, &session.Mode, &planID, &session.TargetKWh, &session.EnergyOffsetKWh); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.EVSession{}, ErrNotFound
		}
		return domain.EVSession{}, err
	}
	var err error
	if session.StartedAt, err = parseTime(started); err != nil {
		return session, err
	}
	if ended.Valid {
		t, err := parseTime(ended.String)
		if err != nil {
			return session, err
		}
		session.EndedAt = &t
	}
	if planID.Valid {
		id := planID.Int64
		session.PlanID = &id
	}
	return session, nil
}

// RecordEVCommand stores an adapter command audit row.
func (s *Store) RecordEVCommand(ctx context.Context, chargerID, command, params, result, errText string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ev_commands(ts_utc, charger_id, command, params_json, result, error)
		VALUES (?, ?, ?, ?, ?, ?)`,
		formatTime(time.Now()), chargerID, command, params, result, errText)
	return err
}

type evPlanScanner interface {
	Scan(dest ...any) error
}

func scanEVPlan(row evPlanScanner) (domain.EVPlan, error) {
	var plan domain.EVPlan
	var weekdays, created, updated string
	if err := row.Scan(&plan.ID, &plan.ChargerID, &plan.Kind, &weekdays, &plan.DeadlineLocal,
		&plan.TargetKWh, &plan.PreconditionMin, &plan.Strategy, &plan.Active, &created, &updated); err != nil {
		return plan, err
	}
	if err := json.Unmarshal([]byte(weekdays), &plan.Weekdays); err != nil {
		return plan, err
	}
	var err error
	if plan.CreatedAt, err = parseTime(created); err != nil {
		return plan, err
	}
	if plan.UpdatedAt, err = parseTime(updated); err != nil {
		return plan, err
	}
	return plan, nil
}
